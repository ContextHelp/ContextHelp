// Package registry implements entity index sync for configured registries.
// Thin mode: fetches only the entity index (IDs, slugs, titles, version hashes)
// and taxonomy structure — no full definitions.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/graph"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// EntityIndexEntry is the minimal record returned by a registry's /entities/index endpoint.
type EntityIndexEntry struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Namespace   string `json:"namespace"`
	VersionHash string `json:"version_hash"`
}

// TaxonomyEntry is a lightweight namespace/tag node returned by /taxonomy.
type TaxonomyEntry struct {
	Namespace   string `json:"namespace"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// SyncResult summarises the outcome of a registry sync.
type SyncResult struct {
	RegistryURL string
	SyncMode    config.RegistrySyncMode
	Upserted    int
	Skipped     int              // full records not overwritten
	Conflicts   []ConflictReport // non-empty when multi-registry disagreements were resolved
	Errors      []error
}

// MultiSyncConfig carries options for a multi-registry sync.
type MultiSyncConfig struct {
	// Registries is the ordered list of registry configs to sync.
	// Priority: Local > first entry > last entry (unless overridden by TrustScores).
	Registries []config.RegistryConfig
	// Strategy is the merge strategy to apply when registries disagree.
	// Defaults to MergeLastWriteWins when empty.
	Strategy MergeStrategy
	// TrustScores maps registry URL to a 0.0–1.0 trust score.
	// Only used when Strategy == MergeTrustScore.
	TrustScores map[string]float64
}

// MultiSyncResult is the combined outcome of syncing multiple registries.
type MultiSyncResult struct {
	// PerRegistry contains per-registry sync results (including offline results).
	PerRegistry []*SyncResult
	// Merged is the total number of entities written after reconciliation.
	Merged int
	// Conflicts is the union of all field-level conflicts across all entities.
	Conflicts []ConflictReport
	// OfflineRegistries lists registry URLs that were unreachable; their cached
	// local values were used instead.
	OfflineRegistries []string
}

// Syncer performs registry syncs against the entity store.
type Syncer struct {
	store             storage.EntityStore
	registryStore     storage.RegistryStore
	entitlements      storage.EntitlementStore
	guard             *graph.EntityIntegrityGuard
	trustGate         *TrustGate
	client            *http.Client
	requireSignatures bool
}

// New returns a Syncer backed by the given entity store.
func New(store storage.EntityStore) *Syncer {
	return &Syncer{
		store:     store,
		guard:     graph.NewEntityIntegrityGuard(),
		trustGate: NewTrustGate(),
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

// TrustGate returns the Syncer's TrustGate for inspecting pending approvals.
func (s *Syncer) TrustGateState() *TrustGate {
	return s.trustGate
}

// WithEntitlements attaches an entitlement store so the syncer can check and
// persist entitlements before sync when the registry manifest declares one.
func (s *Syncer) WithEntitlements(es storage.EntitlementStore) *Syncer {
	s.entitlements = es
	return s
}

// WithRegistryStore attaches a RegistryStore so the syncer can read cached
// manifests for public key material and update trust status after each sync.
func (s *Syncer) WithRegistryStore(rs storage.RegistryStore) *Syncer {
	s.registryStore = rs
	return s
}

// WithRequireSignatures sets whether unsigned registries (those without a
// public_key in their manifest) are rejected. Default: false.
func (s *Syncer) WithRequireSignatures(require bool) *Syncer {
	s.requireSignatures = require
	return s
}

// WithNamespaces loads a namespace→registry mapping into the integrity guard so
// that entities from unknown namespaces are rejected during sync.
func (s *Syncer) WithNamespaces(ns map[string]string) *Syncer {
	s.guard.LoadNamespaces(ns)
	return s
}

// Sync fetches the remote registry according to cfg.SyncMode and stores results.
// When SyncMode == "thin" (or empty/"full"), the behaviour differs:
//   - thin: fetches /entities/index and /taxonomy; stores thin stubs.
//   - full: fetches /entities/index and /taxonomy; stores full stubs
//     (caller is expected to fetch full definitions separately via PullEntity).
//
// When cfg.EntitlementURL is set and an entitlement store is attached via
// WithEntitlements, an entitlement check is performed before any sync occurs.
// Returns ErrEntitlementRequired when the registry denies access.
//
// Returns a SyncResult describing what was written.
func (s *Syncer) Sync(ctx context.Context, cfg config.RegistryConfig) (*SyncResult, error) {
	mode := cfg.SyncMode
	if mode == "" {
		mode = config.RegistrySyncModeFull
	}

	// Entitlement pre-check: call /entitlements if declared in manifest and store is wired.
	if cfg.EntitlementURL != "" && s.entitlements != nil {
		checker := NewEntitlementChecker(s.entitlements)
		if _, err := checker.FetchAndStore(ctx, cfg.Name, cfg.EntitlementURL, ""); err != nil {
			return nil, fmt.Errorf("entitlement check for %s: %w", cfg.Name, err)
		}
	}

	result := &SyncResult{RegistryURL: cfg.URL, SyncMode: mode}

	entries, err := s.fetchAndVerifyEntityIndex(ctx, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch entity index from %s: %w", cfg.URL, err)
	}

	now := time.Now().Truncate(time.Second)
	for _, entry := range entries {
		e := &storage.Entity{
			Slug:        entry.Slug,
			Title:       entry.Title,
			Namespace:   entry.Namespace,
			VersionHash: entry.VersionHash,
			RegistryURL: cfg.URL,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		// Integrity guard: reject malformed or spoofed entities before write.
		if err := s.guard.ValidateAndGuard(ctx, e, "registry.sync"); err != nil {
			slog.WarnContext(ctx, "registry sync: entity integrity rejected",
				"slug", e.Slug, "registry", cfg.URL, "err", err)
			result.Errors = append(result.Errors, fmt.Errorf("integrity guard %s: %w", e.Slug, err))
			result.Skipped++
			continue
		}

		// Trust gate: enforce per-registry trust level before writing.
		if _, err := s.trustGate.CheckWrite(ctx, e, cfg.URL, cfg.EffectiveTrustLevel()); err != nil {
			result.Skipped++
			continue
		}

		if mode == config.RegistrySyncModeThin {
			if err := s.store.UpsertThin(ctx, e); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("upsert thin %s: %w", e.Slug, err))
				continue
			}
		} else {
			// full mode: store with full status; caller fetches definitions separately.
			e.ContentStatus = storage.ContentStatusFull
			if err := s.store.Upsert(ctx, e); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("upsert %s: %w", e.Slug, err))
				continue
			}
		}
		result.Upserted++
	}

	return result, nil
}

// SyncWithOfflineFallback wraps Sync with offline-first semantics.
// When the remote registry is unreachable, the existing local cached entities
// are retained and marked as offline-sourced in the result.
// If the registry is reachable, it behaves identically to Sync.
//
// Returns (result, true, nil) when online; (result, false, nil) when offline
// and local cache was used; (nil, false, err) on unrecoverable error.
func (s *Syncer) SyncWithOfflineFallback(
	ctx context.Context,
	cfg config.RegistryConfig,
) (*SyncResult, bool, error) {
	result, err := s.Sync(ctx, cfg)
	if err == nil {
		return result, true, nil
	}

	// Registry unreachable: surface warning; use local cache (no-write).
	slog.WarnContext(ctx, "registry unreachable; using cached local entities",
		"registry", cfg.URL, "err", err)

	offlineResult := &SyncResult{
		RegistryURL: cfg.URL,
		SyncMode:    cfg.SyncMode,
		Errors: []error{
			fmt.Errorf("registry unreachable (offline mode): %w", err),
		},
	}
	return offlineResult, false, nil
}

// MultiSync syncs multiple registries, reconciles conflicting entity definitions,
// and writes the merged result to the entity store.
//
// Priority rules:
//  1. Local entities (existing content_status=full) are never downgraded.
//  2. Among remote registries: strategy resolves per-field conflicts.
//  3. Unreachable registries: cached local values are kept; registry is listed in
//     MultiSyncResult.OfflineRegistries.
func (s *Syncer) MultiSync(ctx context.Context, cfg MultiSyncConfig) (*MultiSyncResult, error) {
	strategy := cfg.Strategy
	if strategy == "" {
		strategy = MergeLastWriteWins
	}

	out := &MultiSyncResult{}

	// Phase 1: fetch entity index from each registry (offline-tolerant).
	// slug → list of candidates from all reachable registries.
	allCandidates := make(map[string][]RegistryEntity)

	for _, regCfg := range cfg.Registries {
		trust := cfg.TrustScores[regCfg.URL]
		entries, fetchErr := s.fetchAndVerifyEntityIndex(ctx, regCfg.URL)
		if fetchErr != nil {
			slog.WarnContext(ctx, "multi-sync: registry unreachable",
				"registry", regCfg.URL, "err", fetchErr)
			out.OfflineRegistries = append(out.OfflineRegistries, regCfg.URL)
			out.PerRegistry = append(out.PerRegistry, &SyncResult{
				RegistryURL: regCfg.URL,
				SyncMode:    regCfg.SyncMode,
				Errors:      []error{fmt.Errorf("offline: %w", fetchErr)},
			})
			continue
		}

		now := time.Now().Truncate(time.Second)
		perReg := &SyncResult{RegistryURL: regCfg.URL, SyncMode: regCfg.SyncMode}
		for _, entry := range entries {
			e := &storage.Entity{
				Slug:        entry.Slug,
				Title:       entry.Title,
				Namespace:   entry.Namespace,
				VersionHash: entry.VersionHash,
				RegistryURL: regCfg.URL,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if err := s.guard.ValidateAndGuard(ctx, e, "registry.multi-sync"); err != nil {
				slog.WarnContext(ctx, "multi-sync: integrity rejected",
					"slug", e.Slug, "registry", regCfg.URL, "err", err)
				perReg.Errors = append(perReg.Errors, fmt.Errorf("integrity guard %s: %w", e.Slug, err))
				perReg.Skipped++
				continue
			}
			candidate := RegistryEntity{
				Entity:      e,
				RegistryURL: regCfg.URL,
				UpdatedAt:   now,
				TrustScore:  trust,
			}
			allCandidates[entry.Slug] = append(allCandidates[entry.Slug], candidate)
		}
		out.PerRegistry = append(out.PerRegistry, perReg)
	}

	// Phase 2: reconcile and write.
	mode := cfg.Registries[0].SyncMode
	if mode == "" {
		mode = config.RegistrySyncModeFull
	}

	for slug, candidates := range allCandidates {
		rec := Reconcile(slug, candidates, strategy)
		out.Conflicts = append(out.Conflicts, rec.Conflicts...)

		e := rec.Merged
		if mode == config.RegistrySyncModeThin {
			if err := s.store.UpsertThin(ctx, e); err != nil {
				slog.WarnContext(ctx, "multi-sync: upsert thin failed",
					"slug", slug, "err", err)
				continue
			}
		} else {
			e.ContentStatus = storage.ContentStatusFull
			if err := s.store.Upsert(ctx, e); err != nil {
				slog.WarnContext(ctx, "multi-sync: upsert failed",
					"slug", slug, "err", err)
				continue
			}
		}
		out.Merged++
	}

	return out, nil
}

// SyncPluginProvider drains a PluginRegistryProvider via FetchEntities pagination
// and writes entities to the store under the same rules as HTTP sync.
// mode controls thin vs full storage. providerID is used in logs and SyncResult.RegistryURL.
func (s *Syncer) SyncPluginProvider(
	ctx context.Context,
	provider pluginapi.PluginRegistryProvider,
	mode config.RegistrySyncMode,
) (*SyncResult, error) {
	meta, err := provider.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("plugin registry provider metadata: %w", err)
	}
	providerID := "plugin://" + meta.Name

	if mode == "" {
		mode = config.RegistrySyncModeFull
	}
	result := &SyncResult{RegistryURL: providerID, SyncMode: mode}

	var cursor string
	for {
		entities, next, fetchErr := provider.FetchEntities(ctx, cursor)
		if fetchErr != nil {
			return nil, fmt.Errorf("plugin registry %s: fetch entities: %w", meta.Name, fetchErr)
		}

		now := time.Now().Truncate(time.Second)
		for _, pe := range entities {
			e := &storage.Entity{
				Slug:        pe.Slug,
				Title:       pe.Title,
				Description: pe.Description,
				Namespace:   pe.Namespace,
				Aliases:     pe.Aliases,
				VersionHash: pe.VersionHash,
				RegistryURL: providerID,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if err := s.guard.ValidateAndGuard(ctx, e, "registry.plugin-sync"); err != nil {
				slog.WarnContext(ctx, "plugin registry sync: entity integrity rejected",
					"slug", e.Slug, "provider", meta.Name, "err", err)
				result.Errors = append(result.Errors, fmt.Errorf("integrity guard %s: %w", e.Slug, err))
				result.Skipped++
				continue
			}
			if mode == config.RegistrySyncModeThin {
				if err := s.store.UpsertThin(ctx, e); err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("upsert thin %s: %w", e.Slug, err))
					continue
				}
			} else {
				e.ContentStatus = storage.ContentStatusFull
				if err := s.store.Upsert(ctx, e); err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("upsert %s: %w", e.Slug, err))
					continue
				}
			}
			result.Upserted++
		}

		if next == "" {
			break
		}
		cursor = next
	}
	return result, nil
}

// MultiSyncWithPlugins syncs HTTP registries and plugin-backed registries uniformly.
// HTTP registries are synced first (via MultiSync); plugin providers are appended.
// Results from all sources are combined into a single MultiSyncResult.
func (s *Syncer) MultiSyncWithPlugins(
	ctx context.Context,
	cfg MultiSyncConfig,
	providers []pluginapi.PluginRegistryProvider,
) (*MultiSyncResult, error) {
	out, err := s.MultiSync(ctx, cfg)
	if err != nil {
		return nil, err
	}

	mode := config.RegistrySyncModeFull
	if len(cfg.Registries) > 0 && cfg.Registries[0].SyncMode != "" {
		mode = cfg.Registries[0].SyncMode
	}

	for _, p := range providers {
		res, syncErr := s.SyncPluginProvider(ctx, p, mode)
		if syncErr != nil {
			meta, _ := p.Metadata(ctx)
			slog.WarnContext(ctx, "plugin registry sync failed",
				"provider", meta.Name, "err", syncErr)
			out.PerRegistry = append(out.PerRegistry, &SyncResult{
				RegistryURL: "plugin://" + meta.Name,
				SyncMode:    mode,
				Errors:      []error{syncErr},
			})
			continue
		}
		out.PerRegistry = append(out.PerRegistry, res)
		out.Merged += res.Upserted
	}
	return out, nil
}

// fetchEntityIndex calls GET <registryURL>/entities/index and decodes the JSON array.
func (s *Syncer) fetchEntityIndex(ctx context.Context, registryURL string) ([]EntityIndexEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL+"/entities/index", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}

	var entries []EntityIndexEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode entity index: %w", err)
	}
	return entries, nil
}

// fetchAndVerifyEntityIndex fetches the entity index and, when the registry
// declares a public_key in its cached manifest, verifies the Ed25519 signature
// of the response body. On verification failure the sync is aborted.
//
// When requireSignatures is true and the registry has no public_key,
// ErrSignatureRequired is returned.
func (s *Syncer) fetchAndVerifyEntityIndex(ctx context.Context, registryURL string) ([]EntityIndexEntry, error) {
	// Determine whether we need to verify signatures.
	var publicKey string
	var cachedManifest *storage.RegistryCache
	if s.registryStore != nil {
		var err error
		cachedManifest, err = s.registryStore.GetCachedManifest(ctx, registryURL)
		if err == nil && cachedManifest != nil && cachedManifest.Manifest != nil {
			publicKey = cachedManifest.Manifest.PublicKey
		}
	}

	// If require_signatures is on and there is no public key, refuse to sync.
	if s.requireSignatures && publicKey == "" {
		return nil, ErrSignatureRequired
	}

	// If there is no public key declared, skip verification entirely.
	if publicKey == "" {
		return s.fetchEntityIndex(ctx, registryURL)
	}

	// --- Verified path ---
	indexURL := registryURL + "/entities/index"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}

	// Buffer the body so we can both verify and decode it.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read entity index body: %w", err)
	}

	// Try Content-Signature header first; fall back to .sig sidecar.
	var sigBody []byte
	if resp.Header.Get("Content-Signature") == "" {
		sigBody, _ = FetchSigSidecar(s.client, indexURL+".sig")
	}

	// Compute fingerprint for logging even if verification fails.
	fingerprint, _ := KeyFingerprint(publicKey)

	verifyErr := VerifyResponseSignature(registryURL, publicKey, body, resp, sigBody)
	if verifyErr != nil {
		// Update trust status to failed if we have a registry store.
		if s.registryStore != nil && cachedManifest != nil {
			cachedManifest.TrustStatus = storage.RegistryTrustFailed
			cachedManifest.KeyFingerprint = fingerprint
			_ = s.registryStore.CacheManifest(ctx, cachedManifest)
		}
		var missing ErrSignatureMissing
		if errors.As(verifyErr, &missing) {
			// Missing sig on a declared-key registry → abort sync.
			slog.ErrorContext(ctx, "registry sync: signature missing",
				"registry", registryURL, "fingerprint", fingerprint)
			return nil, verifyErr
		}
		slog.ErrorContext(ctx, "registry sync: signature verification failed",
			"registry", registryURL, "fingerprint", fingerprint, "err", verifyErr)
		return nil, verifyErr
	}

	// Update trust status to verified.
	if s.registryStore != nil && cachedManifest != nil {
		cachedManifest.TrustStatus = storage.RegistryTrustVerified
		cachedManifest.KeyFingerprint = fingerprint
		_ = s.registryStore.CacheManifest(ctx, cachedManifest)
	}
	slog.DebugContext(ctx, "registry sync: signature verified",
		"registry", registryURL, "fingerprint", fingerprint)

	var entries []EntityIndexEntry
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode entity index: %w", err)
	}
	return entries, nil
}

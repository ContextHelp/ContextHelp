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
	Skipped     int // entities the integrity guard rejected
	// PendingApproval counts entities an untrusted registry served: not
	// written, since its writes need approval (TrustGate). With a queue
	// attached (WithPendingQueue) they wait there for approve or reject.
	PendingApproval int
	// Blocked counts entities a sandboxed registry served: never written.
	Blocked   int
	Conflicts []ConflictReport // non-empty when multi-registry disagreements were resolved
	Errors    []error
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
	// PendingApproval and Blocked total the per-registry counts: entities
	// the trust gate held back, never reconciled nor written.
	PendingApproval int
	Blocked         int
}

// Syncer performs registry syncs against the entity store.
type Syncer struct {
	store             storage.EntityStore
	registryStore     storage.RegistryStore
	entitlements      storage.EntitlementStore
	guard             *graph.EntityIntegrityGuard
	trustGate         *TrustGate
	client            *http.Client
	creds             *Credentials
	requireSignatures bool
	pending           storage.PendingEntityStore
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

// WithPendingQueue attaches the approval queue: entities an untrusted
// registry serves are queued there instead of only counted, and an
// entity a sync or approval writes leaves it. Without one, held
// entities are counted and dropped.
func (s *Syncer) WithPendingQueue(q storage.PendingEntityStore) *Syncer {
	s.pending = q
	return s
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

// WithCredentials authenticates every request the syncer makes (entity
// index, signature sidecar, entitlements) with the stored token of the
// registry it targets. A 401/403 from a registry becomes an
// ErrRegistryAuth naming the login command.
func (s *Syncer) WithCredentials(c *Credentials) *Syncer {
	s.creds = c
	s.client = c.Client(s.client)
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
		checker := NewEntitlementChecker(s.entitlements).WithCredentials(s.creds)
		if _, err := checker.FetchAndStore(ctx, cfg.Name, cfg.EntitlementURL); err != nil {
			return nil, fmt.Errorf("entitlement check for %s: %w", cfg.Name, err)
		}
	}

	result := &SyncResult{RegistryURL: cfg.URL, SyncMode: mode}

	entries, sig, err := s.fetchAndVerifyEntityIndex(ctx, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch entity index from %s: %w", config.RedactDSN(cfg.URL), err)
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

		if !s.admit(ctx, e, cfg, sig, result) {
			continue
		}

		if err := s.write(ctx, e, mode); err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		s.unqueue(ctx, queueName(cfg), e.Slug, result)
		result.Upserted++
	}

	return result, nil
}

// admit runs e through the trust gate for cfg's effective trust_level.
// It reports whether e may be written. When not, e counts on res as
// blocked (sandboxed: never queued) or pending approval (untrusted),
// and a pending entity goes into the approval queue, when one is
// attached, with sig, the signature status of the index it came in.
func (s *Syncer) admit(ctx context.Context, e *storage.Entity, cfg config.RegistryConfig, sig string, res *SyncResult) bool {
	rec, err := s.trustGate.CheckWrite(ctx, e, cfg.URL, cfg.EffectiveTrustLevel())
	if err == nil {
		return true
	}
	if rec == nil {
		res.Blocked++
		return false
	}
	if s.approvedAlready(ctx, e, cfg.URL) {
		return false
	}
	if s.pending != nil {
		name, version := queueName(cfg), storage.PendingVersion(e)
		p := &storage.PendingEntity{
			ID:           storage.PendingEntityID(name, e.Slug, version),
			RegistryName: name, RegistryURL: cfg.URL,
			Slug: e.Slug, Namespace: e.Namespace, Title: e.Title, Version: version,
			Entity: e, SignatureStatus: sig,
			FirstSeenAt: e.UpdatedAt, LastSeenAt: e.UpdatedAt,
		}
		if err := s.pending.Upsert(ctx, p); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("queue %s for approval: %w", e.Slug, err))
			return false
		}
	}
	res.PendingApproval++
	return false
}

// approvedAlready reports whether the store holds e at its version from
// the same registry: an earlier approval wrote it, so there is nothing
// left to approve and e is neither queued nor counted.
func (s *Syncer) approvedAlready(ctx context.Context, e *storage.Entity, registryURL string) bool {
	if e.VersionHash == "" {
		return false
	}
	got, err := s.store.Get(ctx, e.Slug)
	return err == nil && got != nil && got.RegistryURL == registryURL && got.VersionHash == e.VersionHash
}

// write stores e the way a sync in mode does: a thin stub, or a full
// record (definitions are pulled separately).
func (s *Syncer) write(ctx context.Context, e *storage.Entity, mode config.RegistrySyncMode) error {
	if mode == config.RegistrySyncModeThin {
		if err := s.store.UpsertThin(ctx, e); err != nil {
			return fmt.Errorf("upsert thin %s: %w", e.Slug, err)
		}
		return nil
	}
	e.ContentStatus = storage.ContentStatusFull
	if err := s.store.Upsert(ctx, e); err != nil {
		return fmt.Errorf("upsert %s: %w", e.Slug, err)
	}
	return nil
}

// unqueue drops every queued version of slug from registry: it was just
// written, so there is nothing left to approve. A failure is reported
// on res (when given); the write stands.
func (s *Syncer) unqueue(ctx context.Context, registry, slug string, res *SyncResult) {
	if s.pending == nil {
		return
	}
	if _, err := s.pending.DeleteEntity(ctx, registry, slug); err != nil && res != nil {
		res.Errors = append(res.Errors, fmt.Errorf("drop %s from approval queue: %w", slug, err))
	}
}

// queueName is the registry name the approval queue files cfg's
// entities under: its configured name, else its URL.
func queueName(cfg config.RegistryConfig) string {
	if cfg.Name != "" {
		return cfg.Name
	}
	return cfg.URL
}

// ProvenanceKey is the entity metadata key Approve records where an
// approved entity came from.
const ProvenanceKey = "provenance"

// Approve writes queued entity p the way a sync in mode writes a
// trusted registry's entities, with metadata provenance "approved from
// <registry>" (kept by full writes; a thin stub stores no metadata),
// then drops every queued version of its slug from that registry.
func (s *Syncer) Approve(ctx context.Context, p *storage.PendingEntity, mode config.RegistrySyncMode) error {
	if p.Entity == nil {
		return fmt.Errorf("pending entity %s has no payload", p.ID)
	}
	e := *p.Entity
	e.Metadata = make(map[string]any, len(p.Entity.Metadata)+1)
	for k, v := range p.Entity.Metadata {
		e.Metadata[k] = v
	}
	e.Metadata[ProvenanceKey] = "approved from " + p.RegistryName
	now := time.Now().Truncate(time.Second)
	e.CreatedAt, e.UpdatedAt = now, now
	if err := s.write(ctx, &e, mode); err != nil {
		return err
	}
	if s.pending != nil {
		if _, err := s.pending.DeleteEntity(ctx, p.RegistryName, p.Slug); err != nil {
			return fmt.Errorf("approved %s, but dropping it from the queue failed: %w", p.Slug, err)
		}
	}
	return nil
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
		"registry", config.RedactDSN(cfg.URL), "err", err)

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
//
// Each registry's trust_level gates its entities exactly as in Sync:
// an untrusted or sandboxed registry's entities never reach
// reconciliation, so they are never written; they are counted per
// registry (PendingApproval, Blocked) and in the totals.
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
		entries, sig, fetchErr := s.fetchAndVerifyEntityIndex(ctx, regCfg.URL)
		if fetchErr != nil {
			slog.WarnContext(ctx, "multi-sync: registry unreachable",
				"registry", config.RedactDSN(regCfg.URL), "err", fetchErr)
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
			// Same trust gate as Sync: reconciliation only ever sees
			// entities the registry's trust_level lets it write.
			if !s.admit(ctx, e, regCfg, sig, perReg) {
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
		out.PendingApproval += perReg.PendingApproval
		out.Blocked += perReg.Blocked
	}

	// Phase 2: reconcile and write.
	mode := cfg.Registries[0].SyncMode
	if mode == "" {
		mode = config.RegistrySyncModeFull
	}

	byURL := make(map[string]config.RegistryConfig, len(cfg.Registries))
	for _, r := range cfg.Registries {
		byURL[r.URL] = r
	}
	for slug, candidates := range allCandidates {
		rec := Reconcile(slug, candidates, strategy)
		out.Conflicts = append(out.Conflicts, rec.Conflicts...)

		if err := s.write(ctx, rec.Merged, mode); err != nil {
			slog.WarnContext(ctx, "multi-sync: write failed", "slug", slug, "err", err)
			continue
		}
		for _, c := range candidates {
			s.unqueue(ctx, queueName(byURL[c.RegistryURL]), slug, nil)
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
		return nil, config.RedactURLError(err)
	}
	defer resp.Body.Close()

	if err := s.creds.CheckResponse(resp); err != nil {
		return nil, err
	}
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
//
// The returned status is storage.SignatureVerified or
// storage.SignatureUnsigned.
func (s *Syncer) fetchAndVerifyEntityIndex(ctx context.Context, registryURL string) ([]EntityIndexEntry, string, error) {
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
		return nil, "", ErrSignatureRequired
	}

	// If there is no public key declared, skip verification entirely.
	if publicKey == "" {
		entries, err := s.fetchEntityIndex(ctx, registryURL)
		return entries, storage.SignatureUnsigned, err
	}

	// --- Verified path ---
	indexURL := registryURL + "/entities/index"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", config.RedactURLError(err)
	}
	defer resp.Body.Close()

	if err := s.creds.CheckResponse(resp); err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}

	// Buffer the body so we can both verify and decode it.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read entity index body: %w", err)
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
		s.recordTrust(ctx, cachedManifest, storage.RegistryTrustFailed, fingerprint)
		var missing ErrSignatureMissing
		if errors.As(verifyErr, &missing) {
			// Missing sig on a declared-key registry → abort sync.
			slog.ErrorContext(ctx, "registry sync: signature missing",
				"registry", config.RedactDSN(registryURL), "fingerprint", fingerprint)
			return nil, "", verifyErr
		}
		slog.ErrorContext(ctx, "registry sync: signature verification failed",
			"registry", config.RedactDSN(registryURL), "fingerprint", fingerprint, "err", verifyErr)
		return nil, "", verifyErr
	}

	s.recordTrust(ctx, cachedManifest, storage.RegistryTrustVerified, fingerprint)
	slog.DebugContext(ctx, "registry sync: signature verified",
		"registry", config.RedactDSN(registryURL), "fingerprint", fingerprint)

	var entries []EntityIndexEntry
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&entries); err != nil {
		return nil, "", fmt.Errorf("decode entity index: %w", err)
	}
	return entries, storage.SignatureVerified, nil
}

// recordTrust stores the outcome of a signature check on the cached
// manifest, when there is a registry store and a cached manifest.
func (s *Syncer) recordTrust(ctx context.Context, cache *storage.RegistryCache, status storage.RegistryTrustStatus, fingerprint string) {
	if s.registryStore == nil || cache == nil {
		return
	}
	cache.TrustStatus = status
	cache.KeyFingerprint = fingerprint
	_ = s.registryStore.CacheManifest(ctx, cache)
}

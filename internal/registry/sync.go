// Package registry implements entity index sync for configured registries.
// Thin mode: fetches only the entity index (IDs, slugs, titles, version hashes)
// and taxonomy structure — no full definitions.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/graph"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
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
	Skipped     int // full records not overwritten
	Errors      []error
}

// Syncer performs registry syncs against the entity store.
type Syncer struct {
	store  storage.EntityStore
	guard  *graph.EntityIntegrityGuard
	client *http.Client
}

// New returns a Syncer backed by the given entity store.
func New(store storage.EntityStore) *Syncer {
	return &Syncer{
		store:  store,
		guard:  graph.NewEntityIntegrityGuard(),
		client: &http.Client{Timeout: 30 * time.Second},
	}
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
// Returns a SyncResult describing what was written.
func (s *Syncer) Sync(ctx context.Context, cfg config.RegistryConfig) (*SyncResult, error) {
	mode := cfg.SyncMode
	if mode == "" {
		mode = config.RegistrySyncModeFull
	}

	result := &SyncResult{RegistryURL: cfg.URL, SyncMode: mode}

	entries, err := s.fetchEntityIndex(ctx, cfg.URL)
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

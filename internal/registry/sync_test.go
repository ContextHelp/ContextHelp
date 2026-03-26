package registry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stubEntityStore is an in-memory EntityStore for testing.
type stubEntityStore struct {
	full []storage.Entity
	thin []storage.Entity
}

func (s *stubEntityStore) Upsert(_ context.Context, e *storage.Entity) error {
	s.full = append(s.full, *e)
	return nil
}

func (s *stubEntityStore) UpsertThin(_ context.Context, e *storage.Entity) error {
	s.thin = append(s.thin, *e)
	return nil
}

func (s *stubEntityStore) SetContentStatus(_ context.Context, _ string, _ storage.ContentStatus) error {
	return nil
}

func (s *stubEntityStore) Get(_ context.Context, _ string) (*storage.Entity, error) {
	return nil, nil
}

func (s *stubEntityStore) List(_ context.Context, _ storage.EntityFilter) ([]*storage.Entity, error) {
	return nil, nil
}

func (s *stubEntityStore) Resolve(_ context.Context, _ string) (*storage.Entity, error) {
	return nil, nil
}

func makeTestServer(entries []registry.EntityIndexEntry) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}))
}

func TestSyncer_ThinMode_StoresThinStubs(t *testing.T) {
	entries := []registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
		{Slug: "ai.gpt", Title: "GPT", Namespace: "ai", VersionHash: "v2"},
	}
	srv := makeTestServer(entries)
	defer srv.Close()

	store := &stubEntityStore{}
	syncer := registry.New(store)

	result, err := syncer.Sync(context.Background(), config.RegistryConfig{
		URL:        srv.URL,
		SyncMode:   config.RegistrySyncModeThin,
		TrustLevel: config.RegistryTrustLevelTrusted,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if result.Upserted != 2 {
		t.Errorf("Upserted: got %d, want 2", result.Upserted)
	}
	if len(store.thin) != 2 {
		t.Errorf("thin store count: got %d, want 2", len(store.thin))
	}
	if len(store.full) != 0 {
		t.Errorf("full store should be empty in thin mode, got %d", len(store.full))
	}
	if store.thin[0].RegistryURL != srv.URL {
		t.Errorf("RegistryURL: got %q, want %q", store.thin[0].RegistryURL, srv.URL)
	}
}

func TestSyncer_FullMode_StoresFullRecords(t *testing.T) {
	entries := []registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
	}
	srv := makeTestServer(entries)
	defer srv.Close()

	store := &stubEntityStore{}
	syncer := registry.New(store)

	result, err := syncer.Sync(context.Background(), config.RegistryConfig{
		URL:        srv.URL,
		SyncMode:   config.RegistrySyncModeFull,
		TrustLevel: config.RegistryTrustLevelTrusted,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if result.Upserted != 1 {
		t.Errorf("Upserted: got %d, want 1", result.Upserted)
	}
	if len(store.full) != 1 {
		t.Errorf("full store count: got %d, want 1", len(store.full))
	}
	if len(store.thin) != 0 {
		t.Errorf("thin store should be empty in full mode, got %d", len(store.thin))
	}
}

func TestSyncer_DefaultMode_TreatsAsFull(t *testing.T) {
	entries := []registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai"},
	}
	srv := makeTestServer(entries)
	defer srv.Close()

	store := &stubEntityStore{}
	syncer := registry.New(store)

	// Empty SyncMode should default to full.
	result, err := syncer.Sync(context.Background(), config.RegistryConfig{
		URL:        srv.URL,
		TrustLevel: config.RegistryTrustLevelTrusted,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if result.SyncMode != config.RegistrySyncModeFull {
		t.Errorf("SyncMode: got %q, want %q", result.SyncMode, config.RegistrySyncModeFull)
	}
}

func TestSyncer_RegistryError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	store := &stubEntityStore{}
	syncer := registry.New(store)
	_, err := syncer.Sync(context.Background(), config.RegistryConfig{
		URL:      srv.URL,
		SyncMode: config.RegistrySyncModeThin,
	})
	if err == nil {
		t.Error("expected error for 404 response, got nil")
	}
}

func TestSyncer_UntrustedRegistry_QueuesNotWrites(t *testing.T) {
	entries := []registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
	}
	srv := makeTestServer(entries)
	defer srv.Close()

	store := &stubEntityStore{}
	syncer := registry.New(store)

	result, err := syncer.Sync(context.Background(), config.RegistryConfig{
		URL:        srv.URL,
		SyncMode:   config.RegistrySyncModeFull,
		TrustLevel: config.RegistryTrustLevelUntrusted,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Entity should be queued for approval, not written.
	if result.Upserted != 0 {
		t.Errorf("Upserted: got %d, want 0 (untrusted)", result.Upserted)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped: got %d, want 1", result.Skipped)
	}
	if len(store.full) != 0 {
		t.Errorf("no entities should be written for untrusted registry")
	}

	// Trust gate should have 1 pending approval.
	pending := syncer.TrustGateState().PendingApprovals()
	if len(pending) != 1 {
		t.Fatalf("pending approvals: got %d, want 1", len(pending))
	}
	if pending[0].Entity.Slug != "ai.bert" {
		t.Errorf("pending slug: got %q", pending[0].Entity.Slug)
	}
}

func TestSyncer_SandboxedRegistry_BlocksNoQueue(t *testing.T) {
	entries := []registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
	}
	srv := makeTestServer(entries)
	defer srv.Close()

	store := &stubEntityStore{}
	syncer := registry.New(store)

	result, err := syncer.Sync(context.Background(), config.RegistryConfig{
		URL:        srv.URL,
		SyncMode:   config.RegistrySyncModeFull,
		TrustLevel: config.RegistryTrustLevelSandboxed,
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if result.Upserted != 0 {
		t.Errorf("Upserted: got %d, want 0 (sandboxed)", result.Upserted)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped: got %d, want 1", result.Skipped)
	}
	// Sandboxed: no pending approvals (hard block).
	pending := syncer.TrustGateState().PendingApprovals()
	if len(pending) != 0 {
		t.Errorf("sandboxed should not queue, got %d pending", len(pending))
	}
}

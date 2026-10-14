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
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
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

// newQueue is a fresh SQLite approval queue.
func newQueue(t *testing.T) storage.PendingEntityStore {
	t.Helper()
	return storageutil.NewTestDriver(t).PendingEntities()
}

func listQueue(t *testing.T, q storage.PendingEntityStore, registry string) []*storage.PendingEntity {
	t.Helper()
	got, err := q.List(context.Background(), storage.PendingEntityFilter{RegistryName: registry})
	if err != nil {
		t.Fatalf("List queue: %v", err)
	}
	return got
}

func TestSyncer_UntrustedRegistry_QueuesNotWrites(t *testing.T) {
	entries := []registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
	}
	srv := makeTestServer(entries)
	defer srv.Close()

	store := &stubEntityStore{}
	q := newQueue(t)
	syncer := registry.New(store).WithPendingQueue(q)

	result, err := syncer.Sync(context.Background(), config.RegistryConfig{
		Name:       "pub",
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
	// Held for approval, counted as such: not lumped in with Skipped.
	if result.PendingApproval != 1 || result.Blocked != 0 || result.Skipped != 0 {
		t.Errorf("PendingApproval/Blocked/Skipped = %d/%d/%d, want 1/0/0",
			result.PendingApproval, result.Blocked, result.Skipped)
	}
	if len(store.full) != 0 {
		t.Errorf("no entities should be written for untrusted registry")
	}

	// The persistent queue holds it, with what review needs.
	pending := listQueue(t, q, "pub")
	if len(pending) != 1 {
		t.Fatalf("queued: got %d, want 1", len(pending))
	}
	p := pending[0]
	if p.Slug != "ai.bert" || p.Version != "v1" || p.RegistryURL != srv.URL ||
		p.SignatureStatus != storage.SignatureUnsigned || p.Entity == nil || p.Entity.Title != "BERT" ||
		p.ID != storage.PendingEntityID("pub", "ai.bert", "v1") {
		t.Errorf("queued = %+v", p)
	}
}

func TestSyncer_SandboxedRegistry_BlocksNoQueue(t *testing.T) {
	entries := []registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
	}
	srv := makeTestServer(entries)
	defer srv.Close()

	store := &stubEntityStore{}
	q := newQueue(t)
	syncer := registry.New(store).WithPendingQueue(q)

	result, err := syncer.Sync(context.Background(), config.RegistryConfig{
		Name:       "box",
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
	if result.Blocked != 1 || result.PendingApproval != 0 || result.Skipped != 0 {
		t.Errorf("Blocked/PendingApproval/Skipped = %d/%d/%d, want 1/0/0",
			result.Blocked, result.PendingApproval, result.Skipped)
	}
	// Sandboxed: a hard block, nothing to approve.
	if got := listQueue(t, q, ""); len(got) != 0 {
		t.Errorf("sandboxed should not queue, got %d queued", len(got))
	}
}

// TestSyncer_TrustedSyncDrainsQueue: once the registry is trusted, the
// next sync writes the entity and it leaves the queue.
func TestSyncer_TrustedSyncDrainsQueue(t *testing.T) {
	srv := makeTestServer([]registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
	})
	defer srv.Close()
	store := &stubEntityStore{}
	q := newQueue(t)
	syncer := registry.New(store).WithPendingQueue(q)
	cfg := config.RegistryConfig{Name: "pub", URL: srv.URL}

	if _, err := syncer.Sync(context.Background(), cfg); err != nil {
		t.Fatalf("untrusted Sync: %v", err)
	}
	if got := listQueue(t, q, "pub"); len(got) != 1 {
		t.Fatalf("queued after untrusted sync: %d, want 1", len(got))
	}

	cfg.TrustLevel = config.RegistryTrustLevelTrusted
	res, err := syncer.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatalf("trusted Sync: %v", err)
	}
	if res.Upserted != 1 || len(store.full) != 1 {
		t.Fatalf("trusted sync wrote %d (%d stored), want 1", res.Upserted, len(store.full))
	}
	if got := listQueue(t, q, "pub"); len(got) != 0 {
		t.Errorf("queue after trusted sync = %+v, want empty", got)
	}
}

// TestSyncer_Approve: approval writes the queued entity through the
// sync write path with provenance and drops it from the queue.
func TestSyncer_Approve(t *testing.T) {
	srv := makeTestServer([]registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
	})
	defer srv.Close()
	store := &stubEntityStore{}
	q := newQueue(t)
	syncer := registry.New(store).WithPendingQueue(q)
	if _, err := syncer.Sync(context.Background(), config.RegistryConfig{Name: "pub", URL: srv.URL}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	p := listQueue(t, q, "pub")[0]

	if err := syncer.Approve(context.Background(), p, config.RegistrySyncModeFull); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(store.full) != 1 {
		t.Fatalf("approved entity not written: %+v", store.full)
	}
	got := store.full[0]
	if got.Slug != "ai.bert" || got.RegistryURL != srv.URL || got.ContentStatus != storage.ContentStatusFull ||
		got.Metadata[registry.ProvenanceKey] != "approved from pub" {
		t.Errorf("written = %+v", got)
	}
	if rest := listQueue(t, q, ""); len(rest) != 0 {
		t.Errorf("queue after approve = %+v, want empty", rest)
	}
}

// TestSyncer_MultiSync_TrustGate: reconciliation honours each registry's
// trust_level like a single sync. Only the trusted registry's entities are
// written; the untrusted one's count as pending approval and the
// sandboxed one's as blocked, per registry and in the totals.
func TestSyncer_MultiSync_TrustGate(t *testing.T) {
	trusted := makeTestServer([]registry.EntityIndexEntry{
		{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
	})
	defer trusted.Close()
	untrusted := makeTestServer([]registry.EntityIndexEntry{
		{Slug: "ai.gpt", Title: "GPT", Namespace: "ai", VersionHash: "v1"},
		{Slug: "ai.bert", Title: "BERT (spoofed)", Namespace: "ai", VersionHash: "v9"},
	})
	defer untrusted.Close()
	sandboxed := makeTestServer([]registry.EntityIndexEntry{
		{Slug: "ai.t5", Title: "T5", Namespace: "ai", VersionHash: "v1"},
	})
	defer sandboxed.Close()

	store := &stubEntityStore{}
	q := newQueue(t)
	out, err := registry.New(store).WithPendingQueue(q).MultiSync(context.Background(), registry.MultiSyncConfig{
		Registries: []config.RegistryConfig{
			{Name: "u", URL: untrusted.URL, TrustLevel: config.RegistryTrustLevelUntrusted},
			{Name: "s", URL: sandboxed.URL, TrustLevel: config.RegistryTrustLevelSandboxed},
			{Name: "t", URL: trusted.URL, TrustLevel: config.RegistryTrustLevelTrusted},
		},
	})
	if err != nil {
		t.Fatalf("MultiSync: %v", err)
	}

	if len(store.full) != 1 || store.full[0].Slug != "ai.bert" || store.full[0].Title != "BERT" ||
		store.full[0].RegistryURL != trusted.URL {
		t.Fatalf("written = %+v, want only the trusted registry's ai.bert", store.full)
	}
	if out.Merged != 1 || out.PendingApproval != 2 || out.Blocked != 1 {
		t.Errorf("Merged/PendingApproval/Blocked = %d/%d/%d, want 1/2/1",
			out.Merged, out.PendingApproval, out.Blocked)
	}
	if len(out.Conflicts) != 0 {
		t.Errorf("conflicts = %+v; held entities must not take part in reconciliation", out.Conflicts)
	}
	if got := listQueue(t, q, "u"); len(got) != 2 {
		t.Errorf("untrusted registry queued %d, want 2", len(got))
	}
	if got := listQueue(t, q, ""); len(got) != 2 {
		t.Errorf("queue holds %d, want only the untrusted registry's 2", len(got))
	}
	want := map[string][2]int{untrusted.URL: {2, 0}, sandboxed.URL: {0, 1}, trusted.URL: {0, 0}}
	for _, r := range out.PerRegistry {
		if got := [2]int{r.PendingApproval, r.Blocked}; got != want[r.RegistryURL] {
			t.Errorf("%s pending/blocked = %v, want %v", r.RegistryURL, got, want[r.RegistryURL])
		}
	}
}

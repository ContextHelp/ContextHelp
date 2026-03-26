package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newTestSvc builds a service backed by an in-memory SQLite driver.
func newTestSvc(t *testing.T) *Service {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := pipeline.DefaultRegistry()
	engine := search.NewEngine(driver)
	return New(driver, q, pipes, engine, "", nil)
}

func TestClassifyStepKind(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"entity_extractor", "entity"},
		{"entities_sync", "entity"},
		{"taxonomy_loader", "taxonomy"},
		{"taxon_index", "taxonomy"},
		{"alias_resolver", "alias"},
		{"text_summary", "step"},
		{"url_generic", "step"},
		{"", "step"},
	}
	for _, tc := range cases {
		got := classifyStepKind(tc.name)
		if got != tc.want {
			t.Errorf("classifyStepKind(%q) = %q; want %q", tc.name, got, tc.want)
		}
	}
}

func TestDiffRegistrySyncAllAdditions(t *testing.T) {
	// Remote has two steps; local cache empty → both should appear as "add".
	remote := storage.RegistryManifest{
		Name:    "test-registry",
		Version: "1.0.0",
		Steps: []storage.ManifestStep{
			{Name: "text_summary", Version: "1.0.0"},
			{Name: "entity_extractor", Version: "2.0.0"},
		},
	}
	srv := httptest.NewServer(serveManifest(t, remote))
	defer srv.Close()

	svc := newTestSvc(t)
	ctx := context.Background()

	diffs, err := svc.DiffRegistrySync(ctx, srv.URL)
	if err != nil {
		t.Fatalf("DiffRegistrySync: %v", err)
	}

	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d: %+v", len(diffs), diffs)
	}
	for _, d := range diffs {
		if d.Action != "add" {
			t.Errorf("expected action=add, got %q for %q", d.Action, d.Name)
		}
		if d.OldValue != "" {
			t.Errorf("add entry should have empty OldValue, got %q", d.OldValue)
		}
	}
}

func TestDiffRegistrySyncUpdate(t *testing.T) {
	// Seed local cache with v1.0.0; remote has v2.0.0 → one "update".
	svc := newTestSvc(t)
	ctx := context.Background()

	localManifest := &storage.RegistryManifest{
		Name:    "test-registry",
		Version: "1.0.0",
		Steps:   []storage.ManifestStep{{Name: "text_summary", Version: "1.0.0"}},
	}
	cacheURL := "http://test.example.com/manifest.json"
	err := svc.Store.Registries().CacheManifest(ctx, &storage.RegistryCache{
		RegistryURL: cacheURL,
		Manifest:    localManifest,
	})
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	remote := storage.RegistryManifest{
		Name:    "test-registry",
		Version: "2.0.0",
		Steps:   []storage.ManifestStep{{Name: "text_summary", Version: "2.0.0"}},
	}

	// Serve remote manifest and point the diff at a reachable URL.
	srv := httptest.NewServer(serveManifest(t, remote))
	defer srv.Close()

	// Re-cache under the test server's URL so DiffRegistrySync finds local state.
	err = svc.Store.Registries().CacheManifest(ctx, &storage.RegistryCache{
		RegistryURL: srv.URL,
		Manifest:    localManifest,
	})
	if err != nil {
		t.Fatalf("seed cache at srv URL: %v", err)
	}

	diffs, err := svc.DiffRegistrySync(ctx, srv.URL)
	if err != nil {
		t.Fatalf("DiffRegistrySync: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %+v", len(diffs), diffs)
	}
	d := diffs[0]
	if d.Action != "update" {
		t.Errorf("expected action=update, got %q", d.Action)
	}
	if d.OldValue != "1.0.0" {
		t.Errorf("expected OldValue=1.0.0, got %q", d.OldValue)
	}
	if d.NewValue != "2.0.0" {
		t.Errorf("expected NewValue=2.0.0, got %q", d.NewValue)
	}
	if d.Kind != "step" {
		t.Errorf("expected Kind=step, got %q", d.Kind)
	}
}

func TestDiffRegistrySyncRemoval(t *testing.T) {
	// Local has a step not in remote → "remove".
	svc := newTestSvc(t)
	ctx := context.Background()

	remote := storage.RegistryManifest{
		Name:    "test-registry",
		Version: "2.0.0",
		Steps:   []storage.ManifestStep{},
	}
	srv := httptest.NewServer(serveManifest(t, remote))
	defer srv.Close()

	localManifest := &storage.RegistryManifest{
		Name:    "test-registry",
		Version: "1.0.0",
		Steps:   []storage.ManifestStep{{Name: "old_step", Version: "1.0.0"}},
	}
	err := svc.Store.Registries().CacheManifest(ctx, &storage.RegistryCache{
		RegistryURL: srv.URL,
		Manifest:    localManifest,
	})
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	diffs, err := svc.DiffRegistrySync(ctx, srv.URL)
	if err != nil {
		t.Fatalf("DiffRegistrySync: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %+v", len(diffs), diffs)
	}
	if diffs[0].Action != "remove" {
		t.Errorf("expected action=remove, got %q", diffs[0].Action)
	}
	if diffs[0].OldValue != "1.0.0" {
		t.Errorf("expected OldValue=1.0.0, got %q", diffs[0].OldValue)
	}
}

func TestDiffRegistrySyncNoChanges(t *testing.T) {
	// Local and remote identical → empty diff.
	svc := newTestSvc(t)
	ctx := context.Background()

	manifest := storage.RegistryManifest{
		Name:    "test-registry",
		Version: "1.0.0",
		Steps:   []storage.ManifestStep{{Name: "text_summary", Version: "1.0.0"}},
	}
	srv := httptest.NewServer(serveManifest(t, manifest))
	defer srv.Close()

	err := svc.Store.Registries().CacheManifest(ctx, &storage.RegistryCache{
		RegistryURL: srv.URL,
		Manifest:    &manifest,
	})
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	diffs, err := svc.DiffRegistrySync(ctx, srv.URL)
	if err != nil {
		t.Fatalf("DiffRegistrySync: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical manifests, got %d: %+v", len(diffs), diffs)
	}
}

func TestDiffRegistrySyncWritesNothing(t *testing.T) {
	// After DiffRegistrySync the local cache must remain unchanged.
	svc := newTestSvc(t)
	ctx := context.Background()

	local := storage.RegistryManifest{
		Name:    "test-registry",
		Version: "1.0.0",
		Steps:   []storage.ManifestStep{{Name: "text_summary", Version: "1.0.0"}},
	}
	remote := storage.RegistryManifest{
		Name:    "test-registry",
		Version: "2.0.0",
		Steps:   []storage.ManifestStep{{Name: "text_summary", Version: "2.0.0"}},
	}
	srv := httptest.NewServer(serveManifest(t, remote))
	defer srv.Close()

	err := svc.Store.Registries().CacheManifest(ctx, &storage.RegistryCache{
		RegistryURL: srv.URL,
		Manifest:    &local,
	})
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// Run dry-run diff.
	_, err = svc.DiffRegistrySync(ctx, srv.URL)
	if err != nil {
		t.Fatalf("DiffRegistrySync: %v", err)
	}

	// Local cache must still show version 1.0.0.
	cached, err := svc.Store.Registries().GetCachedManifest(ctx, srv.URL)
	if err != nil {
		t.Fatalf("GetCachedManifest: %v", err)
	}
	if cached.Manifest == nil {
		t.Fatal("cached manifest is nil after dry-run")
	}
	if cached.Manifest.Version != "1.0.0" {
		t.Errorf("dry-run must not update local cache: want 1.0.0, got %q", cached.Manifest.Version)
	}
}

func TestDiffRegistrySyncKindClassification(t *testing.T) {
	// Verify entity/taxonomy/alias kinds flow through correctly.
	remote := storage.RegistryManifest{
		Name:    "test-registry",
		Version: "1.0.0",
		Steps: []storage.ManifestStep{
			{Name: "entity_extractor", Version: "1.0.0"},
			{Name: "taxonomy_loader", Version: "1.0.0"},
			{Name: "alias_resolver", Version: "1.0.0"},
			{Name: "text_summary", Version: "1.0.0"},
		},
	}
	srv := httptest.NewServer(serveManifest(t, remote))
	defer srv.Close()

	svc := newTestSvc(t)
	ctx := context.Background()

	diffs, err := svc.DiffRegistrySync(ctx, srv.URL)
	if err != nil {
		t.Fatalf("DiffRegistrySync: %v", err)
	}
	if len(diffs) != 4 {
		t.Fatalf("expected 4 diffs, got %d", len(diffs))
	}

	kindByName := make(map[string]string)
	for _, d := range diffs {
		kindByName[d.Name] = d.Kind
	}
	want := map[string]string{
		"entity_extractor": "entity",
		"taxonomy_loader":  "taxonomy",
		"alias_resolver":   "alias",
		"text_summary":     "step",
	}
	for name, wantKind := range want {
		if got := kindByName[name]; got != wantKind {
			t.Errorf("step %q: want kind %q, got %q", name, wantKind, got)
		}
	}
}

// serveManifest returns an http.Handler that serves manifest as JSON.
func serveManifest(t *testing.T, manifest storage.RegistryManifest) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(manifest); err != nil {
			t.Errorf("encode manifest: %v", err)
		}
	}
}

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	registrysync "github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// syncRegistry serves a manifest whose capabilities.entity_sync is
// entitySync, an entity index at /entities/index, and 404 elsewhere; it
// records every path requested.
func syncRegistry(t *testing.T, entitySync bool) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/", "":
			_ = json.NewEncoder(w).Encode(storage.RegistryManifest{
				Name: "pub", Version: "1.0.0",
				Capabilities: storage.RegistryCapabilities{EntitySync: entitySync},
			})
		case "/entities/index":
			_ = json.NewEncoder(w).Encode([]registrysync.EntityIndexEntry{
				{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), paths...)
	}
}

func newSyncSvc(t *testing.T, regURL string) *Service {
	t.Helper()
	return newSyncSvcTrust(t, regURL, config.RegistryTrustLevelTrusted)
}

func newSyncSvcTrust(t *testing.T, regURL string, trust config.RegistryTrustLevel) *Service {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	cfg := config.Config{Registries: []config.RegistryConfig{{
		Name: "pub", URL: regURL,
		SyncMode:   config.RegistrySyncModeThin,
		TrustLevel: trust,
	}}}
	return NewWithOptions(driver, jobs.NewQueue(driver.Jobs()), pipeline.DefaultRegistry(),
		search.NewEngine(driver), "", nil, nil, cfg)
}

// TestSyncRegistry_EntitySyncAdvertised: a manifest with
// capabilities.entity_sync makes sync pull the entity index too.
func TestSyncRegistry_EntitySyncAdvertised(t *testing.T) {
	srv, paths := syncRegistry(t, true)
	svc := newSyncSvc(t, srv.URL)
	ctx := context.Background()

	res, err := svc.SyncRegistry(ctx, srv.URL)
	if err != nil {
		t.Fatalf("SyncRegistry: %v", err)
	}
	if !res.EntitySync || res.Upserted != 1 {
		t.Errorf("SyncRegistry = %+v, want EntitySync and 1 entity", res)
	}
	if _, err := svc.Store.Entities().Get(ctx, "ai.bert"); err != nil {
		t.Errorf("entity ai.bert not stored: %v", err)
	}
	if cache, err := svc.Store.Registries().GetCachedManifest(ctx, srv.URL); err != nil || cache == nil {
		t.Errorf("manifest not cached: %v", err)
	}
	if got := paths(); !contains(got, "/entities/index") {
		t.Errorf("registry served %v, want /entities/index among them", got)
	}
}

// TestSyncRegistry_StepsOnlyRegistry: without capabilities.entity_sync
// sync fetches the manifest only and never asks for /entities/index.
func TestSyncRegistry_StepsOnlyRegistry(t *testing.T) {
	srv, paths := syncRegistry(t, false)
	svc := newSyncSvc(t, srv.URL)

	res, err := svc.SyncRegistry(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("SyncRegistry: %v", err)
	}
	if res.EntitySync || res.Upserted != 0 {
		t.Errorf("SyncRegistry = %+v, want no entity sync", res)
	}
	if got := paths(); len(got) != 1 || got[0] != "/" {
		t.Errorf("registry served %v, want the manifest only", got)
	}
}

// TestSyncRegistry_NotTrustedReportsHeldEntities: entities a registry
// that is not trusted serves are not written, and the result counts them
// by why (approval for untrusted, including an unset trust_level; blocked
// for sandboxed) instead of reporting a bare zero.
func TestSyncRegistry_NotTrustedReportsHeldEntities(t *testing.T) {
	for _, tc := range []struct {
		trust          config.RegistryTrustLevel
		wantLevel      config.RegistryTrustLevel
		pending, block int
	}{
		{"", config.RegistryTrustLevelUntrusted, 1, 0},
		{config.RegistryTrustLevelUntrusted, config.RegistryTrustLevelUntrusted, 1, 0},
		{config.RegistryTrustLevelSandboxed, config.RegistryTrustLevelSandboxed, 0, 1},
	} {
		t.Run(string(tc.wantLevel)+"/"+string(tc.trust), func(t *testing.T) {
			srv, _ := syncRegistry(t, true)
			svc := newSyncSvcTrust(t, srv.URL, tc.trust)
			ctx := context.Background()

			res, err := svc.SyncRegistry(ctx, srv.URL)
			if err != nil {
				t.Fatalf("SyncRegistry: %v", err)
			}
			if res.Upserted != 0 || res.PendingApproval != tc.pending || res.Blocked != tc.block ||
				res.TrustLevel != tc.wantLevel {
				t.Errorf("SyncRegistry = %+v, want 0 upserted, %d pending, %d blocked, trust %q",
					res, tc.pending, tc.block, tc.wantLevel)
			}
			if e, err := svc.Store.Entities().Get(ctx, "ai.bert"); err == nil && e != nil {
				t.Errorf("entity from a %q registry was written: %+v", tc.wantLevel, e)
			}
		})
	}
}

// indexRegistry serves one entity index at /entities/index.
func indexRegistry(t *testing.T, entries ...registrysync.EntityIndexEntry) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/entities/index" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(entries)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestSyncAllRegistriesWithReconciliation_TrustGate: `registry sync
// --reconcile` writes only the trusted registry's entities and reports
// what each untrusted or sandboxed registry held back.
func TestSyncAllRegistriesWithReconciliation_TrustGate(t *testing.T) {
	untrusted := indexRegistry(t, registrysync.EntityIndexEntry{Slug: "ai.gpt", Title: "GPT", Namespace: "ai", VersionHash: "v1"})
	sandboxed := indexRegistry(t, registrysync.EntityIndexEntry{Slug: "ai.t5", Title: "T5", Namespace: "ai", VersionHash: "v1"})
	trusted := indexRegistry(t, registrysync.EntityIndexEntry{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"})

	driver := storageutil.NewTestDriver(t)
	cfg := config.Config{Registries: []config.RegistryConfig{
		{Name: "u", URL: untrusted}, // trust_level unset: untrusted
		{Name: "s", URL: sandboxed, TrustLevel: config.RegistryTrustLevelSandboxed},
		{Name: "t", URL: trusted, TrustLevel: config.RegistryTrustLevelTrusted},
	}}
	svc := NewWithOptions(driver, jobs.NewQueue(driver.Jobs()), pipeline.DefaultRegistry(),
		search.NewEngine(driver), "", nil, nil, cfg)
	ctx := context.Background()

	res, err := svc.SyncAllRegistriesWithReconciliation(ctx, "", nil)
	if err != nil {
		t.Fatalf("SyncAllRegistriesWithReconciliation: %v", err)
	}
	if res.Merged != 1 || res.PendingApproval != 1 || res.Blocked != 1 {
		t.Errorf("Merged/PendingApproval/Blocked = %d/%d/%d, want 1/1/1",
			res.Merged, res.PendingApproval, res.Blocked)
	}
	want := []ReconciledRegistryCounts{
		{Name: "u", TrustLevel: config.RegistryTrustLevelUntrusted, PendingApproval: 1},
		{Name: "s", TrustLevel: config.RegistryTrustLevelSandboxed, Blocked: 1},
		{Name: "t", TrustLevel: config.RegistryTrustLevelTrusted},
	}
	if len(res.Registries) != len(want) {
		t.Fatalf("Registries = %+v, want %+v", res.Registries, want)
	}
	for i := range want {
		if res.Registries[i] != want[i] {
			t.Errorf("Registries[%d] = %+v, want %+v", i, res.Registries[i], want[i])
		}
	}
	if _, err := svc.Store.Entities().Get(ctx, "ai.bert"); err != nil {
		t.Errorf("trusted entity ai.bert not written: %v", err)
	}
	for _, slug := range []string{"ai.gpt", "ai.t5"} {
		if e, err := svc.Store.Entities().Get(ctx, slug); err == nil && e != nil {
			t.Errorf("held entity %s was written: %+v", slug, e)
		}
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

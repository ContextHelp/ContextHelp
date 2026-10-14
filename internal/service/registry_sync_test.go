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
	driver := storageutil.NewTestDriver(t)
	cfg := config.Config{Registries: []config.RegistryConfig{{
		Name: "pub", URL: regURL,
		SyncMode:   config.RegistrySyncModeThin,
		TrustLevel: config.RegistryTrustLevelTrusted,
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
	if !res.EntitySync || res.Entities != 1 {
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
	if res.EntitySync || res.Entities != 0 {
		t.Errorf("SyncRegistry = %+v, want no entity sync", res)
	}
	if got := paths(); len(got) != 1 || got[0] != "/" {
		t.Errorf("registry served %v, want the manifest only", got)
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

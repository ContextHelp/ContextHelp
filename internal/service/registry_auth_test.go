package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	registrysync "github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// fakeRegistryToken is a fixed, non-secret value; failures may print it.
const fakeRegistryToken = "fake-registry-token"

// mapTokens maps a registry name to its token; each token is bound to
// the origin of the url the test registry runs at (set via bind).
type mapTokens map[string]string

func (m mapTokens) bind(url string) registrysync.TokenSource {
	return boundTokens{tokens: m, url: url}
}

type boundTokens struct {
	tokens mapTokens
	url    string
}

func (b boundTokens) Get(name string) (registrysync.StoredToken, error) {
	tok, ok := b.tokens[name]
	if !ok {
		return registrysync.StoredToken{}, registrysync.ErrNoToken{RegistryName: name}
	}
	origin, err := registrysync.CanonicalOrigin(b.url)
	if err != nil {
		return registrysync.StoredToken{}, err
	}
	return registrysync.StoredToken{Origin: origin, Token: tok}, nil
}

// paidRegistry answers every registry endpoint the service calls, only
// for requests carrying the bearer token, and counts unauthenticated ones.
type paidRegistry struct {
	*httptest.Server
	mu       sync.Mutex
	rejected []string
}

func newPaidRegistry(t *testing.T) *paidRegistry {
	t.Helper()
	p := &paidRegistry{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeRegistryToken {
			p.mu.Lock()
			p.rejected = append(p.rejected, r.URL.Path)
			p.mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/", "":
			_ = json.NewEncoder(w).Encode(storage.RegistryManifest{Name: "paid", Version: "1.0.0"})
		case "/entities/index":
			_ = json.NewEncoder(w).Encode([]registrysync.EntityIndexEntry{
				{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
			})
		case "/entities/ai.bert":
			_ = json.NewEncoder(w).Encode(storage.Entity{Slug: "ai.bert", Title: "BERT", Namespace: "ai"})
		case "/bookmarks/search":
			_ = json.NewEncoder(w).Encode([]registrysync.RemoteBookmark{{ID: "b1", Title: "One"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(p.Close)
	return p
}

func newAuthSvc(t *testing.T, regURL string, tokens registrysync.TokenSource) *Service {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	cfg := config.Config{Registries: []config.RegistryConfig{{
		Name:       "paid",
		URL:        regURL,
		Auth:       config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
		SyncMode:   config.RegistrySyncModeThin,
		TrustLevel: config.RegistryTrustLevelTrusted,
	}}}
	return NewWithOptions(driver, jobs.NewQueue(driver.Jobs()), pipeline.DefaultRegistry(),
		search.NewEngine(driver), "", nil, []Option{WithRegistryTokens(tokens)}, cfg)
}

func TestService_RegistryRequestsCarryStoredToken(t *testing.T) {
	reg := newPaidRegistry(t)
	svc := newAuthSvc(t, reg.URL, mapTokens{"paid": fakeRegistryToken}.bind(reg.URL))
	ctx := context.Background()

	if err := svc.FetchRegistry(ctx, reg.URL); err != nil {
		t.Errorf("FetchRegistry: %v", err)
	}
	if _, err := svc.DiffRegistrySync(ctx, reg.URL); err != nil {
		t.Errorf("DiffRegistrySync: %v", err)
	}
	if n, err := svc.SyncRegistryEntities(ctx, reg.URL); err != nil || n != 1 {
		t.Errorf("SyncRegistryEntities = %d, %v; want 1, nil", n, err)
	}
	if _, err := svc.SyncAllRegistriesWithReconciliation(ctx, "", nil); err != nil {
		t.Errorf("SyncAllRegistriesWithReconciliation: %v", err)
	}
	if err := svc.Store.Entities().SetContentStatus(ctx, "ai.bert", storage.ContentStatusThin); err != nil {
		t.Fatalf("mark thin: %v", err)
	}
	if _, err := svc.PullEntity(ctx, "ai.bert"); err != nil {
		t.Errorf("PullEntity: %v", err)
	}
	merged, raw, err := svc.SearchRemoteBookmarks(ctx, "q", 5*time.Second)
	if err != nil || len(merged) != 1 {
		t.Errorf("SearchRemoteBookmarks = %d results, %v (sources %+v); want 1", len(merged), err, raw)
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.rejected) != 0 {
		t.Errorf("requests without the token: %v", reg.rejected)
	}
}

func TestService_RegistryNoToken_LoginHint(t *testing.T) {
	reg := newPaidRegistry(t)
	svc := newAuthSvc(t, reg.URL, mapTokens{}.bind(reg.URL))
	ctx := context.Background()

	_, err := svc.SyncRegistryEntities(ctx, reg.URL)
	if err == nil || !strings.Contains(err.Error(), "ctxt registry login paid") {
		t.Errorf("SyncRegistryEntities without token: err = %v", err)
	}
	_, err = svc.DiffRegistrySync(ctx, reg.URL)
	if err == nil || !strings.Contains(err.Error(), "ctxt registry login paid") {
		t.Errorf("DiffRegistrySync without token: err = %v", err)
	}
}

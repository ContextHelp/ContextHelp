package stack

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// fakeRegistryToken is a fixed, non-secret value; failures may print it.
const fakeRegistryToken = "fake-registry-token"

// paidRegistry serves a manifest at / and an entity index at
// /entities/index, each only to a request carrying the bearer token, and
// records the paths it served.
func paidRegistry(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var served []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeRegistryToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		served = append(served, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/entities/index" {
			_ = json.NewEncoder(w).Encode([]registry.EntityIndexEntry{
				{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(storage.RegistryManifest{Name: "paid", Version: "1.0.0"})
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), served...)
	}
}

// paidStack builds a private serve stack whose config names the paid
// registry with bearer auth and reads secrets from the environment.
func paidStack(t *testing.T, url string) *Stack {
	t.Helper()
	cfg := &config.Config{}
	cfg.Secrets.Backend = "env"
	cfg.Registries = []config.RegistryConfig{{
		Name: "paid", URL: url,
		Auth:       config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
		TrustLevel: config.RegistryTrustLevelTrusted,
	}}
	return build(t, cfg, storageutil.NewTestDriver(t))
}

func post(t *testing.T, st *Stack, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	st.Router.ServeHTTP(rec, req)
	return rec
}

// TestServeSendsRegistryTokenFromSecrets: dpkms serve reads a registry's
// token from secrets.backend (here env) under registry/<name> and sends
// it on the manifest fetch and the entity sync routes.
func TestServeSendsRegistryTokenFromSecrets(t *testing.T) {
	srv, served := paidRegistry(t)
	origin, err := registry.CanonicalOrigin(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(registry.StoredToken{Origin: origin, Token: fakeRegistryToken})
	if err != nil {
		t.Fatal(err)
	}
	// env backend: key registry/paid is the variable REGISTRY_PAID.
	t.Setenv("REGISTRY_PAID", string(value))
	st := paidStack(t, srv.URL)
	body := `{"url":"` + srv.URL + `"}`

	if rec := post(t, st, "/api/v1/steps/registries/fetch", body); rec.Code != http.StatusOK {
		t.Fatalf("manifest fetch: status %d: %s", rec.Code, rec.Body)
	}
	if rec := post(t, st, "/api/v1/entities/registry-sync", body); rec.Code != http.StatusOK {
		t.Fatalf("entity sync: status %d: %s", rec.Code, rec.Body)
	}
	got := served()
	if len(got) != 2 || got[0] != "/" || got[1] != "/entities/index" {
		t.Errorf("registry served %v with the token, want [/ /entities/index]", got)
	}
}

// TestServeRegistryWithoutSecretNamesSecretSet: no secret stored, the
// registry refuses and the error names the dpkms command that stores one.
func TestServeRegistryWithoutSecretNamesSecretSet(t *testing.T) {
	srv, served := paidRegistry(t)
	t.Setenv("REGISTRY_PAID", "")
	st := paidStack(t, srv.URL)

	rec := post(t, st, "/api/v1/steps/registries/fetch", `{"url":"`+srv.URL+`"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("manifest fetch without a stored token succeeded")
	}
	if !strings.Contains(rec.Body.String(), "dpkms secret set registry/paid") {
		t.Errorf("error body %q lacks the dpkms remedy", rec.Body)
	}
	if len(served()) != 0 {
		t.Errorf("registry served %v without a token", served())
	}
}

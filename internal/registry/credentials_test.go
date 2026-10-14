package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
)

// fakeToken is the only token value these tests ever store; failure
// messages may print it because it is not a secret.
const fakeToken = "fake-registry-token"

// fakeTokens is an in-memory TokenSource that records every lookup.
type fakeTokens struct {
	mu     sync.Mutex
	tokens map[string]registry.StoredToken
	calls  int
}

// newFakeTokens takes (name, login url, token) triples: each token is
// bound to the origin of the url it was "logged in" against.
func newFakeTokens(kv ...string) *fakeTokens {
	f := &fakeTokens{tokens: map[string]registry.StoredToken{}}
	for i := 0; i+2 < len(kv); i += 3 {
		origin, err := registry.CanonicalOrigin(kv[i+1])
		if err != nil {
			panic(err)
		}
		f.tokens[kv[i]] = registry.StoredToken{Origin: origin, Token: kv[i+2]}
	}
	return f
}

func (f *fakeTokens) Get(name string) (registry.StoredToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	tok, ok := f.tokens[name]
	if !ok {
		return registry.StoredToken{}, registry.ErrNoToken{RegistryName: name}
	}
	return tok, nil
}

func (f *fakeTokens) lookups() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// headerLog records one header's value per request path.
type headerLog struct {
	mu   sync.Mutex
	seen map[string][]string
}

func (h *headerLog) add(path, v string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.seen == nil {
		h.seen = map[string][]string{}
	}
	h.seen[path] = append(h.seen[path], v)
}

func (h *headerLog) get(path string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seen[path]
}

var oneEntry = []registry.EntityIndexEntry{
	{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
}

// protectedRegistry serves the entity index only when header carries want;
// any other request gets 401.
func protectedRegistry(t *testing.T, header, want string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(header) != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(oneEntry)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func syncOne(t *testing.T, cfg config.RegistryConfig, tokens registry.TokenSource) (*registry.SyncResult, error) {
	t.Helper()
	creds := registry.NewCredentials([]config.RegistryConfig{cfg}, tokens)
	return registry.New(&stubEntityStore{}).WithCredentials(creds).Sync(context.Background(), cfg)
}

func TestSync_StoredTokenPerAuthType(t *testing.T) {
	cases := []struct {
		name   string
		auth   config.RegistryAuthConfig
		header string
		want   string
	}{
		{"bearer", config.RegistryAuthConfig{Type: config.RegistryAuthBearer}, "Authorization", "Bearer " + fakeToken},
		{"oauth2", config.RegistryAuthConfig{Type: config.RegistryAuthOAuth2}, "Authorization", "Bearer " + fakeToken},
		{"api_key default header", config.RegistryAuthConfig{Type: config.RegistryAuthAPIKey}, "X-API-Key", fakeToken},
		{"api_key custom header", config.RegistryAuthConfig{Type: config.RegistryAuthAPIKey, HeaderName: "X-Registry-Key"}, "X-Registry-Key", fakeToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := protectedRegistry(t, tc.header, tc.want)
			cfg := config.RegistryConfig{
				Name: "paid", URL: srv.URL, Auth: tc.auth,
				TrustLevel: config.RegistryTrustLevelTrusted,
			}
			res, err := syncOne(t, cfg, newFakeTokens("paid", cfg.URL, fakeToken))
			if err != nil {
				t.Fatalf("Sync with stored token: %v", err)
			}
			if res.Upserted != 1 {
				t.Errorf("Upserted = %d, want 1", res.Upserted)
			}
		})
	}
}

func TestSync_APIKeyDoesNotSendAuthorization(t *testing.T) {
	var log headerLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Path, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(oneEntry)
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL,
		Auth:       config.RegistryAuthConfig{Type: config.RegistryAuthAPIKey},
		TrustLevel: config.RegistryTrustLevelTrusted,
	}
	if _, err := syncOne(t, cfg, newFakeTokens("paid", cfg.URL, fakeToken)); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	for _, v := range log.get("/entities/index") {
		if v != "" {
			t.Errorf("api_key registry got an Authorization header")
		}
	}
}

func TestSync_AuthConfiguredNoToken_LoginHint(t *testing.T) {
	var log headerLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Path, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL,
		Auth: config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
	}
	_, err := syncOne(t, cfg, newFakeTokens())
	if err == nil {
		t.Fatal("Sync against a 401 registry with no stored token succeeded")
	}
	if !strings.Contains(err.Error(), "ctxt registry login paid") {
		t.Errorf("error lacks login hint: %v", err)
	}
	var noTok registry.ErrNoToken
	if !errors.As(err, &noTok) || noTok.RegistryName != "paid" {
		t.Errorf("error is not ErrNoToken for paid: %v", err)
	}
	if got := log.get("/entities/index"); len(got) != 1 || got[0] != "" {
		t.Errorf("request without stored token carried Authorization: %d requests", len(got))
	}
}

func TestSync_RejectedToken_LoginHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL,
		Auth: config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
	}
	_, err := syncOne(t, cfg, newFakeTokens("paid", cfg.URL, fakeToken))
	if err == nil {
		t.Fatal("Sync against a 403 registry succeeded")
	}
	var authErr registry.ErrRegistryAuth
	if !errors.As(err, &authErr) {
		t.Fatalf("error is not ErrRegistryAuth: %v", err)
	}
	if !authErr.TokenSent || authErr.StatusCode != http.StatusForbidden {
		t.Errorf("ErrRegistryAuth = %+v, want TokenSent and 403", authErr)
	}
	if !strings.Contains(err.Error(), "ctxt registry login paid") {
		t.Errorf("error lacks login hint: %v", err)
	}
	if strings.Contains(err.Error(), fakeToken) {
		t.Error("error message contains the token value")
	}
}

func TestSync_PublicRegistryUnauthenticated(t *testing.T) {
	var log headerLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Path, r.Header.Get("Authorization")+r.Header.Get("X-API-Key"))
		_ = json.NewEncoder(w).Encode(oneEntry)
	}))
	defer srv.Close()

	// A token is stored, but the registry declares no auth: nothing is
	// looked up and nothing is sent.
	tokens := newFakeTokens("public", srv.URL, fakeToken)
	cfg := config.RegistryConfig{Name: "public", URL: srv.URL, TrustLevel: config.RegistryTrustLevelTrusted}
	res, err := syncOne(t, cfg, tokens)
	if err != nil {
		t.Fatalf("Sync public registry: %v", err)
	}
	if res.Upserted != 1 {
		t.Errorf("Upserted = %d, want 1", res.Upserted)
	}
	if got := log.get("/entities/index"); len(got) != 1 || got[0] != "" {
		t.Errorf("public registry request carried credentials")
	}
	if n := tokens.lookups(); n != 0 {
		t.Errorf("token store consulted %d times for a registry without auth", n)
	}
}

func TestSync_PublicRegistry401_ConfigHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{Name: "public", URL: srv.URL}
	_, err := syncOne(t, cfg, newFakeTokens())
	if err == nil {
		t.Fatal("Sync against a 401 registry succeeded")
	}
	for _, want := range []string{"auth.type", "ctxt registry login public"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

func TestSync_CrossHostRedirectDropsToken(t *testing.T) {
	cases := []struct {
		name   string
		auth   config.RegistryAuthConfig
		header string
	}{
		{"bearer", config.RegistryAuthConfig{Type: config.RegistryAuthBearer}, "Authorization"},
		{"api_key", config.RegistryAuthConfig{Type: config.RegistryAuthAPIKey, HeaderName: "X-Registry-Key"}, "X-Registry-Key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var otherLog headerLog
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				otherLog.add(r.URL.Path, r.Header.Get(tc.header))
				_ = json.NewEncoder(w).Encode(oneEntry)
			}))
			defer other.Close()

			var regLog headerLog
			reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				regLog.add(r.URL.Path, r.Header.Get(tc.header))
				http.Redirect(w, r, other.URL+"/entities/index", http.StatusFound)
			}))
			defer reg.Close()

			cfg := config.RegistryConfig{
				Name: "paid", URL: reg.URL, Auth: tc.auth,
				TrustLevel: config.RegistryTrustLevelTrusted,
			}
			if _, err := syncOne(t, cfg, newFakeTokens("paid", cfg.URL, fakeToken)); err != nil {
				t.Fatalf("Sync: %v", err)
			}
			if got := regLog.get("/entities/index"); len(got) != 1 || got[0] == "" {
				t.Errorf("registry host did not get the token")
			}
			got := otherLog.get("/entities/index")
			if len(got) != 1 {
				t.Fatalf("redirect target got %d requests, want 1", len(got))
			}
			if got[0] != "" {
				t.Errorf("token followed a redirect to another host (%s: %q)", tc.header, got[0])
			}
		})
	}
}

func TestSync_SameHostRedirectKeepsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/entities/index" {
			http.Redirect(w, r, "/v2/entities/index", http.StatusFound)
			return
		}
		_ = json.NewEncoder(w).Encode(oneEntry)
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL,
		Auth:       config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
		TrustLevel: config.RegistryTrustLevelTrusted,
	}
	if _, err := syncOne(t, cfg, newFakeTokens("paid", cfg.URL, fakeToken)); err != nil {
		t.Fatalf("Sync through same-host redirect: %v", err)
	}
}

func TestSync_SharedHostPicksRegistryByPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := map[string]string{
			"/team/entities/index": "Bearer team-token",
			"/org/entities/index":  "Bearer org-token",
		}[r.URL.Path]
		if want == "" || r.Header.Get("Authorization") != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(oneEntry)
	}))
	defer srv.Close()

	bearer := config.RegistryAuthConfig{Type: config.RegistryAuthBearer}
	regs := []config.RegistryConfig{
		{Name: "team", URL: srv.URL + "/team", Auth: bearer, TrustLevel: config.RegistryTrustLevelTrusted},
		{Name: "org", URL: srv.URL + "/org", Auth: bearer, TrustLevel: config.RegistryTrustLevelTrusted},
	}
	creds := registry.NewCredentials(regs, newFakeTokens("team", regs[0].URL, "team-token", "org", regs[1].URL, "org-token"))
	syncer := registry.New(&stubEntityStore{}).WithCredentials(creds)
	for _, cfg := range regs {
		if _, err := syncer.Sync(context.Background(), cfg); err != nil {
			t.Errorf("Sync %s: %v", cfg.Name, err)
		}
	}
}

func TestSync_UnknownAuthTypeFails(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(oneEntry)
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL,
		Auth: config.RegistryAuthConfig{Type: "basic"},
	}
	_, err := syncOne(t, cfg, newFakeTokens("paid", cfg.URL, fakeToken))
	if err == nil || !strings.Contains(err.Error(), `unsupported auth.type "basic"`) {
		t.Fatalf("Sync with unknown auth.type: err = %v", err)
	}
	if hits != 0 {
		t.Errorf("request with unknown auth.type reached the registry")
	}
}

func TestMultiSync_TokenPerRegistry(t *testing.T) {
	a := protectedRegistry(t, "Authorization", "Bearer token-a")
	b := protectedRegistry(t, "X-API-Key", "token-b")
	regs := []config.RegistryConfig{
		{Name: "a", URL: a.URL, Auth: config.RegistryAuthConfig{Type: config.RegistryAuthBearer}},
		{Name: "b", URL: b.URL, Auth: config.RegistryAuthConfig{Type: config.RegistryAuthAPIKey}},
	}
	creds := registry.NewCredentials(regs, newFakeTokens("a", a.URL, "token-a", "b", b.URL, "token-b"))
	out, err := registry.New(&stubEntityStore{}).WithCredentials(creds).
		MultiSync(context.Background(), registry.MultiSyncConfig{Registries: regs})
	if err != nil {
		t.Fatalf("MultiSync: %v", err)
	}
	if len(out.OfflineRegistries) != 0 {
		t.Errorf("registries rejected: %v", out.OfflineRegistries)
	}
}

func TestSync_EntitlementRequestCarriesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/entitlements" {
			_ = json.NewEncoder(w).Encode(map[string]any{"plan": "pro", "namespaces": []string{"ai"}})
			return
		}
		_ = json.NewEncoder(w).Encode(oneEntry)
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL, EntitlementURL: srv.URL + "/entitlements",
		Auth:       config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
		TrustLevel: config.RegistryTrustLevelTrusted,
	}
	ents := newStubEntitlementStore()
	creds := registry.NewCredentials([]config.RegistryConfig{cfg}, newFakeTokens("paid", cfg.URL, fakeToken))
	_, err := registry.New(&stubEntityStore{}).WithEntitlements(ents).WithCredentials(creds).
		Sync(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Sync with entitlement check: %v", err)
	}
	if ents.records["paid"] == nil || ents.records["paid"].Plan != "pro" {
		t.Errorf("entitlement not stored: %+v", ents.records["paid"])
	}
}

func TestSync_EntitlementNoToken_LoginHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL, EntitlementURL: srv.URL + "/entitlements",
		Auth: config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
	}
	creds := registry.NewCredentials([]config.RegistryConfig{cfg}, newFakeTokens())
	_, err := registry.New(&stubEntityStore{}).WithEntitlements(newStubEntitlementStore()).
		WithCredentials(creds).Sync(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "ctxt registry login paid") {
		t.Fatalf("entitlement 401 without token: err = %v", err)
	}
}

func TestBookmarkSearch_CarriesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]registry.RemoteBookmark{{ID: "b1", Title: "One"}})
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{
		Name: "paid", URL: srv.URL,
		Auth: config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
	}
	creds := registry.NewCredentials([]config.RegistryConfig{cfg}, newFakeTokens("paid", cfg.URL, fakeToken))
	client := registry.NewBookmarkSearchClient(0).WithCredentials(creds)
	got, err := client.Search(context.Background(), srv.URL, "q")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("results = %d, want 1", len(got))
	}

	noTok := registry.NewBookmarkSearchClient(0).
		WithCredentials(registry.NewCredentials([]config.RegistryConfig{cfg}, newFakeTokens()))
	if _, err := noTok.Search(context.Background(), srv.URL, "q"); err == nil ||
		!strings.Contains(err.Error(), "ctxt registry login paid") {
		t.Errorf("Search without token: err = %v", err)
	}
}

// failingTokens fails the test on any lookup.
type failingTokens struct{ t *testing.T }

func (f failingTokens) Get(name string) (registry.StoredToken, error) {
	f.t.Errorf("token store consulted for %q, a registry without auth", name)
	return registry.StoredToken{}, registry.ErrNoToken{RegistryName: name}
}

func TestCredentials_NoAuthRegistryNeverLooksUpToken(t *testing.T) {
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(oneEntry)
	}))
	defer srv.Close()

	cfg := config.RegistryConfig{Name: "public", URL: srv.URL, TrustLevel: config.RegistryTrustLevelTrusted}
	if _, err := syncOne(t, cfg, failingTokens{t}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	// A refusal from a public registry must not look up a token either.
	status = http.StatusUnauthorized
	if _, err := syncOne(t, cfg, failingTokens{t}); err == nil {
		t.Fatal("Sync against 401 succeeded")
	}
}

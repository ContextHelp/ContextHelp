package http

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestUISession_CSRF(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()
	obj := &storage.KnowledgeObject{ID: "obj-csrf", Type: "note", RawContent: "x", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, f.bundle.svc.Store.Objects().Create(context.Background(), obj))

	cases := []struct {
		name     string
		opts     []reqOpt
		wantCode string
	}{
		{"no CSRF header", []reqOpt{fromUI, withoutHeader(HeaderCSRF)}, "CSRF_HEADER_REQUIRED"},
		{"cross-site", []reqOpt{fromUI, withHeader("Sec-Fetch-Site", "cross-site"), withHeader("Origin", "https://evil.example")}, "CROSS_ORIGIN_REQUEST"},
		{"other port same-site", []reqOpt{fromUI, withHeader("Sec-Fetch-Site", "same-site"), withHeader("Origin", "http://127.0.0.1:1")}, "CROSS_ORIGIN_REQUEST"},
		{"no browser headers", []reqOpt{withHeader(HeaderCSRF, "1")}, "CROSS_ORIGIN_REQUEST"},
		{"extension origin", []reqOpt{withHeader(HeaderCSRF, "1"), withHeader("Origin", "chrome-extension://abcdef")}, "CROSS_ORIGIN_REQUEST"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := f.do(http.MethodDelete, "/api/v1/objects/"+obj.ID, nil, append([]reqOpt{withCookie(c)}, tc.opts...)...)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
			assert.Equal(t, tc.wantCode, errCode(t, resp))
		})
	}
	t.Run("cross-site read", func(t *testing.T) {
		resp := f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), withHeader("Sec-Fetch-Site", "same-site"))
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
	t.Run("own page write passes", func(t *testing.T) {
		resp := f.do(http.MethodDelete, "/api/v1/objects/"+obj.ID, nil, withCookie(c), fromUI)
		assert.Less(t, resp.StatusCode, 300, "status %d", resp.StatusCode)
	})
}

func TestUISession_ScopeRefusals(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/audit-log"},
		{http.MethodPost, "/api/v1/federation/push"},
		{http.MethodPost, "/api/v1/mcp"},
		{http.MethodPost, "/api/v1/mcp/"},
		{http.MethodGet, "/api/v1/pipelines"},
		{http.MethodPost, "/api/v1/pipelines/enqueue"},
		{http.MethodGet, "/api/v1/steps"},
		{http.MethodGet, "/api/v1/steps/registries"},
		{http.MethodPost, "/api/v1/steps/registries/fetch"},
		{http.MethodPost, "/api/v1/inbox"},
		{http.MethodGet, "/api/v1/watches"},
		{http.MethodPatch, "/api/v1/objects/x"},
		{http.MethodGet, "/api/v1/no-such-route"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body any
			if tc.method != http.MethodGet {
				body = map[string]string{}
			}
			resp := f.do(tc.method, tc.path, body, withCookie(c), fromUI)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
			assert.Equal(t, "SESSION_SCOPE", errCode(t, resp))
		})
	}
}

// Every /api/v1 route must be classified: a new route without a class
// fails here instead of silently refusing (or serving) browser sessions.
func TestAPIRouteClassesCoverEveryRoute(t *testing.T) {
	f := newUIFixture(t)
	router := NewRouterWithConfig(f.bundle.svc, RouterConfig{
		Auth:     f.sessions.Provider(mustStatic(t)),
		Sessions: f.sessions,
	})
	seen := 0
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1") {
			return nil
		}
		seen++
		route = strings.TrimSuffix(route, "*")
		if _, ok := ClassifyRoute(method, route); !ok {
			t.Errorf("route %s %s has no class in apiRouteClasses", method, route)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, seen, 80, "walked the whole API")
}

func mustStatic(t *testing.T) *authn.StaticProvider {
	t.Helper()
	p, err := authn.NewStatic([]authn.StaticToken{{Token: uiOpsToken, Principal: "ops", Roles: []string{"admin"}}})
	require.NoError(t, err)
	return p
}

// The pattern the guard classifies must be the full route pattern, for
// nested and mounted routes alike.
func TestRoutePatternResolvesFullPattern(t *testing.T) {
	f := newUIFixture(t)
	router := NewRouterWithConfig(f.bundle.svc, RouterConfig{})
	for path, want := range map[string]string{
		"/api/v1/objects/abc":                   "/api/v1/objects/{id}",
		"/api/v1/entities/x/backlinks":          "/api/v1/entities/{slug}/backlinks",
		"/api/v1/steps/registries/u%2Fx/update": "/api/v1/steps/registries/{url}/update",
		"/api/v1/mcp/":                          "/api/v1/mcp/",
	} {
		method := http.MethodGet
		if strings.HasSuffix(want, "/update") {
			method = http.MethodPost
		}
		req := httptest.NewRequest(method, path, nil)
		rctx := chi.NewRouteContext()
		rctx.Routes = router
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		assert.Equal(t, want, routePattern(req), path)
	}
}

func TestUISession_EventStreamWithCookie(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()
	req, err := http.NewRequest(http.MethodGet, f.ts.URL+"/api/v1/events", nil)
	require.NoError(t, err)
	req.AddCookie(c)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Revoking the session ends the open stream.
	sid := decodeBody[uiSessionResponse](t, f.do(http.MethodGet, "/api/v1/ui/session", nil, withCookie(c), fromUI)).Session.ID
	require.NoError(t, f.sessions.Revoke(context.Background(), sid, authn.RevokeReasonRevoked))
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, bufio.NewReader(resp.Body))
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("event stream kept running after its session was revoked")
	}
}

// The routes a browser session must never reach are token-only in the
// table itself, not just refused further down.
func TestAPIRouteClasses_SensitiveRoutesTokenOnly(t *testing.T) {
	for _, key := range [][2]string{
		{http.MethodPost, "/api/v1/ui/login-codes"},
		{http.MethodGet, "/api/v1/audit-log"},
		{http.MethodPost, "/api/v1/federation/push"},
		{http.MethodPost, "/api/v1/mcp"},
		{http.MethodGet, "/api/v1/mcp/"},
		{http.MethodPost, "/api/v1/pipelines/enqueue"},
		{http.MethodPost, "/api/v1/steps/install"},
		{http.MethodGet, "/api/v1/steps/registries"},
		{http.MethodPost, "/api/v1/entities/registry-sync"},
		{http.MethodGet, "/api/v1/watches"},
	} {
		class, ok := ClassifyRoute(key[0], key[1])
		assert.True(t, ok, "%s %s unclassified", key[0], key[1])
		assert.Equal(t, RouteTokenOnly, class, "%s %s", key[0], key[1])
	}
}

// The mint handler refuses a session principal on its own, whatever the
// route table says.
func TestMintCodeRefusesSessionPrincipal(t *testing.T) {
	f := newUIFixture(t)
	routes := newUISessionRoutes(RouterConfig{Sessions: f.sessions})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ui/login-codes", nil)
	req.Header.Set("Authorization", "Bearer "+uiOpsToken)
	princ := &authn.Principal{ID: "ops", Meta: map[string]string{authn.MetaVia: authn.ViaSession, authn.MetaScope: authn.ScopeUI}}
	req = req.WithContext(authn.WithPrincipal(req.Context(), princ))
	rr := httptest.NewRecorder()
	routes.mintCode(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)
}

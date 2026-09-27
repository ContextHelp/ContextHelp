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

// A browser session holds its token's scopes intersected with the ui
// set: every route outside the set answers 403 INSUFFICIENT_SCOPE naming
// its scope, even for an admin token; the web UI's own routes answer.
func TestUISession_ScopeRefusals(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()
	for _, tc := range []struct {
		method, path string
		scope        authn.Scope
	}{
		{http.MethodGet, "/api/v1/audit-log", authn.ScopeAdminAudit},
		{http.MethodPost, "/api/v1/federation/push", authn.ScopeWriteObjects},
		{http.MethodPost, "/api/v1/mcp", authn.ScopeReadMCP},
		{http.MethodPost, "/api/v1/mcp/", authn.ScopeReadMCP},
		{http.MethodGet, "/api/v1/pipelines", authn.ScopeReadPipelines},
		{http.MethodPost, "/api/v1/pipelines/enqueue", authn.ScopeWriteObjects},
		{http.MethodGet, "/api/v1/steps", authn.ScopeReadPipelines},
		{http.MethodPost, "/api/v1/steps/registries/fetch", authn.ScopeSyncRegistries},
		{http.MethodPost, "/api/v1/inbox", authn.ScopeWriteInbox},
		{http.MethodGet, "/api/v1/watches", authn.ScopeReadWatches},
		{http.MethodPatch, "/api/v1/objects/x", authn.ScopeWriteObjects},
		{http.MethodDelete, "/api/v1/aliases/x", authn.ScopeDeleteAliases},
		{http.MethodDelete, "/api/v1/search-history", authn.ScopeDeleteSearches},
		{http.MethodPost, "/api/v1/ui/login-codes", authn.ScopeReadUI},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body any
			if tc.method != http.MethodGet {
				body = map[string]string{}
			}
			resp := f.do(tc.method, tc.path, body, withCookie(c), fromUI)
			require.Equal(t, http.StatusForbidden, resp.StatusCode)
			env := decodeBody[ErrorEnvelope](t, resp)
			assert.Equal(t, CodeInsufficientScope, env.Error.Code)
			assert.Equal(t, string(tc.scope), env.Error.Details["required_scope"])
			assert.Contains(t, env.Error.Message, "web UI session")
		})
	}

	// The admin token behind the session reaches them: the refusal is the
	// session's set, not the token's.
	resp := f.do(http.MethodGet, "/api/v1/audit-log", nil, withBearer(uiOpsToken))
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Past the scope check; the handler answers (the graph has no
	// embedding provider here).
	for _, path := range []string{"/api/v1/steps/registries", "/api/v1/search/graph?q=anything", "/api/v1/whoami", "/api/v1/objects"} {
		resp := f.do(http.MethodGet, path, nil, withCookie(c), fromUI)
		assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, resp.StatusCode, path)
	}
}

// Intersection: a session never holds a scope its token lacks. A reader
// token's session reads but cannot delete or retry, although both are
// in the ui set; a writer's may retry, not delete.
func TestUISession_ScopesFollowMintingToken(t *testing.T) {
	f := newUIFixture(t,
		authn.StaticToken{Token: uiOpsToken, Principal: "ops", Roles: []string{authn.RoleAdmin}},
		authn.StaticToken{Token: "tok-reader", Principal: "tablet", Roles: []string{authn.RoleReader}},
		authn.StaticToken{Token: "tok-writer", Principal: "phone", Roles: []string{authn.RoleWriter}},
	)
	signInAs := func(tok string) *http.Cookie {
		resp := f.exchange(f.mint(tok))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		return sessionCookieOf(t, resp)
	}
	reader, writer := signInAs("tok-reader"), signInAs("tok-writer")

	resp := f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(reader), fromUI)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	for _, tc := range []struct {
		cookie       *http.Cookie
		method, path string
		scope        authn.Scope
	}{
		{reader, http.MethodDelete, "/api/v1/objects/x", authn.ScopeDeleteObjects},
		{reader, http.MethodPost, "/api/v1/jobs/x/retry", authn.ScopeWriteJobs},
		{writer, http.MethodDelete, "/api/v1/objects/x", authn.ScopeDeleteObjects},
	} {
		resp := f.do(tc.method, tc.path, nil, withCookie(tc.cookie), fromUI)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s", tc.method, tc.path)
		assert.Equal(t, string(tc.scope), decodeBody[ErrorEnvelope](t, resp).Error.Details["required_scope"])
	}
	resp = f.do(http.MethodPost, "/api/v1/jobs/x/retry", nil, withCookie(writer), fromUI)
	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode, "a writer's session retries jobs")

	who := decodeBody[whoamiResponse](t, f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(reader), fromUI))
	assert.Equal(t, "tablet", who.Principal)
	assert.Equal(t, []string{authn.RoleReader}, who.Roles)
	assert.Equal(t, authn.SessionScopesFor(authn.ScopesForRoles([]string{authn.RoleReader}), authn.SessionKindUI), who.Scopes)
}

// One whoami for every caller: a token reports its role bundle, a
// session its narrowed set and the session itself, a private instance
// the local principal.
func TestWhoami_TokenSessionAndPrivate(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()

	tok := decodeBody[whoamiResponse](t, f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(uiOpsToken)))
	assert.Equal(t, viaToken, tok.Via)
	admin, _ := authn.Bundle(authn.RoleAdmin)
	assert.Equal(t, admin, tok.Scopes)
	assert.Nil(t, tok.Session)

	resp := f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(c), fromUI)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	sess := decodeBody[whoamiResponse](t, resp)
	assert.Equal(t, viaSession, sess.Via)
	assert.Equal(t, "ops", sess.Principal)
	assert.Equal(t, []string{authn.RoleAdmin}, sess.Roles)
	assert.Equal(t, authn.UISessionScopes, sess.Scopes)
	require.NotNil(t, sess.Session)
	assert.Equal(t, authn.SessionKindUI, sess.Session.Kind)
	assert.NotEmpty(t, sess.Session.ID)
	assert.True(t, sess.Session.ExpiresAt.After(sess.Session.IdleExpiresAt) || sess.Session.ExpiresAt.Equal(sess.Session.IdleExpiresAt))

	private := httptest.NewServer(NewRouterWithConfig(f.bundle.svc, RouterConfig{}))
	t.Cleanup(private.Close)
	res, err := http.Get(private.URL + "/api/v1/whoami")
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	local := decodeBody[whoamiResponse](t, res)
	assert.Equal(t, viaNone, local.Via)
	assert.Equal(t, "local", local.Principal)
	assert.Nil(t, local.Session)
}

// The pattern the guard resolves (to find event streams) must be the
// full route pattern, for nested and mounted routes alike.
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
	sid := decodeBody[whoamiResponse](t, f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(c), fromUI)).Session.ID
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

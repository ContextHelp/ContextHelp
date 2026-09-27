package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}

// fixedProvider authenticates any presented credential as one fixed
// principal: the route-coverage tests use it to call every route as a
// principal holding no scope at all.
type fixedProvider struct{ p *authn.Principal }

func (fixedProvider) Name() string { return "fixed" }

func (f fixedProvider) Authenticate(_ context.Context, cred authn.Credential) (*authn.Principal, error) {
	if cred.Empty() {
		return nil, authn.ErrNoCredential
	}
	out := *f.p
	return &out, nil
}

func newScopeTestService(t *testing.T) *service.Service {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	return service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
}

// walkAPIRoutes returns every "METHOD /api/v1/..." route the router
// serves.
func walkAPIRoutes(t *testing.T, r chi.Router) []string {
	t.Helper()
	var out []string
	require.NoError(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/v1/") {
			out = append(out, method+" "+route)
		}
		return nil
	}))
	require.NotEmpty(t, out, "walk found no /api/v1 routes")
	return out
}

// tableEntry finds the route-table entry serving a walked route.
func tableEntry(table []apiRoute, walked string) (apiRoute, bool) {
	method, pattern, _ := strings.Cut(walked, " ")
	for _, rt := range table {
		if "/api/v1"+rt.Pattern == pattern && (rt.Method == "" || rt.Method == method) {
			return rt, true
		}
	}
	return apiRoute{}, false
}

var urlParam = regexp.MustCompile(`\{[^}]+\}`)

// Every route the router serves under /api/v1 must come from the
// route-to-scope table and declare a known scope. The only exception is
// the federation push, which keeps its own credential rule.
func TestRouteCoverageEveryRouteDeclaresAScope(t *testing.T) {
	svc := newScopeTestService(t)
	rc := RouterConfig{Auth: fixedProvider{p: &authn.Principal{ID: "none"}}}
	table := apiRoutes(svc, rc)
	walked := walkAPIRoutes(t, NewRouterWithConfig(svc, rc))

	for _, w := range walked {
		rt, ok := tableEntry(table, w)
		if !assert.True(t, ok, "%s is served but missing from the route-to-scope table", w) {
			continue
		}
		assert.True(t, slices.Contains(authn.AllScopes, rt.Scope), "%s declares unknown scope %q", w, rt.Scope)
	}
}

// Behavioral half of the coverage check: a principal holding no scope is
// refused by every route with 403 naming the route's scope, so every
// route is really mounted behind RequireScope. Handlers never run.
func TestRouteCoverageScopelessPrincipalDeniedEverywhere(t *testing.T) {
	svc := newScopeTestService(t)
	rc := RouterConfig{Auth: fixedProvider{p: &authn.Principal{ID: "none"}}}
	table := apiRoutes(svc, rc)
	router := NewRouterWithConfig(svc, rc)

	for _, w := range walkAPIRoutes(t, router) {
		rt, ok := tableEntry(table, w)
		require.True(t, ok, "%s missing from the table", w)
		method, pattern, _ := strings.Cut(w, " ")
		// Bounded so a route that wrongly reaches a streaming handler
		// (SSE) fails the assertion instead of hanging the run.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req := httptest.NewRequestWithContext(ctx, method, urlParam.ReplaceAllString(pattern, "x"), nil)
		req.Header.Set("Authorization", "Bearer any")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		cancel()

		if !assert.Equal(t, http.StatusForbidden, rec.Code, "%s: scopeless principal got through", w) {
			continue
		}
		var env ErrorEnvelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), w)
		assert.Equal(t, CodeInsufficientScope, env.Error.Code, w)
		assert.Equal(t, string(rt.Scope), env.Error.Details["required_scope"], w)
		assert.Contains(t, env.Error.Message, string(rt.Scope), w)
	}
}

// roleServer is an authenticated router with one static token per role.
func roleServer(t *testing.T) *httptest.Server {
	t.Helper()
	provider, err := authn.NewStatic([]authn.StaticToken{
		{Token: "tok-admin", Principal: "owner", Roles: []string{authn.RoleAdmin}},
		{Token: "tok-writer", Principal: "phone", Roles: []string{authn.RoleWriter}},
		{Token: "tok-reader", Principal: "tablet", Roles: []string{authn.RoleReader}},
	})
	require.NoError(t, err)
	ts := httptest.NewServer(NewRouterWithConfig(newScopeTestService(t), RouterConfig{Auth: provider}))
	t.Cleanup(ts.Close)
	return ts
}

func roleDo(t *testing.T, method, url, token string, body any) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rd)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()
	return resp, out
}

var analyzeBody = map[string]string{"content": "scope test", "type": "text", "source": "test"}

// assertScopeDenied checks a 403 that names the missing scope.
func assertScopeDenied(t *testing.T, resp *http.Response, body []byte, scope authn.Scope) {
	t.Helper()
	require.Equal(t, http.StatusForbidden, resp.StatusCode, string(body))
	assert.Equal(t, CodeInsufficientScope, errorCode(t, body))
	assert.Contains(t, string(body), string(scope))
	assert.Contains(t, resp.Header.Get("WWW-Authenticate"), `error="insufficient_scope"`)
}

func TestReaderCannotWrite(t *testing.T) {
	ts := roleServer(t)

	resp, body := roleDo(t, http.MethodPost, ts.URL+"/api/v1/analyze", "tok-reader", analyzeBody)
	assertScopeDenied(t, resp, body, authn.ScopeWriteObjects)

	resp, _ = roleDo(t, http.MethodGet, ts.URL+"/api/v1/objects", "tok-reader", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestWriterWritesButCannotDeleteOrAdminister(t *testing.T) {
	ts := roleServer(t)

	resp, body := roleDo(t, http.MethodPost, ts.URL+"/api/v1/analyze", "tok-writer", analyzeBody)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode, string(body))

	resp, body = roleDo(t, http.MethodDelete, ts.URL+"/api/v1/objects/o-1", "tok-writer", nil)
	assertScopeDenied(t, resp, body, authn.ScopeDeleteObjects)

	resp, body = roleDo(t, http.MethodPost, ts.URL+"/api/v1/inbox/i-1/triage", "tok-writer", nil)
	assertScopeDenied(t, resp, body, authn.ScopeProcessInbox)

	resp, body = roleDo(t, http.MethodGet, ts.URL+"/api/v1/audit-log", "tok-writer", nil)
	assertScopeDenied(t, resp, body, authn.ScopeAdminAudit)

	resp, body = roleDo(t, http.MethodPost, ts.URL+"/api/v1/steps/install", "tok-writer", map[string]string{})
	assertScopeDenied(t, resp, body, authn.ScopeAdminPlugins)
}

func TestAdminPassesEveryScope(t *testing.T) {
	ts := roleServer(t)

	resp, body := roleDo(t, http.MethodPost, ts.URL+"/api/v1/analyze", "tok-admin", analyzeBody)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode, string(body))

	// Past the scope check: the handler answers for the missing object.
	resp, body = roleDo(t, http.MethodDelete, ts.URL+"/api/v1/objects/o-missing", "tok-admin", nil)
	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode, string(body))
	assert.NotEqual(t, http.StatusUnauthorized, resp.StatusCode, string(body))

	resp, body = roleDo(t, http.MethodGet, ts.URL+"/api/v1/audit-log", "tok-admin", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))
}

// 401 is a missing or invalid token; 403 is a valid token without the
// scope.
func TestMissingOrInvalidTokenIs401(t *testing.T) {
	ts := roleServer(t)

	resp, _ := roleDo(t, http.MethodGet, ts.URL+"/api/v1/objects", "", nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	resp, _ = roleDo(t, http.MethodGet, ts.URL+"/api/v1/objects", "tok-nope", nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// A private instance (no auth provider) grants every scope without a
// token and reports the local principal.
func TestPrivateInstanceNeedsNoToken(t *testing.T) {
	ts := httptest.NewServer(NewRouterWithConfig(newScopeTestService(t), RouterConfig{}))
	t.Cleanup(ts.Close)

	resp, body := roleDo(t, http.MethodPost, ts.URL+"/api/v1/analyze", "", analyzeBody)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode, string(body))

	resp, body = roleDo(t, http.MethodGet, ts.URL+"/api/v1/audit-log", "", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	resp, body = roleDo(t, http.MethodGet, ts.URL+"/api/v1/whoami", "", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var who whoamiResponse
	require.NoError(t, json.Unmarshal(body, &who))
	assert.Equal(t, "local", who.Principal)
	assert.Equal(t, authn.AllScopes, who.Scopes)
}

func TestWhoamiReportsPrincipalRolesScopes(t *testing.T) {
	ts := roleServer(t)

	for token, want := range map[string]struct {
		principal string
		role      string
	}{
		"tok-admin":  {"owner", authn.RoleAdmin},
		"tok-writer": {"phone", authn.RoleWriter},
		"tok-reader": {"tablet", authn.RoleReader},
	} {
		resp, body := roleDo(t, http.MethodGet, ts.URL+"/api/v1/whoami", token, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		var who whoamiResponse
		require.NoError(t, json.Unmarshal(body, &who))
		bundle, _ := authn.Bundle(want.role)
		assert.Equal(t, want.principal, who.Principal)
		assert.Equal(t, authn.ProviderStatic, who.Provider)
		assert.Equal(t, []string{want.role}, who.Roles)
		assert.Equal(t, bundle, who.Scopes, token)
	}

	resp, _ := roleDo(t, http.MethodGet, ts.URL+"/api/v1/whoami", "", nil)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// Federation push needs write:objects on top of its federation.token
// rule: a reader principal is refused before the handler runs; a writer
// reaches the handler (which then applies the federation credential).
func TestFederationPushRequiresWriteObjects(t *testing.T) {
	svc := newScopeTestService(t)
	for role, wantScopeDenied := range map[string]bool{authn.RoleReader: true, authn.RoleWriter: false, authn.RoleAdmin: false} {
		p := &authn.Principal{ID: role, Roles: []string{role}, Scopes: authn.ScopesForRoles([]string{role})}
		router := NewRouterWithConfig(svc, RouterConfig{Auth: fixedProvider{p: p}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/federation/push", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer any")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		denied := rec.Code == http.StatusForbidden && strings.Contains(rec.Body.String(), "INSUFFICIENT_SCOPE")
		assert.Equal(t, wantScopeDenied, denied, "%s: status %d body %s", role, rec.Code, rec.Body.String())
	}
}

// uiSessionRoutes is every route a web UI browser session can reach:
// the routes whose scope is in authn.UISessionScopes. Pinned here so a
// new route declaring a ui-set scope, or a change to the set, fails
// until someone decides a browser session should have it.
var uiSessionRouteSet = []string{
	"GET /api/v1/whoami",
	"GET /api/v1/objects",
	"GET /api/v1/objects/facets",
	"GET /api/v1/objects/{id}",
	"GET /api/v1/objects/{id}/related",
	"DELETE /api/v1/objects/{id}",
	"GET /api/v1/jobs",
	"GET /api/v1/jobs/{id}",
	"POST /api/v1/jobs/{id}/retry",
	"GET /api/v1/search",
	"POST /api/v1/find",
	"GET /api/v1/search/graph",
	"GET /api/v1/entities",
	"GET /api/v1/entities/{slug}",
	"GET /api/v1/entities/{slug}/backlinks",
	"GET /api/v1/entities/resolve",
	"GET /api/v1/steps/registries",
	"GET /api/v1/feeds",
	"GET /api/v1/import/{id}",
	"GET /api/v1/importers/runs/{id}",
	"GET /api/v1/system/reminders",
	"GET /api/v1/inbox",
	"GET /api/v1/inbox/queue",
	"GET /api/v1/events",
	"GET /api/v1/suggestions",
	"GET /api/v1/aliases",
	"GET /api/v1/aliases/{alias}",
	"GET /api/v1/capture/recent",
	"GET /api/v1/saved-searches",
	"GET /api/v1/saved-searches/{name}",
	"GET /api/v1/search-history",
	"DELETE /api/v1/ui/session",
}

// The ui scope set, applied to the route table, yields exactly the web
// UI's routes: its reads, its own writes (delete an object, retry a job,
// sign out) and the registry list. The routes a session must never
// reach declare scopes outside the set.
func TestUISessionRouteSetPinned(t *testing.T) {
	svc := newScopeTestService(t)
	rc := RouterConfig{Auth: fixedProvider{p: &authn.Principal{ID: "none"}}}
	table := apiRoutes(svc, rc)

	var got []string
	for _, w := range walkAPIRoutes(t, NewRouterWithConfig(svc, rc)) {
		rt, ok := tableEntry(table, w)
		require.True(t, ok, "%s missing from the table", w)
		if slices.Contains(authn.UISessionScopes, rt.Scope) {
			got = append(got, w)
		}
	}
	assert.ElementsMatch(t, uiSessionRouteSet, got)

	// Minting takes read:ui, which every role holds and no session; the
	// sign-out route takes signout:ui, which only sessions hold.
	mint, _ := tableEntry(table, "POST /api/v1/ui/login-codes")
	assert.Equal(t, authn.ScopeReadUI, mint.Scope)
	signOut, _ := tableEntry(table, "DELETE /api/v1/ui/session")
	assert.Equal(t, authn.ScopeSignoutUI, signOut.Scope)

	for _, w := range []string{
		"POST /api/v1/ui/login-codes",
		"GET /api/v1/audit-log",
		"POST /api/v1/federation/push",
		"POST /api/v1/mcp", "GET /api/v1/mcp/",
		"GET /api/v1/watches", "GET /api/v1/watches/{id}/files",
		"GET /api/v1/pipelines", "GET /api/v1/steps",
		"POST /api/v1/pipelines/enqueue", "POST /api/v1/steps/install",
		"POST /api/v1/steps/registries/fetch", "POST /api/v1/entities/registry-sync",
		"DELETE /api/v1/aliases/{alias}", "DELETE /api/v1/saved-searches/{name}", "DELETE /api/v1/search-history",
		"PATCH /api/v1/objects/{id}", "POST /api/v1/analyze",
	} {
		rt, ok := tableEntry(table, w)
		require.True(t, ok, "%s missing from the table", w)
		assert.False(t, slices.Contains(authn.UISessionScopes, rt.Scope), "%s: scope %s is in the ui set", w, rt.Scope)
	}
}

// Behavioral half: a session principal holding the whole ui set is
// refused by every route outside the pinned set with 403 naming the
// route's scope. Handlers never run.
func TestUISessionRefusedOutsideRouteSet(t *testing.T) {
	svc := newScopeTestService(t)
	sess := &authn.Principal{
		ID: "ops", Roles: []string{authn.RoleAdmin}, Scopes: authn.UISessionScopes,
		Meta: map[string]string{authn.MetaVia: authn.ViaSession, authn.MetaSessionKind: authn.SessionKindUI},
	}
	rc := RouterConfig{Auth: fixedProvider{p: sess}}
	table := apiRoutes(svc, rc)
	router := NewRouterWithConfig(svc, rc)

	refused := 0
	for _, w := range walkAPIRoutes(t, router) {
		if slices.Contains(uiSessionRouteSet, w) {
			continue
		}
		rt, _ := tableEntry(table, w)
		method, pattern, _ := strings.Cut(w, " ")
		req := httptest.NewRequest(method, urlParam.ReplaceAllString(pattern, "x"), strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer any")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set(HeaderCSRF, "1")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if !assert.Equal(t, http.StatusForbidden, rec.Code, "%s: session got through", w) {
			continue
		}
		var env ErrorEnvelope
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), w)
		assert.Equal(t, CodeInsufficientScope, env.Error.Code, w)
		assert.Equal(t, string(rt.Scope), env.Error.Details["required_scope"], w)
		refused++
	}
	assert.Greater(t, refused, 50, "walked the routes outside the ui set")
}

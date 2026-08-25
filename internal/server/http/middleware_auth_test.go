package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
)

func testProvider(t *testing.T) authn.Provider {
	t.Helper()
	p, err := authn.NewStatic([]authn.StaticToken{
		{Token: "tok-valid", Principal: "ops", Roles: []string{"admin"}},
	})
	require.NoError(t, err)
	return p
}

func authedEcho(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		princ, ok := authn.FromContext(r.Context())
		require.True(t, ok, "handler must see the authenticated principal")
		w.Header().Set("X-Test-Principal", princ.ID)
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireAuthMissingCredential(t *testing.T) {
	h := RequireAuth(testProvider(t), nil)(authedEcho(t))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/search", nil))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("WWW-Authenticate"))
}

func TestRequireAuthInvalidBearer(t *testing.T) {
	h := RequireAuth(testProvider(t), nil)(authedEcho(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuthValidBearer(t *testing.T) {
	h := RequireAuth(testProvider(t), nil)(authedEcho(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	req.Header.Set("Authorization", "Bearer tok-valid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ops", rec.Header().Get("X-Test-Principal"))
}

func TestRequireAuthValidAPIKey(t *testing.T) {
	h := RequireAuth(testProvider(t), nil)(authedEcho(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	req.Header.Set("X-API-Key", "tok-valid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ops", rec.Header().Get("X-Test-Principal"))
}

// The secured router guards the whole /api/v1 table (MCP mount included)
// while health endpoints stay reachable for probes.
func TestSecuredRouterGuardsAPI(t *testing.T) {
	driverBundle := newTestServerBundle(t)
	defer driverBundle.Close()

	router := NewRouterWithConfig(driverBundle.svc, RouterConfig{Auth: testProvider(t)})
	ts := httptest.NewServer(router)
	defer ts.Close()

	for _, path := range []string{"/api/v1/search?q=x", "/api/v1/objects", "/api/v1/mcp"} {
		req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "path %s must require auth", path)
	}

	// Health endpoints stay open for load-balancer probes.
	resp, err := http.Get(ts.URL + "/health")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Valid credential passes through to the route table.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/objects", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok-valid")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// A router constructed without auth (private instance) keeps every
// route open — existing behavior unchanged.
func TestUnsecuredRouterStaysOpen(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/objects")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

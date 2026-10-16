package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// viewerProbe records the viewer the wrapped handler sees.
func viewerProbe(t *testing.T) (http.Handler, func() (sharing.Viewer, bool)) {
	t.Helper()
	var seen sharing.Viewer
	var ok bool
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, ok = sharing.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	return h, func() (sharing.Viewer, bool) { return seen, ok }
}

// A static-token principal authenticated by RequireAuth reads
// unscoped: the viewer is attached in the same step as the principal.
func TestRequireAuthAttachesUnrestrictedViewer(t *testing.T) {
	probe, seen := viewerProbe(t)
	h := RequireAuth(testChain(t), nil, false, nil)(probe)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	req.Header.Set("Authorization", "Bearer tok-valid-fixture-0123456789abcdef0123456789")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	v, ok := seen()
	require.True(t, ok, "authenticated handler must see a viewer")
	assert.True(t, v.IsUnrestricted())
}

// A rejected request never reaches the handler, so no viewer leaks
// past a failed authentication.
func TestRequireAuthRejectedAttachesNoViewer(t *testing.T) {
	probe, seen := viewerProbe(t)
	h := RequireAuth(testChain(t), nil, false, nil)(probe)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/search", nil))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	_, ok := seen()
	assert.False(t, ok)
}

func TestWithViewerAttachesGivenViewer(t *testing.T) {
	probe, seen := viewerProbe(t)
	h := WithViewer(sharing.Unrestricted)(probe)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/search", nil))

	require.Equal(t, http.StatusNoContent, rec.Code)
	v, ok := seen()
	require.True(t, ok)
	assert.True(t, v.IsUnrestricted())
}

// apiMiddlewares returns the middleware chain the router runs ahead of
// a /api/v1 route: chi.Walk accumulates the parent and sub-router
// stacks per route, so the chain is exactly what the handler sits
// behind in production.
func apiMiddlewares(t *testing.T, r chi.Router) chi.Middlewares {
	t.Helper()
	var chain chi.Middlewares
	found := false
	require.NoError(t, chi.Walk(r, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		if found || method != http.MethodGet || !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}
		chain, found = chi.Middlewares(mws), true
		return nil
	}))
	require.True(t, found, "walk found no GET /api/v1 route")
	return chain
}

// A private instance (no auth provider) has no principal to resolve,
// yet every /api/v1 handler must still see the owner's viewer.
func TestPrivateRouterAttachesUnrestrictedViewer(t *testing.T) {
	router := NewRouterWithConfig(newScopeTestService(t), RouterConfig{})
	probe, seen := viewerProbe(t)
	h := apiMiddlewares(t, router).Handler(probe)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil))

	require.Equal(t, http.StatusNoContent, rec.Code)
	v, ok := seen()
	require.True(t, ok, "private /api/v1 handler must see a viewer")
	assert.True(t, v.IsUnrestricted())
}

// With a provider, the same chain attaches the viewer only after the
// credential checks out.
func TestAuthedRouterAttachesViewerAfterAuth(t *testing.T) {
	router := NewRouterWithConfig(newScopeTestService(t), RouterConfig{Auth: testChain(t)})
	chain := apiMiddlewares(t, router)

	probe, seen := viewerProbe(t)
	rec := httptest.NewRecorder()
	chain.Handler(probe).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	_, ok := seen()
	assert.False(t, ok, "unauthenticated request must not reach a viewer")

	probe, seen = viewerProbe(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer tok-valid-fixture-0123456789abcdef0123456789")
	rec = httptest.NewRecorder()
	chain.Handler(probe).ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
	v, ok := seen()
	require.True(t, ok)
	assert.True(t, v.IsUnrestricted())
}

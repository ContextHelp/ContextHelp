package http

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/ui"
)

// shellMarker is the SPA mount point every copy of index.html carries.
const shellMarker = `<div id="root">`

func uiRouter(t *testing.T) http.Handler {
	t.Helper()
	bundle := newTestServerBundle(t)
	t.Cleanup(bundle.Close)
	return NewRouterWithConfig(bundle.svc, RouterConfig{})
}

func serveUI(router http.Handler, method, target string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(method, target, nil))
	return rr
}

// Loading a client route directly (bookmark, reload, a link from the
// search-graph viewer) must hand back the SPA shell so the client
// router can resolve it, however deep the route is.
func TestUIClientRoutesServeShell(t *testing.T) {
	router := uiRouter(t)

	for _, target := range []string{
		"/ui/search",
		"/ui/objects",
		"/ui/objects/3f2b8f0e-6f7c-4a53-9d8e-0d1c2b3a4f5e",
		"/ui/jobs/3f2b8f0e-6f7c-4a53-9d8e-0d1c2b3a4f5e",
		"/ui/objects/a%2Fb",
		"/ui/objects/some-id/",
		"/ui/objects/some-id?tab=links",
	} {
		t.Run(target, func(t *testing.T) {
			rr := serveUI(router, http.MethodGet, target)
			assert.Equal(t, http.StatusOK, rr.Code)
			assert.True(t, strings.HasPrefix(rr.Header().Get("Content-Type"), "text/html"), rr.Header().Get("Content-Type"))
			assert.Contains(t, rr.Body.String(), shellMarker)
		})
	}
}

// HEAD answers like GET, minus the body.
func TestUIClientRouteHead(t *testing.T) {
	router := uiRouter(t)

	rr := serveUI(router, http.MethodHead, "/ui")
	assert.Equal(t, http.StatusMovedPermanently, rr.Code)

	rr = serveUI(router, http.MethodHead, "/ui/objects/some-id")
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, strings.HasPrefix(rr.Header().Get("Content-Type"), "text/html"))
	assert.Empty(t, rr.Body.String())

	rr = serveUI(router, http.MethodHead, "/ui/assets/missing.js")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// A broken asset reference must stay a 404: serving the shell as
// JavaScript or CSS would hide the breakage behind a MIME error.
func TestUIMissingAssetsStay404(t *testing.T) {
	router := uiRouter(t)

	for _, target := range []string{
		"/ui/assets/missing.js",
		"/ui/assets/missing",
		"/ui/assets/",
		"/ui/assets",
		"/ui/missing.css",
		"/ui/icons/icon-192.png",
		"/ui/objects/favicon.svg",
	} {
		t.Run(target, func(t *testing.T) {
			rr := serveUI(router, http.MethodGet, target)
			assert.Equal(t, http.StatusNotFound, rr.Code)
			assert.NotContains(t, rr.Body.String(), shellMarker)
		})
	}
}

// The mount root and every embedded file keep working as before.
func TestUIRootAndEmbeddedFiles(t *testing.T) {
	router := uiRouter(t)

	rr := serveUI(router, http.MethodGet, "/ui")
	assert.Equal(t, http.StatusMovedPermanently, rr.Code)
	assert.Equal(t, "/ui/", rr.Header().Get("Location"))

	rr = serveUI(router, http.MethodGet, "/ui/")
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), shellMarker)

	dist, err := fs.Sub(ui.FS, "dist")
	require.NoError(t, err)
	assets, err := fs.Glob(dist, "assets/*")
	require.NoError(t, err)
	require.NotEmpty(t, assets, "embedded bundle has no assets")

	for _, name := range append([]string{"favicon.svg"}, assets...) {
		want, err := fs.ReadFile(dist, name)
		require.NoError(t, err)
		rr := serveUI(router, http.MethodGet, "/ui/"+name)
		assert.Equal(t, http.StatusOK, rr.Code, name)
		assert.Equal(t, string(want), rr.Body.String(), name)
	}
}

// Deep links and missing assets carry the same anti-framing headers
// as the rest of /ui.
func TestUIFallbackCarriesSecurityHeaders(t *testing.T) {
	router := uiRouter(t)

	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/ui/objects/some-id"},
		{http.MethodHead, "/ui/objects/some-id"},
		{http.MethodGet, "/ui/assets/missing.js"},
	} {
		rr := serveUI(router, tc.method, tc.target)
		h := rr.Header()
		assert.Equal(t, "frame-ancestors 'none'", h.Get("Content-Security-Policy"), tc)
		assert.Equal(t, "DENY", h.Get("X-Frame-Options"), tc)
		assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"), tc)
		assert.Equal(t, "no-referrer", h.Get("Referrer-Policy"), tc)
	}
}

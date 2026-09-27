package http

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/searchgraph/viewer"
)

var viewerConfigElem = regexp.MustCompile(`<script type="application/json" id="viewer-config">([^<]*)</script>`)

// The page at /ui/searchgraph/ is the viewer, not the SPA shell, and it
// carries the dpkms host config: the REST graph endpoint on this origin
// with the page's query forwarded, and object clicks linking to the web
// UI's object page. The values are spelled out, not read back from
// SearchGraphViewerConfig, so a changed config fails here.
func TestSearchGraphViewerIndexCarriesHostConfig(t *testing.T) {
	router := uiRouter(t)

	for _, target := range []string{"/ui/searchgraph/", "/ui/searchgraph/?q=deploy&limit=3"} {
		t.Run(target, func(t *testing.T) {
			rr := serveUI(router, http.MethodGet, target)
			require.Equal(t, http.StatusOK, rr.Code)
			assert.True(t, strings.HasPrefix(rr.Header().Get("Content-Type"), "text/html"), rr.Header().Get("Content-Type"))
			body := rr.Body.String()
			assert.NotContains(t, body, shellMarker)
			assert.Contains(t, body, `<script src="viewer.js"></script>`)

			m := viewerConfigElem.FindStringSubmatch(body)
			require.NotNil(t, m, "no injected viewer config")
			var got map[string]any
			require.NoError(t, json.Unmarshal([]byte(m[1]), &got))
			assert.Equal(t, map[string]any{
				"dataUrl":      "/api/v1/search/graph",
				"forwardQuery": true,
				"objectAction": "link",
				"objectHref":   "/ui/objects/{id}",
			}, got)
		})
	}
}

// The script, styles and notices are the embedded files, byte for byte,
// with the types nosniff needs.
func TestSearchGraphViewerServesAssets(t *testing.T) {
	router := uiRouter(t)

	for name, ctype := range map[string]string{
		viewer.ScriptFile:  "text/javascript",
		viewer.StyleFile:   "text/css",
		viewer.NoticesFile: "text/plain",
	} {
		want, err := fs.ReadFile(viewer.Assets(), name)
		require.NoError(t, err)
		rr := serveUI(router, http.MethodGet, "/ui/searchgraph/"+name)
		assert.Equal(t, http.StatusOK, rr.Code, name)
		assert.True(t, strings.HasPrefix(rr.Header().Get("Content-Type"), ctype), "%s: %s", name, rr.Header().Get("Content-Type"))
		assert.Equal(t, string(want), rr.Body.String(), name)
	}

	rr := serveUI(router, http.MethodHead, "/ui/searchgraph/")
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Body.String())
	rr = serveUI(router, http.MethodHead, "/ui/searchgraph/"+viewer.ScriptFile)
	assert.Equal(t, http.StatusOK, rr.Code)
}

// Nothing under /ui/searchgraph/ falls back to the SPA shell: a missing
// viewer file, a graph.json the dpkms host never serves, or a deeper
// path is a 404.
func TestSearchGraphViewerMissingFilesStay404(t *testing.T) {
	router := uiRouter(t)

	for _, target := range []string{
		"/ui/searchgraph/missing.js",
		"/ui/searchgraph/" + viewer.DataFile,
		"/ui/searchgraph/assets/index.js",
		"/ui/searchgraph/objects/some-id",
	} {
		t.Run(target, func(t *testing.T) {
			rr := serveUI(router, http.MethodGet, target)
			assert.Equal(t, http.StatusNotFound, rr.Code)
			assert.NotContains(t, rr.Body.String(), shellMarker)
			assert.NotContains(t, rr.Body.String(), `id="viewer-config"`)
		})
	}
}

// /ui/searchgraph redirects to the viewer with its query intact, so a
// link without the trailing slash still renders the same search.
func TestSearchGraphViewerRedirectKeepsQuery(t *testing.T) {
	router := uiRouter(t)

	for target, want := range map[string]string{
		"/ui/searchgraph":                      "/ui/searchgraph/",
		"/ui/searchgraph?q=deploy%20x&limit=3": "/ui/searchgraph/?q=deploy%20x&limit=3",
		"/ui/searchgraph?q=%2F%2Fevil.example": "/ui/searchgraph/?q=%2F%2Fevil.example",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rr := serveUI(router, method, target)
			assert.Equal(t, http.StatusMovedPermanently, rr.Code, method+" "+target)
			got, err := url.Parse(rr.Header().Get("Location"))
			require.NoError(t, err)
			wantURL, err := url.Parse(want)
			require.NoError(t, err)
			assert.Empty(t, got.Host, "redirect must stay same-origin")
			assert.Equal(t, wantURL.Path, got.Path, method+" "+target)
			assert.Equal(t, wantURL.Query(), got.Query(), method+" "+target)
		}
	}

	// index.html answers with the file server's canonical redirect to
	// the directory; the page then loads from there.
	rr := serveUI(router, http.MethodGet, "/ui/searchgraph/index.html")
	assert.Equal(t, http.StatusMovedPermanently, rr.Code)
	assert.Equal(t, "./", rr.Header().Get("Location"))
}

// The viewer mount leaves the SPA alone: its root, client routes and
// missing-asset 404s behave as before, including paths that merely
// start with "searchgraph".
func TestSearchGraphViewerLeavesSPAFallback(t *testing.T) {
	router := uiRouter(t)

	for _, target := range []string{"/ui/", "/ui/objects/some-id", "/ui/searchgraphs", "/ui/search"} {
		rr := serveUI(router, http.MethodGet, target)
		assert.Equal(t, http.StatusOK, rr.Code, target)
		assert.Contains(t, rr.Body.String(), shellMarker, target)
	}
	rr := serveUI(router, http.MethodGet, "/ui/assets/missing.js")
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// Every viewer response carries the /ui anti-framing headers, and the
// page keeps its own Content-Security-Policy: the header forbids framing
// (the one directive a meta element cannot carry) and sets no fetch
// directive, so the meta policy alone decides what the page may load,
// and its connect-src 'self' is what lets the page fetch /api/v1.
func TestSearchGraphViewerSecurityHeaders(t *testing.T) {
	router := uiRouter(t)

	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/ui/searchgraph"},
		{http.MethodGet, "/ui/searchgraph/"},
		{http.MethodHead, "/ui/searchgraph/"},
		{http.MethodGet, "/ui/searchgraph/" + viewer.ScriptFile},
		{http.MethodGet, "/ui/searchgraph/missing.js"},
	} {
		rr := serveUI(router, tc.method, tc.target)
		h := rr.Header()
		assert.Equal(t, "frame-ancestors 'none'", h.Get("Content-Security-Policy"), tc)
		assert.Equal(t, "DENY", h.Get("X-Frame-Options"), tc)
		assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"), tc)
		assert.Equal(t, "no-referrer", h.Get("Referrer-Policy"), tc)
	}

	body := serveUI(router, http.MethodGet, "/ui/searchgraph/").Body.String()
	assert.Contains(t, body, `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data: blob:; base-uri 'none'; form-action 'none'">`)
}

// On an instance with inbound auth the page itself stays open, like the
// rest of /ui, while the graph it fetches answers 401 without a
// credential and passes auth with one: the page holds no data.
func TestSearchGraphViewerPageOpenDataGuarded(t *testing.T) {
	bundle := newTestServerBundle(t)
	t.Cleanup(bundle.Close)
	router := NewRouterWithConfig(bundle.svc, RouterConfig{Auth: testProvider(t)})

	rr := serveUI(router, http.MethodGet, "/ui/searchgraph/?q=deploy")
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `id="viewer-config"`)

	rr = serveUI(router, http.MethodGet, "/api/v1/search/graph?q=deploy")
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/graph?q=deploy", nil)
	req.Header.Set("Authorization", "Bearer tok-valid")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	// Past auth; this bare test service may then refuse the search
	// itself (no embedding model), which is not the auth layer's 401.
	assert.NotEqual(t, http.StatusUnauthorized, rr.Code, rr.Body.String())
}

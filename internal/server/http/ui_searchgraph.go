package http

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/searchgraph/viewer"
)

// SearchGraphViewerPath is where the embedded search-graph viewer is
// served. A page load at SearchGraphViewerPath+"?q=<query>" renders
// GET /api/v1/search/graph for that query.
const SearchGraphViewerPath = "/ui/searchgraph/"

// SearchGraphViewerConfig is the host config the served viewer carries:
// fetch the graph from the REST endpoint on the same origin with the
// page's own query string, and link a clicked object to the web UI's
// object page. The fetch is same-origin, so it carries whatever
// credential the browser holds for /api/v1.
func SearchGraphViewerConfig() viewer.Config {
	return viewer.Config{
		DataURL:      "/api/v1/search/graph",
		ForwardQuery: true,
		ObjectAction: viewer.ObjectLink,
		ObjectHref:   "/ui/objects/" + viewer.IDPlaceholder,
	}
}

// SearchGraphViewer serves the viewer's files under a stripped
// SearchGraphViewerPath: the page with SearchGraphViewerConfig injected,
// and its script, styles and notices as-is. Anything else is a 404,
// never the SPA shell, so a broken asset reference fails loudly.
func SearchGraphViewer() (http.Handler, error) {
	assets, err := viewer.AssetsWith(SearchGraphViewerConfig())
	if err != nil {
		return nil, err
	}
	return http.FileServerFS(assets), nil
}

// mountSearchGraphViewer routes GET|HEAD SearchGraphViewerPath and
// everything under it to SearchGraphViewer, ahead of the /ui/* SPA
// fallback (chi prefers the longer static prefix), and redirects the
// slashless path to it with the query kept. The /ui security headers
// apply as on every /ui response; the page adds its own stricter
// Content-Security-Policy in a meta element.
func mountSearchGraphViewer(r chi.Router) {
	h, err := SearchGraphViewer()
	if err != nil {
		panic("searchgraph viewer: " + err.Error())
	}
	slashless := strings.TrimSuffix(SearchGraphViewerPath, "/")
	files := http.StripPrefix(slashless, h)
	redirect := func(w http.ResponseWriter, req *http.Request) {
		target := SearchGraphViewerPath
		if req.URL.RawQuery != "" {
			target += "?" + req.URL.RawQuery
		}
		http.Redirect(w, req, target, http.StatusMovedPermanently)
	}
	r.Get(slashless, redirect)
	r.Head(slashless, redirect)
	r.Get(SearchGraphViewerPath+"*", files.ServeHTTP)
	r.Head(SearchGraphViewerPath+"*", files.ServeHTTP)
}

package http

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// uiShell is the SPA entry document at the root of the embedded bundle.
const uiShell = "index.html"

// uiAssetDir holds the bundler's hashed output (vite build.assetsDir).
const uiAssetDir = "assets"

// SPAHandler serves the embedded web UI mounted under a stripped
// prefix: existing files as-is, and the SPA shell for any other path
// so the client router can resolve deep links (/objects/<id>,
// /jobs/<id>) on a direct load or reload.
//
// Requests that look like assets never fall back: a path under
// assets/ or whose last segment has a file extension answers 404, so a
// broken asset reference fails loudly instead of receiving HTML.
func SPAHandler(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			files.ServeHTTP(w, r)
			return
		}
		if st, err := fs.Stat(dist, name); err == nil && !st.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		if isUIAssetPath(name) {
			http.NotFound(w, r)
			return
		}
		http.ServeFileFS(w, r, dist, uiShell)
	})
}

// isUIAssetPath reports whether a cleaned, root-relative UI path names
// a static asset rather than a client route.
func isUIAssetPath(name string) bool {
	if name == uiAssetDir || strings.HasPrefix(name, uiAssetDir+"/") {
		return true
	}
	return path.Ext(name) != ""
}

package http

import (
	"mime"
	"net/http"
	"strings"
)

// IsExtensionOrigin reports whether origin is a browser extension's.
// Extensions are trusted the same way CORS trusts them (see
// corsAllowedPrefixes): they hold host permissions for dpkms and can
// reach it whatever this server answers. The cookie bridge admits only
// these origins.
func IsExtensionOrigin(origin string) bool {
	for _, prefix := range corsAllowedPrefixes {
		if strings.HasPrefix(origin, prefix) {
			return true
		}
	}
	return false
}

// CrossOriginGuard refuses state-changing browser requests that come
// from another origin, including another port on localhost: 403
// CROSS_ORIGIN_REQUEST. Without it any web page can POST a "simple"
// request (text/plain, form or no Content-Type, no CORS preflight) to
// a private instance, which authenticates nobody.
//
// It uses net/http's CrossOriginProtection (Sec-Fetch-Site, falling
// back to Origin versus Host). GET, HEAD and OPTIONS always pass, and
// so do requests that carry neither header: the ctxt CLI, federation
// peers, MCP clients. Browser-extension origins pass, and the Vite dev
// server's origins pass when devCORS is set.
func CrossOriginGuard(devCORS bool) func(http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	if devCORS {
		for _, o := range allowedDevOrigins {
			if err := cop.AddTrustedOrigin(o); err != nil {
				panic("csrf: invalid dev origin " + o + ": " + err.Error())
			}
		}
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusForbidden, "CROSS_ORIGIN_REQUEST",
			"cross-origin request refused: changes must come from this instance's own pages, the ctxt CLI or the ctxt browser extension")
	}))
	return func(next http.Handler) http.Handler {
		guarded := cop.Handler(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsExtensionOrigin(r.Header.Get("Origin")) {
				next.ServeHTTP(w, r)
				return
			}
			guarded.ServeHTTP(w, r)
		})
	}
}

// RequireJSONBody answers 415 UNSUPPORTED_MEDIA_TYPE to a
// state-changing request whose body is not labelled application/json.
// Every body the API accepts is JSON (REST, MCP's JSON-RPC, federation
// push); text/plain, form encodings and a missing Content-Type are
// exactly what a web page can send without a CORS preflight. Requests
// without a body (bodyless POST, DELETE) pass.
func RequireJSONBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.ContentLength == 0 {
			next.ServeHTTP(w, r)
			return
		}
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "application/json" {
			WriteError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE",
				"request body must be JSON: send Content-Type: application/json")
			return
		}
		next.ServeHTTP(w, r)
	})
}

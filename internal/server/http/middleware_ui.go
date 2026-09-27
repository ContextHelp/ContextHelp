package http

import (
	"net/http"
	"strings"
)

// uiCSP forbids framing only. frame-ancestors is the one directive a
// meta tag cannot carry, and a script or style policy would first need
// an audit of the built bundle.
const uiCSP = "frame-ancestors 'none'"

// UISecurityHeaders marks every /ui response (SPA shell, assets, the
// client-route fallback, the /ui redirect) as unframeable, not to be
// MIME-sniffed, and sent without a Referer. Framing would let another
// site overlay the SPA's delete and retry buttons (clickjacking).
func UISecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ui" || strings.HasPrefix(r.URL.Path, "/ui/") {
			h := w.Header()
			h.Set("Content-Security-Policy", uiCSP)
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
		}
		next.ServeHTTP(w, r)
	})
}

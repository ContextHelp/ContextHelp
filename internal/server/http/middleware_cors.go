package http

import (
	"net/http"
	"strings"
)

// allowedDevOrigins are extra origins permitted only when devCORS is true.
var allowedDevOrigins = []string{
	"http://localhost:5173",
	"http://127.0.0.1:5173",
}

// corsAllowedPrefixes are always-allowed origin prefixes (extension origins).
var corsAllowedPrefixes = []string{
	"chrome-extension://",
	"moz-extension://",
	"safari-web-extension://",
}

// isLocalhostHost returns true when the request Host header is a localhost address.
func isLocalhostHost(host string) bool {
	// Strip port.
	h := host
	if i := strings.LastIndex(host, ":"); i != -1 {
		h = host[:i]
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// CORS returns middleware that sets Access-Control-* headers for permitted origins.
// When devCORS is true, Vite dev server origins are also allowed.
// CORS is only applied when the request Host is localhost — never for remote hosts.
func CORS(devCORS bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only apply CORS on localhost.
			if !isLocalhostHost(r.Host) {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := false

			// Always allow extension origins.
			for _, prefix := range corsAllowedPrefixes {
				if strings.HasPrefix(origin, prefix) {
					allowed = true
					break
				}
			}

			// Allow dev origins only when --dev flag is set.
			if !allowed && devCORS {
				for _, o := range allowedDevOrigins {
					if origin == o {
						allowed = true
						break
					}
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
				w.Header().Set("Vary", "Origin")
			}

			// Handle preflight.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

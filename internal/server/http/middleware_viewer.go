package http

import (
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// WithViewer returns middleware that attaches v to every request
// context. It is the private instance's counterpart of RequireAuth:
// with no auth provider there is no principal to resolve, every
// caller is the owner (ADR-079 §1), and the owner sees everything
// (sharing.Unrestricted). Authenticated instances never use it; their
// viewer is attached by RequireAuth in the same step as the principal.
func WithViewer(v sharing.Viewer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(sharing.WithViewer(r.Context(), v)))
		})
	}
}

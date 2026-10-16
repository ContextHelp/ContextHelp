package http

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
)

// streamRoutes hold a request open; a session guard re-checks the
// session while they run.
var streamRoutes = map[string]bool{"GET /api/v1/events": true}

// DefaultSessionRecheck is how often an open event stream re-checks its
// session.
const DefaultSessionRecheck = 30 * time.Second

// SessionGuard returns middleware that ends a long-lived request (the
// event stream) once the browser session it runs under stops being
// active: revoked, expired, or its identity or principal revoked. The
// browser rules a cookie needs (Origin check, CSRF header) are applied
// by RequireAuth, where the chain knows which method authenticated the
// request. Requests not authenticated by a session pass untouched.
func SessionGuard(sessions *authn.Sessions, recheck time.Duration) func(http.Handler) http.Handler {
	if recheck <= 0 {
		recheck = DefaultSessionRecheck
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, _ := authn.FromContext(r.Context())
			if !p.IsSession() {
				next.ServeHTTP(w, r)
				return
			}
			if streamRoutes[r.Method+" "+routePattern(r)] && sessions != nil {
				if c, err := r.Cookie(SessionCookieName(r.Host)); err == nil {
					ctx, cancel := context.WithCancel(r.Context())
					defer cancel()
					go watchSession(ctx, cancel, sessions, authn.HashSecret(c.Value), recheck)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// watchSession cancels a long request once its session stops being
// active (revoked, expired, token removed).
func watchSession(ctx context.Context, cancel context.CancelFunc, sessions *authn.Sessions, secretHash string, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := sessions.Check(ctx, secretHash); err != nil {
				cancel()
				return
			}
		}
	}
}

// routePattern resolves the chi pattern the request will match, from
// the root router: middleware on a sub-router runs before routing, so
// the request's own route context does not know it yet.
func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil || rctx.Routes == nil {
		return ""
	}
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	return strings.TrimSuffix(rctx.Routes.Find(chi.NewRouteContext(), r.Method, path), "*")
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

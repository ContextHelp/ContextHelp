package http

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
)

// RouteClass says who may call an /api/v1 route.
type RouteClass int

const (
	// RouteTokenOnly routes answer API tokens only, never a browser
	// session: writes the web UI does not make, pipelines, steps,
	// registries, federation, MCP and admin.
	RouteTokenOnly RouteClass = iota + 1
	// RouteUI routes also answer a browser session (scope "ui"): reads,
	// search and the web UI's own writes.
	RouteUI
)

// anyMethod matches every method of a mounted handler (MCP).
const anyMethod = "*"

// apiRouteClasses classifies every /api/v1 route, keyed "METHOD
// pattern" with the chi pattern. A route missing from this table
// refuses browser sessions, and TestAPIRouteClassesCoverEveryRoute fails
// until it is classified.
var apiRouteClasses = map[string]RouteClass{
	// The caller's own identity.
	"GET /api/v1/whoami": RouteUI,

	// Objects: the web UI lists, reads and deletes.
	"GET /api/v1/objects":              RouteUI,
	"GET /api/v1/objects/facets":       RouteUI,
	"GET /api/v1/objects/{id}":         RouteUI,
	"GET /api/v1/objects/{id}/related": RouteUI,
	"DELETE /api/v1/objects/{id}":      RouteUI,
	"PATCH /api/v1/objects/{id}":       RouteTokenOnly,

	"POST /api/v1/analyze": RouteTokenOnly,

	// Jobs: the web UI lists, reads and retries.
	"GET /api/v1/jobs":             RouteUI,
	"GET /api/v1/jobs/{id}":        RouteUI,
	"POST /api/v1/jobs/{id}/retry": RouteUI,

	"GET /api/v1/search": RouteUI,
	"POST /api/v1/find":  RouteUI,
	// The search-graph viewer's data.
	"GET /api/v1/search/graph": RouteUI,

	"GET /api/v1/entities":                  RouteUI,
	"GET /api/v1/entities/{slug}":           RouteUI,
	"GET /api/v1/entities/{slug}/backlinks": RouteUI,
	"POST /api/v1/entities/{slug}/pull":     RouteTokenOnly,
	"POST /api/v1/entities/registry-sync":   RouteTokenOnly,

	// Pipelines, steps and registries: token only, except the registry list
	// the web UI Registry page reads.
	"POST /api/v1/pipelines":                     RouteTokenOnly,
	"GET /api/v1/pipelines":                      RouteTokenOnly,
	"GET /api/v1/pipelines/{name}":               RouteTokenOnly,
	"DELETE /api/v1/pipelines/{name}":            RouteTokenOnly,
	"POST /api/v1/pipelines/{name}/archive":      RouteTokenOnly,
	"POST /api/v1/pipelines/{name}/unarchive":    RouteTokenOnly,
	"POST /api/v1/pipelines/enqueue":             RouteTokenOnly,
	"GET /api/v1/steps":                          RouteTokenOnly,
	"GET /api/v1/steps/{name}":                   RouteTokenOnly,
	"POST /api/v1/steps/install":                 RouteTokenOnly,
	"DELETE /api/v1/steps/{name}":                RouteTokenOnly,
	"POST /api/v1/steps/registries/fetch":        RouteTokenOnly,
	"POST /api/v1/steps/registries/{url}/update": RouteTokenOnly,
	"GET /api/v1/steps/registries":               RouteUI, // the web UI Registry page lists them; writes stay token-only
	"POST /api/v1/feeds":                         RouteTokenOnly,
	"GET /api/v1/feeds":                          RouteUI,
	"POST /api/v1/feeds/sync":                    RouteTokenOnly,
	"POST /api/v1/feeds/{id}/sync":               RouteTokenOnly,
	"DELETE /api/v1/feeds/{id}":                  RouteTokenOnly,
	"POST /api/v1/import":                        RouteTokenOnly,
	"GET /api/v1/import/{id}":                    RouteUI,
	"POST /api/v1/importers/dropbox/run":         RouteTokenOnly,
	"POST /api/v1/importers/slack/run":           RouteTokenOnly,
	"POST /api/v1/importers/discord/run":         RouteTokenOnly,
	"GET /api/v1/importers/runs/{id}":            RouteUI,
	"GET /api/v1/system/reminders":               RouteUI,
	"POST /api/v1/system/reminders/{id}/dismiss": RouteTokenOnly,
	"POST /api/v1/watches":                       RouteTokenOnly,
	"GET /api/v1/watches":                        RouteTokenOnly, // host filesystem layout: admin
	"GET /api/v1/watches/{id}":                   RouteTokenOnly,
	"PATCH /api/v1/watches/{id}":                 RouteTokenOnly,
	"DELETE /api/v1/watches/{id}":                RouteTokenOnly,
	"POST /api/v1/watches/{id}/pause":            RouteTokenOnly,
	"POST /api/v1/watches/{id}/resume":           RouteTokenOnly,
	"GET /api/v1/watches/{id}/files":             RouteTokenOnly,
	"POST /api/v1/inbox":                         RouteTokenOnly,
	"GET /api/v1/inbox":                          RouteUI,
	"POST /api/v1/inbox/{id}/triage":             RouteTokenOnly,
	"POST /api/v1/inbox/{id}/discard":            RouteTokenOnly,
	"GET /api/v1/events":                         RouteUI,
	"GET /api/v1/suggestions":                    RouteUI,
	"POST /api/v1/suggestions/{id}/approve":      RouteTokenOnly,
	"POST /api/v1/suggestions/{id}/reject":       RouteTokenOnly,
	"POST /api/v1/aliases":                       RouteTokenOnly,
	"GET /api/v1/aliases":                        RouteUI,
	"GET /api/v1/aliases/{alias}":                RouteUI,
	"DELETE /api/v1/aliases/{alias}":             RouteTokenOnly,
	"POST /api/v1/capture/page":                  RouteTokenOnly,
	"POST /api/v1/capture/selection":             RouteTokenOnly,
	"POST /api/v1/capture/element":               RouteTokenOnly,
	"GET /api/v1/capture/recent":                 RouteUI,
	"POST /api/v1/saved-searches":                RouteTokenOnly,
	"GET /api/v1/saved-searches":                 RouteUI,
	"GET /api/v1/saved-searches/{name}":          RouteUI,
	"DELETE /api/v1/saved-searches/{name}":       RouteTokenOnly,
	"GET /api/v1/search-history":                 RouteUI,
	"DELETE /api/v1/search-history":              RouteTokenOnly,
	"GET /api/v1/audit-log":                      RouteTokenOnly, // security events: admin
	"POST /api/v1/federation/push":               RouteTokenOnly,
	anyMethod + " /api/v1/mcp":                   RouteTokenOnly,
	anyMethod + " /api/v1/mcp/":                  RouteTokenOnly,
	"POST /api/v1/ui/login-codes":                RouteTokenOnly, // a session must not mint sessions
	"GET /api/v1/ui/session":                     RouteUI,
	"DELETE /api/v1/ui/session":                  RouteUI,
}

// ClassifyRoute returns the class of method on the chi pattern. An
// unclassified route reports false; callers treat it as token-only.
func ClassifyRoute(method, pattern string) (RouteClass, bool) {
	if c, ok := apiRouteClasses[method+" "+pattern]; ok {
		return c, true
	}
	c, ok := apiRouteClasses[anyMethod+" "+pattern]
	return c, ok
}

// streamRoutes hold a request open; a session guard re-checks the
// session while they run.
var streamRoutes = map[string]bool{"GET /api/v1/events": true}

// DefaultSessionRecheck is how often an open event stream re-checks its
// session.
const DefaultSessionRecheck = 30 * time.Second

// SessionGuard confines browser-session requests, after RequireAuth:
//
//   - scope: only RouteUI routes; any other /api/v1 route answers 403
//     SESSION_SCOPE, the route table deciding (fail closed on a route it
//     does not list);
//   - CSRF: a write must come from this instance's own pages
//     (Sec-Fetch-Site or Origin, see sameOriginWrite) and carry
//     X-Ctxt-CSRF: 1; a read sent by another site (Sec-Fetch-Site
//     same-site or cross-site, e.g. another port on this host) is
//     refused too;
//   - lifetime: an event stream ends once its session does.
//
// Token-authenticated requests pass untouched.
func SessionGuard(sessions *authn.Sessions, sec *security.Emitter, devCORS bool, recheck time.Duration) func(http.Handler) http.Handler {
	cop := newSessionCOP(devCORS)
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
			pattern := routePattern(r)
			key := r.Method + " " + pattern
			if class, ok := ClassifyRoute(r.Method, pattern); !ok || class != RouteUI || p.SessionScope() != authn.ScopeUI {
				if sec != nil {
					sec.RecordACLDenial(r.Context(), p.ID)
				}
				WriteError(w, http.StatusForbidden, "SESSION_SCOPE",
					"not available to a web UI session; use the ctxt CLI or an API token")
				return
			}
			if isSafeMethod(r.Method) {
				if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
					WriteError(w, http.StatusForbidden, "CROSS_ORIGIN_REQUEST",
						"cross-origin request refused: the web UI session answers this instance's own pages only")
					return
				}
			} else {
				if !sameOriginWrite(cop, r) {
					WriteError(w, http.StatusForbidden, "CROSS_ORIGIN_REQUEST",
						"cross-origin request refused: changes must come from this instance's own pages")
					return
				}
				if r.Header.Get(HeaderCSRF) != "1" {
					WriteError(w, http.StatusForbidden, "CSRF_HEADER_REQUIRED",
						"web UI session writes must send "+HeaderCSRF+": 1")
					return
				}
			}
			if streamRoutes[key] && sessions != nil {
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

package http

import (
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Capability-closed routes (ADR-080 §4, §6; plan decision D19). A
// restricted viewer reaches a route only when the route table opens
// it: the owner-only routes (jobs, inbox, capture payloads, events,
// pipelines, steps, registries, feeds, imports, watches, entity pages,
// the topic index, system reminders, the audit log) answer 403
// NOT_SHARED, never an empty 200, because their rows never belong to a
// share policy; the routes a policy capability opens (entities, the
// MCP mount, the RSQL and graph search modes) answer 403 NOT_SHARED
// without it. Everything else is open: its rows are scoped by the
// viewer's predicate, and a hidden row is a 404 there. The owner passes
// every route at no cost.

// routeAccess says what a restricted viewer needs to reach a route.
type routeAccess struct {
	// ownerOnly closes the route to every restricted viewer.
	ownerOnly bool
	// capability, when set, is the capability (storage.Capability*,
	// "search:<mode>") the viewer must hold.
	capability string
}

// Route access kinds.
var (
	routeOpen      = routeAccess{}
	routeOwnerOnly = routeAccess{ownerOnly: true}
)

// needs is the access of a route one capability opens.
func needs(capability string) routeAccess { return routeAccess{capability: capability} }

// routeAccessTable classifies every /api/v1 route. It is keyed by
// routeKey; the package's tests check it names exactly the routes of
// the route table, so a route lands with its classification or not at
// all, and an unclassified route fails closed at runtime.
var routeAccessTable = map[string]routeAccess{
	"GET /whoami": routeOpen,

	"GET /objects":                 routeOpen,
	"GET /objects/facets":          routeOpen,
	"GET /objects/{id}":            routeOpen,
	"GET /objects/{id}/related":    routeOpen,
	"PATCH /objects/{id}":          routeOpen,
	"DELETE /objects/{id}":         routeOpen,
	"POST /objects/{id}/reprocess": routeOpen,

	"POST /edges":             routeOpen,
	"GET /objects/{id}/edges": routeOpen,
	"DELETE /edges":           routeOpen,

	"POST /analyze": routeOpen,

	"GET /jobs":              routeOwnerOnly,
	"GET /jobs/{id}":         routeOwnerOnly,
	"POST /jobs/{id}/retry":  routeOwnerOnly,
	"POST /jobs/{id}/cancel": routeOwnerOnly,

	"GET /search":       needs(storage.CapabilitySearchPrefix + sharing.SearchRSQL),
	"POST /find":        routeOpen,
	"GET /search/graph": needs(storage.CapabilitySearchPrefix + sharing.SearchGraph),

	"GET /entities":                           needs(storage.CapabilityEntities),
	"GET /entities/resolve":                   needs(storage.CapabilityEntities),
	"GET /entities/{slug}":                    needs(storage.CapabilityEntities),
	"GET /entities/{slug}/backlinks":          needs(storage.CapabilityEntities),
	"POST /entities/{slug}/pull":              needs(storage.CapabilityEntities),
	"POST /entities/registry-sync":            routeOwnerOnly,
	"GET /entities/registry-pending":          routeOwnerOnly,
	"POST /entities/registry-pending/approve": routeOwnerOnly,
	"POST /entities/registry-pending/reject":  routeOwnerOnly,

	"GET /pages":                 routeOwnerOnly,
	"GET /pages/{slug}":          routeOwnerOnly,
	"POST /pages/{slug}/refresh": routeOwnerOnly,
	"GET /topic-index":           routeOwnerOnly,
	"GET /topic-index/{topic}":   routeOwnerOnly,
	"POST /topic-index":          routeOwnerOnly,
	"GET /stats":                 routeOpen,

	"POST /pipelines":                  routeOwnerOnly,
	"GET /pipelines":                   routeOwnerOnly,
	"GET /pipelines/{name}":            routeOwnerOnly,
	"DELETE /pipelines/{name}":         routeOwnerOnly,
	"POST /pipelines/{name}/archive":   routeOwnerOnly,
	"POST /pipelines/{name}/unarchive": routeOwnerOnly,
	"POST /pipelines/enqueue":          routeOpen,

	"GET /steps":           routeOwnerOnly,
	"GET /steps/{name}":    routeOwnerOnly,
	"POST /steps/install":  routeOwnerOnly,
	"DELETE /steps/{name}": routeOwnerOnly,

	"POST /steps/registries/fetch":        routeOwnerOnly,
	"POST /steps/registries/{url}/update": routeOwnerOnly,
	"GET /steps/registries":               routeOwnerOnly,

	"POST /feeds":           routeOwnerOnly,
	"GET /feeds":            routeOwnerOnly,
	"POST /feeds/sync":      routeOwnerOnly,
	"POST /feeds/{id}/sync": routeOwnerOnly,
	"DELETE /feeds/{id}":    routeOwnerOnly,

	"POST /import":     routeOwnerOnly,
	"GET /import/{id}": routeOwnerOnly,

	"POST /importers/dropbox/run": routeOwnerOnly,
	"POST /importers/slack/run":   routeOwnerOnly,
	"POST /importers/discord/run": routeOwnerOnly,
	"GET /importers/runs/{id}":    routeOwnerOnly,

	"GET /system/reminders":               routeOwnerOnly,
	"POST /system/reminders/{id}/dismiss": routeOwnerOnly,

	"POST /watches":             routeOwnerOnly,
	"GET /watches":              routeOwnerOnly,
	"GET /watches/{id}":         routeOwnerOnly,
	"PATCH /watches/{id}":       routeOwnerOnly,
	"DELETE /watches/{id}":      routeOwnerOnly,
	"POST /watches/{id}/pause":  routeOwnerOnly,
	"POST /watches/{id}/resume": routeOwnerOnly,
	"GET /watches/{id}/files":   routeOwnerOnly,

	"POST /inbox":              routeOpen,
	"GET /inbox":               routeOwnerOnly,
	"GET /inbox/queue":         routeOwnerOnly,
	"POST /inbox/clear":        routeOwnerOnly,
	"POST /inbox/{id}/triage":  routeOwnerOnly,
	"POST /inbox/{id}/discard": routeOwnerOnly,

	"GET /events": routeOwnerOnly,

	"POST /aliases":           routeOpen,
	"GET /aliases":            routeOpen,
	"GET /aliases/{alias}":    routeOpen,
	"DELETE /aliases/{alias}": routeOpen,

	"POST /capture/page":      routeOpen,
	"POST /capture/selection": routeOpen,
	"POST /capture/element":   routeOpen,
	"GET /capture/recent":     routeOwnerOnly,

	"POST /saved-searches":          routeOpen,
	"GET /saved-searches":           routeOpen,
	"GET /saved-searches/{name}":    routeOpen,
	"DELETE /saved-searches/{name}": routeOpen,

	"GET /search-history":    routeOpen,
	"DELETE /search-history": routeOwnerOnly,

	"GET /audit-log": routeOwnerOnly,

	"POST /federation/push": routeOpen,

	"ANY /mcp/": needs(storage.CapabilityMCP),
	"ANY /mcp":  needs(storage.CapabilityMCP),

	"POST /ui/login-codes": routeOpen,
	"DELETE /ui/session":   routeOpen,
}

// routeKey keys routeAccessTable: the method and the pattern relative
// to /api/v1, ANY for a route mounted on every method.
func routeKey(method, pattern string) string {
	if method == "" {
		method = "ANY"
	}
	return method + " " + pattern
}

// RouteAccess reports what a restricted viewer needs to reach the
// route mounted on method and pattern: whether it is owner-only, the
// capability it needs ("" for none), and whether the route is
// classified at all. The leak suite reads it to know what a restricted
// principal must be answered.
func RouteAccess(method, pattern string) (ownerOnly bool, capability string, known bool) {
	a, ok := routeAccessTable[routeKey(method, pattern)]
	return a.ownerOnly, a.capability, ok
}

// RequireCapability returns middleware that lets a request through
// only when the viewer RequireAuth attached may reach the route: an
// unrestricted viewer always; a restricted one when the route is open,
// or needs a capability it holds. An owner-only route, a capability
// the viewer lacks, an unclassified route and a request with no viewer
// are answered 403 NOT_SHARED. It never names a row.
func RequireCapability(method, pattern string) func(http.Handler) http.Handler {
	access, known := routeAccessTable[routeKey(method, pattern)]
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v, ok := sharing.FromContext(r.Context())
			if ok && v.IsUnrestricted() {
				next.ServeHTTP(w, r)
				return
			}
			if !ok || !known || access.ownerOnly || (access.capability != "" && !v.Allows(access.capability)) {
				WriteError(w, http.StatusForbidden, CodeNotShared, notSharedMessage)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

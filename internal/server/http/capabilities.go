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
//
// Every route also declares its response shape (ADR-080 §8, decision
// D20): what the body carries, so the visibility backstop knows which
// ID paths to walk. A write route answers about the caller's own
// mutation and declares none; so does a route that carries no object,
// entity or edge. An owner-only route never answers a restricted
// viewer and declares none.

// routeAccess says what a restricted viewer needs to reach a route and
// what the route's answer carries.
type routeAccess struct {
	// ownerOnly closes the route to every restricted viewer.
	ownerOnly bool
	// capability, when set, is the capability (storage.Capability*,
	// "search:<mode>") the viewer must hold.
	capability string
	// shape is the response shape the backstop walks.
	shape sharing.Shape
}

// Route access kinds.
var (
	routeOwnerOnly = routeAccess{ownerOnly: true, shape: sharing.ShapeNone}
	// routeNone is open and carries no object, entity or edge.
	routeNone = routeAccess{shape: sharing.ShapeNone}
	// routeObjects is open and carries objects; the entity routes are
	// all capability-gated (needs).
	routeObjects = routeAccess{shape: sharing.ShapeObjects}
)

// needs is the access of a route one capability opens, with the shape
// its answer carries.
func needs(capability string, shape sharing.Shape) routeAccess {
	return routeAccess{capability: capability, shape: shape}
}

// routeAccessTable classifies every /api/v1 route. It is keyed by
// routeKey; the package's tests check it names exactly the routes of
// the route table, so a route lands with its classification or not at
// all, and an unclassified route fails closed at runtime.
var routeAccessTable = map[string]routeAccess{
	"GET /whoami": routeNone,

	"GET /objects":                 routeObjects,
	"GET /objects/facets":          routeObjects,
	"GET /objects/{id}":            routeObjects,
	"GET /objects/{id}/related":    routeObjects,
	"PATCH /objects/{id}":          routeObjects,
	"DELETE /objects/{id}":         routeNone,
	"POST /objects/{id}/reprocess": routeNone,

	"POST /edges":             routeNone,
	"GET /objects/{id}/edges": routeObjects,
	"DELETE /edges":           routeNone,

	"POST /analyze": routeNone,

	"GET /jobs":              routeOwnerOnly,
	"GET /jobs/{id}":         routeOwnerOnly,
	"POST /jobs/{id}/retry":  routeOwnerOnly,
	"POST /jobs/{id}/cancel": routeOwnerOnly,

	"GET /search":       needs(storage.CapabilitySearchPrefix+sharing.SearchRSQL, sharing.ShapeObjects),
	"POST /find":        routeObjects,
	"GET /search/graph": needs(storage.CapabilitySearchPrefix+sharing.SearchGraph, sharing.ShapeMixed),

	"GET /entities":                           needs(storage.CapabilityEntities, sharing.ShapeEntities),
	"GET /entities/resolve":                   needs(storage.CapabilityEntities, sharing.ShapeEntities),
	"GET /entities/{slug}":                    needs(storage.CapabilityEntities, sharing.ShapeEntities),
	"GET /entities/{slug}/backlinks":          needs(storage.CapabilityEntities, sharing.ShapeObjects),
	"POST /entities/{slug}/pull":              needs(storage.CapabilityEntities, sharing.ShapeNone),
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
	"GET /stats":                 routeNone,

	"POST /pipelines":                  routeOwnerOnly,
	"GET /pipelines":                   routeOwnerOnly,
	"GET /pipelines/{name}":            routeOwnerOnly,
	"DELETE /pipelines/{name}":         routeOwnerOnly,
	"POST /pipelines/{name}/archive":   routeOwnerOnly,
	"POST /pipelines/{name}/unarchive": routeOwnerOnly,
	"POST /pipelines/enqueue":          routeNone,

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

	"POST /inbox":              routeNone,
	"GET /inbox":               routeOwnerOnly,
	"GET /inbox/queue":         routeOwnerOnly,
	"POST /inbox/clear":        routeOwnerOnly,
	"POST /inbox/{id}/triage":  routeOwnerOnly,
	"POST /inbox/{id}/discard": routeOwnerOnly,

	"GET /events": routeOwnerOnly,

	"POST /aliases":           routeNone,
	"GET /aliases":            routeObjects,
	"GET /aliases/{alias}":    routeObjects,
	"DELETE /aliases/{alias}": routeNone,

	"POST /capture/page":      routeNone,
	"POST /capture/selection": routeNone,
	"POST /capture/element":   routeNone,
	"GET /capture/recent":     routeOwnerOnly,

	"POST /saved-searches":          routeNone,
	"GET /saved-searches":           routeNone,
	"GET /saved-searches/{name}":    routeNone,
	"DELETE /saved-searches/{name}": routeNone,

	"GET /search-history":    routeNone,
	"DELETE /search-history": routeOwnerOnly,

	"GET /audit-log": routeOwnerOnly,

	"POST /federation/push": routeNone,

	// The principal registry, a principal's own credentials and the
	// sharing registry are governed by their scopes (admin:principals,
	// admin:shares, write:self), not by share policies: they read no
	// knowledge object, so a restricted viewer reaches them at the
	// capability layer and the scope middleware stays the gate
	// (ADR-079 §3, ADR-080 §1).
	"GET /principals":                                        routeNone,
	"POST /principals":                                       routeNone,
	"GET /principals/{id}":                                   routeNone,
	"POST /principals/{id}/suspend":                          routeNone,
	"POST /principals/{id}/revoke":                           routeNone,
	"POST /principals/{id}/links":                            routeNone,
	"GET /principals/{id}/identities":                        routeNone,
	"POST /principals/{id}/identities":                       routeNone,
	"DELETE /principals/{id}/identities/{identity}":          routeNone,
	"GET /principals/{id}/keys":                              routeNone,
	"POST /principals/{id}/keys":                             routeNone,
	"DELETE /principals/{id}/keys/{key}":                     routeNone,
	"GET /principals/{id}/ceiling":                           routeNone,
	"PUT /principals/{id}/ceiling":                           routeNone,
	"GET /self/keys":                                         routeNone,
	"POST /self/keys":                                        routeNone,
	"DELETE /self/keys/{key}":                                routeNone,
	"PUT /self/keys/{key}/policy":                            routeNone,
	"GET /self/identities":                                   routeNone,
	"DELETE /self/identities/{identity}":                     routeNone,
	"GET /self/sessions":                                     routeNone,
	"DELETE /self/sessions/{session}":                        routeNone,
	"POST /share/invites":                                    routeNone,
	"GET /share/audiences":                                   routeNone,
	"POST /share/audiences":                                  routeNone,
	"GET /share/audiences/{audience}":                        routeNone,
	"PUT /share/audiences/{audience}/members/{principal}":    routeNone,
	"DELETE /share/audiences/{audience}/members/{principal}": routeNone,
	"GET /share/policies":                                    routeNone,
	"POST /share/policies":                                   routeNone,
	"GET /share/policies/{policy}":                           routeNone,
	"PUT /share/policies/{policy}":                           routeNone,
	"GET /share/templates":                                   routeNone,
	"POST /share/templates/{name}":                           routeNone,
	"GET /share/grants":                                      routeNone,
	"POST /share/grants":                                     routeNone,
	"DELETE /share/grants/{grant}":                           routeNone,
	"PUT /share/keys/{key}/policy":                           routeNone,

	// The MCP mount answers tools/call with the search results as a
	// JSON document inside the answer; the objects shape follows it.
	"ANY /mcp/": needs(storage.CapabilityMCP, sharing.ShapeObjects),
	"ANY /mcp":  needs(storage.CapabilityMCP, sharing.ShapeObjects),

	"POST /ui/login-codes": routeNone,
	"DELETE /ui/session":   routeNone,
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

// RouteShape reports the response shape the route mounted on method
// and pattern declares, and whether the route is classified at all.
func RouteShape(method, pattern string) (sharing.Shape, bool) {
	a, ok := routeAccessTable[routeKey(method, pattern)]
	return a.shape, ok
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

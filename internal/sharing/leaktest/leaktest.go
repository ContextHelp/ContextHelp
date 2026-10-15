// Package leaktest is the harness of the sharing leak suite (ADR-080,
// "Testing"): it enumerates every read route the API serves and keeps
// one case per route that calls it as a fixture principal against an
// in-process dpkms.
//
// Read routes come from the two route tables, never from a hand-kept
// list: the HTTP table (internal/server/http, APIRouteSpecs) and the
// gRPC table (internal/server/grpc, MethodScopes). A route is a read
// route when a caller learns data from it without changing anything:
//
//   - every HTTP route mounted on a safe method (GET or HEAD), whatever
//     its scope: GET /audit-log (admin:audit) and
//     GET /entities/registry-pending (sync:registries) included;
//   - every HTTP route and every RPC whose scope verb is read, whatever
//     its method: POST /find, the MCP mount (any method, read:mcp),
//     POST /ui/login-codes (read:ui), the query, entity and job
//     services, and server reflection.
//
// A route reached only on an unsafe method under another verb (write,
// delete, process, sync, cancel, admin, signout) answers about the
// caller's own mutation and is outside the suite.
//
// The registry must hold a case for every read route. The package's
// tests fail naming each read route without a case and each case
// naming no read route, so a new route lands with its leak case or not
// at all. The fixture holds the owner only for now; each case's Check
// hook is where the per-viewer assertions (hidden IDs, masked-value
// canary, counts) go once restricted viewers exist.
//
// Import it from test files only.
package leaktest

import (
	"net/http"
	"slices"
	"strings"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
)

// Route is one enumerated read route.
type Route struct {
	// Key names the route in the registry: HTTPKey for an HTTP route,
	// the full method name ("/dpkms.v1.QueryService/Search") for an
	// RPC.
	Key string
	// Scope is the scope the route table requires.
	Scope authn.Scope
}

// AnyMethod stands in for an HTTP table entry mounted on every method.
const AnyMethod = "ANY"

// HTTPKey is the registry key of an HTTP route: the method and the
// pattern relative to /api/v1 ("GET /objects/{id}"); AnyMethod for an
// entry mounted on every method.
func HTTPKey(method, pattern string) string {
	if method == "" {
		method = AnyMethod
	}
	return method + " " + pattern
}

// ReadRoutes enumerates the read routes of both tables: HTTP routes in
// table order, then RPCs by name.
func ReadRoutes() []Route {
	return append(httpReadRoutes(httpserver.APIRouteSpecs()), rpcReadRoutes(grpcserver.MethodScopes())...)
}

// httpReadRoutes keeps the specs mounted on a safe method or under a
// read scope.
func httpReadRoutes(specs []httpserver.RouteSpec) []Route {
	out := make([]Route, 0, len(specs))
	for _, s := range specs {
		if isSafeMethod(s.Method) || isReadScope(s.Scope) {
			out = append(out, Route{Key: HTTPKey(s.Method, s.Pattern), Scope: s.Scope})
		}
	}
	return out
}

// rpcReadRoutes keeps the methods under a read scope, sorted by name.
func rpcReadRoutes(scopes map[string]authn.Scope) []Route {
	out := make([]Route, 0, len(scopes))
	for name, scope := range scopes {
		if isReadScope(scope) {
			out = append(out, Route{Key: name, Scope: scope})
		}
	}
	slices.SortFunc(out, func(a, b Route) int { return strings.Compare(a.Key, b.Key) })
	return out
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func isReadScope(s authn.Scope) bool {
	return s.Verb() == "read"
}

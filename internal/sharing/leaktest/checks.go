package leaktest

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
)

// DefaultCheck is the leak check of every case (ADR-080, "Testing"):
// for a restricted principal, a route its capabilities close must be
// answered 403 NOT_SHARED (PermissionDenied over gRPC), never a body;
// an open route's answer must name no hidden object id, entity slug or
// edge id and carry no canary string; and the routes that report a
// total or counts must report the visible count exactly. The owner is
// checked for a server error only (Run does that).
func DefaultCheck(t testing.TB, f *Fixture, p Principal, r Result) {
	t.Helper()
	if !p.Restricted() {
		return
	}
	route := routeOf(f, r)
	if closed, ok := Closed(route, p); ok {
		if closed {
			assertNotShared(t, route, p, r)
			return
		}
	}
	AssertNoLeak(t, f, p, route, r)
	assertTotals(t, f, p, route, r)
}

// routeOf names the route r answered: the case's key, which Run
// records on the result.
func routeOf(_ *Fixture, r Result) string { return r.route }

// Closed reports whether route (a registry key) is closed to p by the
// capability tables: owner-only, or needing a capability p lacks. ok
// is false when the route is unclassified, which the tables answer
// closed at runtime; the check then applies the leak assertions.
func Closed(route string, p Principal) (closed, ok bool) {
	if strings.HasPrefix(route, "/") {
		ownerOnly, capability, known := grpcserver.MethodAccess(route)
		return ownerOnly || (capability != "" && !p.Allows(capability)), known
	}
	method, pattern, _ := strings.Cut(route, " ")
	if method == AnyMethod {
		method = ""
	}
	ownerOnly, capability, known := httpserver.RouteAccess(method, pattern)
	return ownerOnly || (capability != "" && !p.Allows(capability)), known
}

// assertNotShared checks a closed route answered a refusal and nothing
// else: NOT_SHARED from the capability table, or INSUFFICIENT_SCOPE
// when the route's scope refused first (the scope check sits ahead of
// the capability check); both name no row.
func assertNotShared(t testing.TB, route string, p Principal, r Result) {
	t.Helper()
	body := string(r.Body)
	refused := strings.Contains(body, httpserver.CodeNotShared) || strings.Contains(body, httpserver.CodeInsufficientScope)
	if strings.HasPrefix(route, "/") {
		if r.Code != codes.PermissionDenied || !refused {
			t.Errorf("%s as %s: code %s body %s, want PermissionDenied NOT_SHARED", route, p.Name, r.Code, excerpt(r.Body))
		}
		return
	}
	if r.Status != http.StatusForbidden || !refused {
		t.Errorf("%s as %s: HTTP %d body %s, want 403 %s", route, p.Name, r.Status, excerpt(r.Body), httpserver.CodeNotShared)
	}
}

// AssertNoLeak checks r names nothing of f.Hidden(p).
func AssertNoLeak(t testing.TB, f *Fixture, p Principal, route string, r Result) {
	t.Helper()
	body := string(r.Body)
	for _, h := range f.Hidden(p) {
		if strings.Contains(body, h) {
			t.Errorf("%s as %s: body names hidden %q: %s", route, p.Name, h, excerpt(r.Body))
		}
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			for _, h := range f.Hidden(p) {
				if strings.Contains(v, h) {
					t.Errorf("%s as %s: header %s names hidden %q", route, p.Name, k, h)
				}
			}
		}
	}
}

// assertTotals checks the routes that count objects count the visible
// ones exactly.
func assertTotals(t testing.TB, f *Fixture, p Principal, route string, r Result) {
	t.Helper()
	want := len(f.VisibleObjects(p))
	switch route {
	case HTTPKey(http.MethodGet, "/objects"):
		var out struct {
			Data  []json.RawMessage `json:"data"`
			Total int               `json:"total"`
		}
		if json.Unmarshal(r.Body, &out) == nil && (out.Total != want || len(out.Data) != want) {
			t.Errorf("%s as %s: %d objects, total %d, want %d", route, p.Name, len(out.Data), out.Total, want)
		}
	case HTTPKey(http.MethodGet, "/objects/facets"):
		var out struct {
			Data map[string]int `json:"data"`
		}
		if json.Unmarshal(r.Body, &out) == nil {
			sum := 0
			for _, n := range out.Data {
				sum += n
			}
			if sum != want {
				t.Errorf("%s as %s: facets sum to %d (%v), want %d", route, p.Name, sum, out.Data, want)
			}
		}
	case HTTPKey(http.MethodGet, "/stats"):
		var out struct {
			Objects int `json:"knowledge_objects"`
			Jobs    struct {
				Total int `json:"total"`
			} `json:"jobs"`
		}
		if json.Unmarshal(r.Body, &out) == nil && (out.Objects != want || out.Jobs.Total != 0) {
			t.Errorf("%s as %s: stats count %d objects and %d jobs, want %d and 0", route, p.Name, out.Objects, out.Jobs.Total, want)
		}
	case HTTPKey(http.MethodPost, "/find"):
		var out struct {
			Objects []json.RawMessage `json:"objects"`
			Total   int               `json:"total"`
		}
		if json.Unmarshal(r.Body, &out) == nil && (out.Total != len(out.Objects) || out.Total > want) {
			t.Errorf("%s as %s: %d results, total %d, at most %d visible", route, p.Name, len(out.Objects), out.Total, want)
		}
	}
}

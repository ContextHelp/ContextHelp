package leaktest

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// CallTimeout bounds one case's call, streams included.
const CallTimeout = 10 * time.Second

// StreamWindow is how long a case holds a stream (SSE, WatchJob,
// reflection) open before it reports the frames received so far.
const StreamWindow = 300 * time.Millisecond

// Principal is a fixture identity a case calls its route as.
type Principal struct {
	// Name labels the principal in test output ("owner", "colleague",
	// "partner").
	Name string
	// ID is its registry id.
	ID string
	// Token is the bearer token it presents, over HTTP and gRPC alike.
	Token string
	// viewer is the viewer the instance compiles for it; the zero
	// viewer for the owner, whose checks are the no-server-error one.
	viewer sharing.Viewer
}

// Restricted reports whether p is a non-owner principal, whose
// answers the leak checks apply to.
func (p Principal) Restricted() bool { return p.Name != "owner" }

// Allows reports whether p's policies open capability c.
func (p Principal) Allows(c string) bool { return p.viewer.Allows(c) }

// Result is what a route answered.
type Result struct {
	// route is the case's registry key, recorded by Run for the checks.
	route string
	// Status is the HTTP status; zero for an RPC.
	Status int
	// Code is the gRPC status code; codes.OK for an HTTP call.
	Code codes.Code
	// Header holds the HTTP response headers; nil for an RPC.
	Header http.Header
	// Body is the HTTP body as received (a stream's frames up to the
	// call's window) or, for an RPC, every response message as JSON,
	// one per line, then the status message of an error.
	Body []byte
}

// ServerError reports whether the route failed on the server: an HTTP
// 5xx, or a gRPC code the server answers its own failures with.
func (r Result) ServerError() bool {
	if r.Status >= http.StatusInternalServerError {
		return true
	}
	switch r.Code {
	case codes.Unknown, codes.Internal, codes.Unimplemented, codes.Unavailable, codes.DataLoss:
		return true
	default:
		return false
	}
}

// CallFunc calls a case's route as p against f.
type CallFunc func(ctx context.Context, f *Fixture, p Principal) (Result, error)

// CheckFunc asserts over what a case's route answered p.
type CheckFunc func(t testing.TB, f *Fixture, p Principal, r Result)

// Case is the leak case of one read route.
type Case struct {
	// Route is the route's registry key: HTTPKey for an HTTP route, the
	// full method name for an RPC.
	Route string
	// Call calls the route as a principal.
	Call CallFunc
	// Check asserts over the answer; nil takes DefaultCheck, the
	// hidden-ID, canary, capability and count assertions of ADR-080.
	// Run asserts the route did not fail on the server whether or not
	// Check is set.
	Check CheckFunc
}

// Registry returns every registered case: HTTP cases in route-table
// order, then RPC cases, each with DefaultCheck unless it names its
// own.
func Registry() []Case {
	cases := append(httpCases(), rpcCases()...)
	for i := range cases {
		if cases[i].Check == nil {
			cases[i].Check = DefaultCheck
		}
	}
	return cases
}

// Coverage compares routes with cases. missing names the routes with
// no case; stale names the cases whose route is not enumerated. Both
// are sorted and both are empty when the registry matches the tables.
func Coverage(routes []Route, cases []Case) (missing, stale []string) {
	enumerated := make(map[string]bool, len(routes))
	for _, r := range routes {
		enumerated[r.Key] = true
	}
	covered := make(map[string]bool, len(cases))
	for _, c := range cases {
		if !enumerated[c.Route] && !covered[c.Route] {
			stale = append(stale, c.Route)
		}
		covered[c.Route] = true
	}
	for _, r := range routes {
		if !covered[r.Key] {
			missing = append(missing, r.Key)
		}
	}
	slices.Sort(missing)
	slices.Sort(stale)
	return missing, stale
}

// Run calls c as p against f, fails t when the route failed on the
// server, applies c.Check when set, and returns the answer.
func Run(t testing.TB, f *Fixture, p Principal, c Case) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), CallTimeout)
	defer cancel()
	r, err := c.Call(ctx, f, p)
	if err != nil {
		t.Fatalf("%s as %s: %v", c.Route, p.Name, err)
	}
	r.route = c.Route
	if r.ServerError() {
		t.Errorf("%s as %s: server error (HTTP %d, gRPC %s): %s", c.Route, p.Name, r.Status, r.Code, excerpt(r.Body))
	}
	if c.Check != nil {
		c.Check(t, f, p, r)
	}
	return r
}

// excerpt is the head of a body for a failure message.
func excerpt(b []byte) string {
	const limit = 300
	if len(b) > limit {
		return string(b[:limit]) + "..."
	}
	return string(b)
}

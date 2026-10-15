package leaktest

import (
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}

func keys(routes []Route) []string {
	out := make([]string, len(routes))
	for i, r := range routes {
		out[i] = r.Key
	}
	return out
}

// Every read route the tables enumerate has a case, and every case
// names an enumerated route: a route added without its leak case, or
// a case left behind by a removed route, fails here by name.
func TestRegistryCoversEveryReadRoute(t *testing.T) {
	routes := ReadRoutes()
	require.NotEmpty(t, routes)
	missing, stale := Coverage(routes, Registry())
	for _, r := range missing {
		t.Errorf("read route %s has no leak case", r)
	}
	for _, c := range stale {
		t.Errorf("leak case %s names no read route", c)
	}
}

// The enumeration reaches both tables.
func TestReadRoutesSpanBothTables(t *testing.T) {
	got := keys(ReadRoutes())
	assert.Contains(t, got, HTTPKey(http.MethodGet, "/objects"))
	assert.Contains(t, got, HTTPKey(http.MethodPost, "/find"))
	assert.Contains(t, got, HTTPKey("", "/mcp"))
	assert.Contains(t, got, pb.QueryService_Search_FullMethodName)
	assert.NotContains(t, got, HTTPKey(http.MethodPost, "/analyze"))
	assert.NotContains(t, got, pb.AnalyzeService_Analyze_FullMethodName)
}

// The HTTP rule: a safe method whatever the scope, or a read scope
// whatever the method; nothing else.
func TestHTTPReadRule(t *testing.T) {
	specs := []httpserver.RouteSpec{
		{Method: http.MethodGet, Pattern: "/audit-log", Scope: authn.ScopeAdminAudit},
		{Method: http.MethodPost, Pattern: "/find", Scope: authn.ScopeReadObjects},
		{Method: "", Pattern: "/mcp", Scope: authn.ScopeReadMCP},
		{Method: http.MethodPost, Pattern: "/analyze", Scope: authn.ScopeWriteObjects},
		{Method: http.MethodDelete, Pattern: "/objects/{id}", Scope: authn.ScopeDeleteObjects},
		{Method: http.MethodPost, Pattern: "/inbox/clear", Scope: authn.ScopeProcessInbox},
	}
	assert.Equal(t, []string{"GET /audit-log", "POST /find", "ANY /mcp"}, keys(httpReadRoutes(specs)))
}

// The RPC rule: a read scope, listed by name.
func TestRPCReadRule(t *testing.T) {
	got := rpcReadRoutes(map[string]authn.Scope{
		"/s/Write": authn.ScopeWriteObjects,
		"/s/B":     authn.ScopeReadJobs,
		"/s/A":     authn.ScopeReadSystem,
	})
	assert.Equal(t, []string{"/s/A", "/s/B"}, keys(got))
}

// Coverage names what is missing and what is stale, and nothing when
// the registry matches; a case dropped from a full registry is named.
func TestCoverageNamesMissingAndStaleCases(t *testing.T) {
	routes := []Route{{Key: "GET /a"}, {Key: "GET /b"}, {Key: "/svc/C"}}
	full := []Case{{Route: "GET /a"}, {Route: "GET /b"}, {Route: "/svc/C"}}

	missing, stale := Coverage(routes, full)
	assert.Empty(t, missing)
	assert.Empty(t, stale)

	missing, stale = Coverage(routes, []Case{{Route: "GET /a"}, {Route: "/svc/C"}, {Route: "GET /gone"}, {Route: "GET /gone"}})
	assert.Equal(t, []string{"GET /b"}, missing)
	assert.Equal(t, []string{"GET /gone"}, stale)

	missing, stale = Coverage(routes, full[1:])
	assert.Equal(t, []string{"GET /a"}, missing)
	assert.Empty(t, stale)
}

func TestResultServerError(t *testing.T) {
	assert.False(t, Result{Status: http.StatusOK}.ServerError())
	assert.False(t, Result{Status: http.StatusNotFound}.ServerError())
	assert.True(t, Result{Status: http.StatusInternalServerError}.ServerError())
	assert.True(t, Result{Status: http.StatusBadGateway}.ServerError())
	assert.False(t, Result{Code: codes.OK}.ServerError())
	assert.False(t, Result{Code: codes.NotFound}.ServerError())
	assert.False(t, Result{Code: codes.DeadlineExceeded}.ServerError())
	assert.True(t, Result{Code: codes.Internal}.ServerError())
	assert.True(t, Result{Code: codes.Unknown}.ServerError())
}

// The owner reaches every read route without a server error. Each
// answer is logged so a route answering its error path (400, 404) as
// the owner is visible in -v output.
func TestOwnerReadsEveryRoute(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	for _, c := range Registry() {
		t.Run(c.Route, func(t *testing.T) {
			r := Run(t, f, f.Owner(), c)
			t.Logf("HTTP %d, gRPC %s, %d bytes", r.Status, r.Code, len(r.Body))
		})
	}
}

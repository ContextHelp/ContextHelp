package leaktest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// receiptsOf returns the entries p left, oldest first, after flushing
// the audit writer.
func receiptsOf(t *testing.T, f *Fixture, p Principal) []*storage.AccessEntry {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, f.Server.Stack.Audit.Flush(ctx))
	es, err := f.Server.Driver.Sharing().AccessLog().Query(ctx, storage.AccessFilter{PrincipalID: p.ID})
	require.NoError(t, err)
	for i, j := 0, len(es)-1; i < j; i, j = i+1, j-1 {
		es[i], es[j] = es[j], es[i]
	}
	return es
}

func trail(e *storage.AccessEntry) []string {
	out := make([]string, 0, len(e.Checks))
	for _, c := range e.Checks {
		out = append(out, c.Check+"="+string(c.Outcome)+":"+c.Reason)
	}
	return out
}

// The read audit is a receipt (ADR-080 §9): a decision, the checks in
// the order they ran, a digest, over HTTP and gRPC alike. A refusal at
// any layer is an entry with the failing check and its reason; the
// checks after it never ran and are absent.
func TestReadAuditReceipts(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	ctx := context.Background()
	colleague, expired := f.Colleague(), f.Principal(NameExpired)

	// Allowed over HTTP; the same query in another order is the same
	// operation, another limit is not.
	require.Equal(t, http.StatusOK, fetch(t, f, colleague, "/objects?limit=100&sort=created_at").Status)
	require.Equal(t, http.StatusOK, fetch(t, f, colleague, "/objects?sort=created_at&limit=100").Status)
	require.Equal(t, http.StatusOK, fetch(t, f, colleague, "/objects?limit=50&sort=created_at").Status)
	// Scope, then the capability table, then (gRPC) scope again.
	require.Equal(t, http.StatusForbidden, fetch(t, f, colleague, "/share/audit?principal="+ColleagueID).Status)
	require.Equal(t, http.StatusForbidden, fetch(t, f, colleague, "/jobs/"+f.JobID).Status)
	// A viewer whose grant expired reads through the scoped driver and
	// is refused by the share policy.
	require.Equal(t, http.StatusOK, fetch(t, f, expired, "/objects").Status)

	_, err := pb.NewQueryServiceClient(f.Conn).ListObjects(f.RPCContext(ctx, colleague), &pb.ListObjectsRequest{})
	require.NoError(t, err)
	_, err = pb.NewJobServiceClient(f.Conn).GetJob(f.RPCContext(ctx, colleague), &pb.GetJobRequest{Id: f.JobID})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = pb.NewAnalyzeServiceClient(f.Conn).Analyze(f.RPCContext(ctx, colleague), &pb.AnalyzeRequest{Content: "x"})
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	es := receiptsOf(t, f, colleague)
	require.Len(t, es, 8)
	byRoute := map[string][]*storage.AccessEntry{}
	for _, e := range es {
		byRoute[e.Route] = append(byRoute[e.Route], e)
	}
	method := es[0].Checks[0].Check
	require.Contains(t, method, "authn.")
	authn := method + "=PASS:authenticated"

	wantAllowed := []string{
		authn, "scope=PASS:scope_held", "route_access=PASS:route_open", "share_policy=PASS:rows_scoped",
		"deny_class=NOT_APPLICABLE:no_ceiling", "backstop=PASS:clean",
	}
	lists := byRoute["GET /objects"]
	require.Len(t, lists, 3)
	digests := map[string]int{}
	for _, e := range lists {
		assert.Equal(t, storage.AccessDecisionAllowed, e.Decision)
		assert.Equal(t, wantAllowed, trail(e))
		assert.Regexp(t, `^v1:sha256:[0-9a-f]{64}$`, e.OperationDigest)
		digests[e.OperationDigest]++
	}
	// limit=100&sort=... and sort=...&limit=100 are one operation;
	// limit=50 is another.
	assert.Len(t, digests, 2, "query order is not consequential, a different limit is")
	rpc := byRoute[pb.QueryService_ListObjects_FullMethodName]
	require.Len(t, rpc, 1)
	assert.Equal(t, storage.AccessDecisionAllowed, rpc[0].Decision)
	assert.Equal(t, wantAllowed, trail(rpc[0]))
	assert.NotContains(t, digests, rpc[0].OperationDigest, "another route is another operation")

	// The scope refusal ends at scope: nothing after it ran.
	audit := byRoute["GET /share/audit"]
	require.Len(t, audit, 1)
	assert.Equal(t, storage.AccessDecisionDenied, audit[0].Decision)
	assert.Equal(t, []string{authn, "scope=FAIL:scope_missing"}, trail(audit[0]))
	// The owner-only route passes scope and fails the capability table,
	// over HTTP and over gRPC.
	job := byRoute["GET /jobs/{id}"]
	require.Len(t, job, 1, "the route is the template, not the path")
	rpcJob := byRoute[pb.JobService_GetJob_FullMethodName]
	require.Len(t, rpcJob, 1)
	for _, e := range []*storage.AccessEntry{job[0], rpcJob[0]} {
		assert.Equal(t, storage.AccessDecisionDenied, e.Decision, e.Route)
		assert.Equal(t, []string{authn, "scope=PASS:scope_held", "route_access=FAIL:owner_only"}, trail(e), e.Route)
	}
	// A write the colleague's scopes do not cover is refused at scope
	// over gRPC too.
	analyze := byRoute[pb.AnalyzeService_Analyze_FullMethodName]
	require.Len(t, analyze, 1, "the refused write is an entry")
	assert.Equal(t, storage.AccessDecisionDenied, analyze[0].Decision)
	assert.Equal(t, []string{authn, "scope=FAIL:scope_missing"}, trail(analyze[0]))

	// No grant in force: the share policy refused.
	ex := receiptsOf(t, f, expired)
	require.Len(t, ex, 1)
	assert.Equal(t, storage.AccessDecisionDenied, ex[0].Decision)
	assert.Contains(t, trail(ex[0]), "share_policy=FAIL:no_applicable_grant")

}

// An unauthenticated request leaves no entry, however many arrive: the
// table grows only with attributable principals. The owner's requests,
// refused or not, are not logged.
func TestReadAuditIgnoresUnattributedAndOwner(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	ctx := context.Background()
	revoked, owner := f.Principal(NameRevoked), f.Owner()
	anon := Principal{Name: "anon", Token: "not-a-key"}

	for i := 0; i < 20; i++ {
		require.Equal(t, http.StatusUnauthorized, fetch(t, f, anon, "/objects").Status)
		require.Equal(t, http.StatusUnauthorized, fetch(t, f, Principal{Name: "none"}, "/objects").Status)
		require.Equal(t, http.StatusUnauthorized, fetch(t, f, revoked, "/objects").Status)
	}
	require.Equal(t, http.StatusOK, fetch(t, f, owner, "/objects").Status)
	require.Equal(t, http.StatusNotFound, fetch(t, f, owner, "/objects/no-such-object").Status)

	require.NoError(t, f.Server.Stack.Audit.Flush(ctx))
	all, err := f.Server.Driver.Sharing().AccessLog().Query(ctx, storage.AccessFilter{})
	require.NoError(t, err)
	assert.Empty(t, all, "no entry for unauthenticated requests or for the owner")
}

// The owner lists refusals over the API: ?decision= narrows, a value
// that is not a decision is a 400, and every entry carries its receipt.
func TestReadAuditDecisionFilterOverAPI(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	colleague, owner := f.Colleague(), f.Owner()
	require.Equal(t, http.StatusOK, fetch(t, f, colleague, "/objects").Status)
	require.Equal(t, http.StatusForbidden, fetch(t, f, colleague, "/jobs/"+f.JobID).Status)
	require.NoError(t, f.Server.Stack.Audit.Flush(context.Background()))

	r := fetch(t, f, owner, "/share/audit?decision=denied&principal="+ColleagueID)
	require.Equal(t, http.StatusOK, r.Status, "%s", r.Body)
	var out struct {
		Data  []storage.AccessEntry `json:"data"`
		Total int                   `json:"total"`
	}
	require.NoError(t, json.Unmarshal(r.Body, &out))
	require.Equal(t, 1, out.Total)
	e := out.Data[0]
	assert.Equal(t, storage.AccessDecisionDenied, e.Decision)
	assert.Equal(t, "GET /jobs/{id}", e.Route)
	assert.Equal(t, "route_access", e.Checks[len(e.Checks)-1].Check)
	assert.Equal(t, "owner_only", e.Checks[len(e.Checks)-1].Reason)
	assert.NotEmpty(t, e.OperationDigest)

	assert.Equal(t, http.StatusBadRequest, fetch(t, f, owner, "/share/audit?decision=maybe").Status)
}

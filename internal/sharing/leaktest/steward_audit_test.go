package leaktest

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// stewardEntries lists the steward entries of grant in append order.
func stewardEntries(t *testing.T, f *Fixture, grant string) []*storage.AccessEntry {
	t.Helper()
	got, err := f.Server.Driver.Sharing().AccessLog().Query(context.Background(), storage.AccessFilter{StewardGrantID: grant})
	require.NoError(t, err)
	for _, e := range got {
		require.NotNil(t, e.Chain, "steward entry %s is not chained", e.ID)
	}
	slices.SortFunc(got, func(a, b *storage.AccessEntry) int { return cmp.Compare(a.Chain.Seq, b.Chain.Seq) })
	return got
}

// verifyStewardChain checks the instance's steward chain holds.
func verifyStewardChain(t *testing.T, f *Fixture) int64 {
	t.Helper()
	head, entries, err := f.Server.Driver.Sharing().AccessLog().StewardChain(context.Background())
	require.NoError(t, err)
	require.NoError(t, storage.VerifyAccessChain(head, entries, time.Time{}))
	return head.Seq
}

// callOnce runs c as p and returns the one steward entry the call left:
// it fails unless exactly one new entry of p's grant was committed by
// the time the answer arrived.
func callOnce(t *testing.T, f *Fixture, p Principal, c Case) (Result, *storage.AccessEntry) {
	t.Helper()
	before := len(stewardEntries(t, f, p.Grant()))
	r := Run(t, f, p, Case{Route: c.Route, Call: c.Call})
	after := stewardEntries(t, f, p.Grant())
	require.Len(t, after, before+1, "%s as %s: one durable entry per elevated request, committed before the answer", c.Route, p.Name)
	return r, after[len(after)-1]
}

// stewardVisible is what the steward's scope opens: the partner
// template's rows.
func stewardVisible(f *Fixture) []string { return f.VisibleObjects(f.Partner()) }

// Every read route of both tables, called under a grant in force,
// leaves exactly one durable steward entry before its answer: under the
// route, with the grant, the steward_grant check passed right after
// authentication, and object ids the scope opens and the count of them.
// A route the tables close (owner-only, a capability the scope lacks,
// a stream) is refused and recorded the same way.
func TestStewardEveryReadRouteIsRecorded(t *testing.T) {
	stewardEveryReadRouteIsRecorded(t, storageutil.NewTestDriver)
}

func stewardEveryReadRouteIsRecorded(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t), WithStewards())
	s := f.Steward()
	require.NotEmpty(t, s.Grant())
	visible := stewardVisible(f)
	calls := 0
	for _, c := range Registry() {
		if !s.Reaches(c) {
			continue
		}
		t.Run(c.Route, func(t *testing.T) {
			r, e := callOnce(t, f, s, c)
			calls++
			assert.Equal(t, c.Route, e.Route)
			assert.Equal(t, StewardID, e.PrincipalID)
			assert.Equal(t, s.Grant(), e.StewardGrantID)
			require.GreaterOrEqual(t, len(e.Checks), 2)
			assert.Equal(t, storage.AccessCheck{Check: "authn." + stewardAuthMethod, Outcome: storage.AccessOutcomePass, Reason: sharing.ReasonAuthenticated}, e.Checks[0])
			assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomePass, Reason: sharing.StewardReasonGrantActive}, e.Checks[1])
			assert.Equal(t, len(e.ObjectIDs), e.ObjectCount)
			for _, id := range e.ObjectIDs {
				assert.Contains(t, visible, id, "the entry names an object the scope does not open")
			}
			assert.NotContains(t, string(r.Body), sharing.CodeAuditUnavailable)
			if strings.HasPrefix(c.Route, "/") && isStream(c.Route) {
				assert.Equal(t, codes.PermissionDenied, r.Code, "streams are closed to an elevation")
				assert.Equal(t, sharing.ReasonStewardNoStream, e.Checks[len(e.Checks)-1].Reason)
			}
		})
	}
	assert.Equal(t, int64(calls), verifyStewardChain(t, f), "the chain holds every call's entry, no more")
}

// isStream reports whether an RPC route is one of the server's streams.
func isStream(method string) bool {
	return method == pb.JobService_WatchJob_FullMethodName || method == reflectionV1Method || method == reflectionV1AlphaMethod
}

// stewardDataCase is a read the steward's scope answers with data.
type stewardDataCase struct {
	Case
	// objects is whether the answer carries objects, or else entities.
	objects bool
	// none marks a read the fixture answers with no rows: the backlinks
	// route reads the dotted fixture slug as namespace/id, which no edge
	// names, for every principal. Its entry is still recorded.
	none bool
}

// stewardDataCases are the data reads of ADR-083 §5's list, REST and
// gRPC: objects, search, find, the search graph, entities and
// backlinks, each asked something the scope opens.
func stewardDataCases() []stewardDataCase {
	obj := "/objects/" + ContactSarah
	sarah := "/entities/" + EntitySarah
	rpcCall := func(route string, call func(ctx context.Context, f *Fixture, p Principal) (Result, error)) Case {
		return Case{Route: route, Call: call}
	}
	return []stewardDataCase{
		{getAt("/objects", "/objects?limit=100"), true, false},
		{getAt("/objects/{id}", obj), true, false},
		{getAt("/search", "/search?q=type==contact"), true, false},
		{post("/find", map[string]any{"query": "Sarah", "mode": "fts"}), true, false},
		{getAt("/search/graph", "/search/graph?q=Sarah"), true, false},
		{get("/entities"), false, false},
		{getAt("/entities/{slug}", sarah), false, false},
		{getAt("/entities/{slug}/backlinks", sarah+"/backlinks"), true, true},
		{rpcCall(pb.QueryService_ListObjects_FullMethodName, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).ListObjects(f.RPCContext(ctx, p), &pb.ListObjectsRequest{}))
		}), true, false},
		{rpcCall(pb.QueryService_GetObject_FullMethodName, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).GetObject(f.RPCContext(ctx, p), &pb.GetObjectRequest{Id: ContactSarah}))
		}), true, false},
		{rpcCall(pb.QueryService_Search_FullMethodName, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).Search(f.RPCContext(ctx, p), &pb.SearchRequest{Query: "type==contact"}))
		}), true, false},
		{rpcCall(pb.QueryService_NodeAwareSearch_FullMethodName, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).NodeAwareSearch(f.RPCContext(ctx, p), &pb.NodeAwareSearchRequest{Query: "type==contact"}))
		}), true, false},
		{rpcCall(pb.EntityService_ListEntities_FullMethodName, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewEntityServiceClient(f.Conn).ListEntities(f.RPCContext(ctx, p), &pb.ListEntitiesRequest{}))
		}), false, false},
		{rpcCall(pb.EntityService_GetEntity_FullMethodName, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewEntityServiceClient(f.Conn).GetEntity(f.RPCContext(ctx, p), &pb.GetEntityRequest{Slug: EntitySarah}))
		}), false, false},
		{rpcCall(pb.EntityService_GetEntityBacklinks_FullMethodName, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewEntityServiceClient(f.Conn).GetEntityBacklinks(f.RPCContext(ctx, p), &pb.GetEntityBacklinksRequest{Slug: EntitySarah}))
		}), true, true},
	}
}

// answered reports whether r is a success, over either transport.
func answered(r Result) bool {
	if r.Status != 0 {
		return r.Status == http.StatusOK
	}
	return r.Code == codes.OK
}

// Each data read under a grant in force answers, and its entry names
// the objects (or entities) it released; the full listing names every
// object the scope opens.
func TestStewardDataReadsRecordWhatTheyRelease(t *testing.T) {
	stewardDataReadsRecordWhatTheyRelease(t, storageutil.NewTestDriver)
}

func stewardDataReadsRecordWhatTheyRelease(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t), WithStewards())
	s := f.Steward()
	visible := stewardVisible(f)
	for _, c := range stewardDataCases() {
		t.Run(c.Route, func(t *testing.T) {
			r, e := callOnce(t, f, s, c.Case)
			require.True(t, answered(r), "HTTP %d gRPC %s: %s", r.Status, r.Code, excerpt(r.Body))
			assert.Equal(t, storage.AccessDecisionAllowed, e.Decision)
			if c.none {
				assert.Empty(t, e.ObjectIDs)
			} else if c.objects {
				assert.NotEmpty(t, e.ObjectIDs, "the entry names the objects the answer carried")
				// Who saw an object: every object the answer carries is
				// named. The entry may name more (a row a handler read
				// and left out), which over-records, the safe error.
				for _, id := range visible {
					if strings.Contains(string(r.Body), id) {
						assert.Contains(t, e.ObjectIDs, id, "the answer carried %s and the entry does not name it", id)
					}
				}
				for _, id := range e.ObjectIDs {
					assert.Contains(t, visible, id, "the entry names an object the scope does not open")
				}
			} else {
				assert.Positive(t, e.EntityCount, "the entry counts the entities the answer carried")
			}
		})
	}
	all := stewardEntries(t, f, s.Grant())
	require.NotEmpty(t, all)
	list := all[0]
	require.Equal(t, "GET /objects", list.Route)
	assert.ElementsMatch(t, visible, list.ObjectIDs, "the full listing's entry names every object the scope opens")
	verifyStewardChain(t, f)
}

// A request under a grant whose window has ended, or that was revoked,
// is refused with the check's code and discloses nothing: every data
// read, over both transports, leaves a denied entry under its route
// with the refusal's code and no object. A grant revoked between two
// requests refuses the second.
func TestStewardRefusedGrantsAreRecorded(t *testing.T) {
	stewardRefusedGrantsAreRecorded(t, storageutil.NewTestDriver)
}

func stewardRefusedGrantsAreRecorded(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t), WithStewards())
	visible := stewardVisible(f)
	refuse := func(t *testing.T, p Principal, reason string) {
		t.Helper()
		for _, c := range stewardDataCases() {
			t.Run(p.Name+c.Route, func(t *testing.T) {
				r, e := callOnce(t, f, p, c.Case)
				if r.Status != 0 {
					assert.Equal(t, http.StatusForbidden, r.Status)
				} else {
					assert.Equal(t, codes.PermissionDenied, r.Code)
				}
				assert.Contains(t, string(r.Body), "STEWARD_"+strings.ToUpper(reason))
				for _, id := range append(slices.Clone(visible), EntitySarah, "Sarah Lee") {
					assert.NotContains(t, string(r.Body), id, "a refusal disclosed %s", id)
				}
				assert.Equal(t, c.Route, e.Route)
				assert.Equal(t, storage.AccessDecisionDenied, e.Decision)
				assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomeFail, Reason: reason}, e.Checks[len(e.Checks)-1])
				assert.Empty(t, e.ObjectIDs)
				assert.Zero(t, e.ObjectCount)
			})
		}
	}
	refuse(t, f.StewardOf(NameStewardExpired), sharing.StewardReasonGrantExpired)
	refuse(t, f.StewardOf(NameStewardRevoked), sharing.StewardReasonGrantRevoked)

	s := f.Steward()
	r, _ := callOnce(t, f, s, stewardDataCases()[0].Case)
	require.True(t, answered(r), "before the revocation")
	require.NoError(t, f.Server.Driver.Sharing().StewardGrants().Revoke(context.Background(), s.Grant(), "prn_leak_approver", "done", time.Now()))
	refuse(t, s, sharing.StewardReasonGrantRevoked)
	verifyStewardChain(t, f)
}

// K concurrent elevated requests leave K entries on one linear chain:
// none dropped by a buffer, none forked.
func TestStewardConcurrentRequestsAreAllRecorded(t *testing.T) {
	stewardConcurrentRequestsAreAllRecorded(t, storageutil.NewTestDriver)
}

func stewardConcurrentRequestsAreAllRecorded(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t), WithStewards())
	s := f.Steward()
	const k = 16
	cases := stewardDataCases()
	var wg sync.WaitGroup
	results := make(chan Result, k)
	for i := range k {
		wg.Add(1)
		go func(c Case) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), CallTimeout)
			defer cancel()
			r, err := c.Call(ctx, f, s)
			if err != nil {
				t.Errorf("%s: %v", c.Route, err)
			}
			results <- r
		}(cases[i%len(cases)].Case)
	}
	wg.Wait()
	close(results)
	for r := range results {
		assert.True(t, answered(r), "HTTP %d gRPC %s: %s", r.Status, r.Code, excerpt(r.Body))
		assert.NotContains(t, string(r.Body), httpserver.CodeVisibilityBackstop)
	}
	assert.Len(t, stewardEntries(t, f, s.Grant()), k)
	assert.Equal(t, int64(k), verifyStewardChain(t, f))
}

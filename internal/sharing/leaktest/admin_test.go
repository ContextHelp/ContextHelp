package leaktest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Instance admin is not data access (ADR-082 §10, ADR-083 §10): a principal holding
// the admin bundle and no grant is a viewer no grant applies to. The
// owner's bypass is the only one, so every surface answers the
// administrators what it answers a stranger: no rows, no counts, no
// facets, no entities, and a refusal wherever the route is closed.

// driverFunc opens a fresh store for one fixture: SQLite here, Postgres
// in the integration-tagged runs of the same tests (pg_test.go).
type driverFunc func(*testing.T) storage.StorageDriver

// corpusRows lists every seeded object, the visible contacts and the
// hidden rows alike: an administrator is shown none of them.
var corpusRows = corpusOrder

// Every read route and RPC, called as each administrator, answers
// without a server error, refuses what the capability tables close,
// names nothing hidden and counts nothing: the administrator's view
// holds zero visible objects.
func TestAdminReadsEveryRouteSeesNothing(t *testing.T) {
	adminReadsEveryRouteSeesNothing(t, storageutil.NewTestDriver)
}

func adminReadsEveryRouteSeesNothing(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	for _, p := range f.Admins() {
		require.Empty(t, f.VisibleObjects(p), "%s is granted nothing", p.Name)
		require.True(t, p.Restricted(), "%s is not the owner", p.Name)
		for _, c := range Registry() {
			if !p.Reaches(c) {
				continue
			}
			t.Run(p.Name+"/"+c.Route, func(t *testing.T) {
				r := Run(t, f, p, c)
				t.Logf("HTTP %d, gRPC %s, %d bytes", r.Status, r.Code, len(r.Body))
			})
		}
	}
}

// An administrator is a viewer under no grant: the viewer holds no
// predicate, no capability and no field of any type.
func TestAdminViewerMatchesNothing(t *testing.T) {
	adminViewerMatchesNothing(t, storageutil.NewTestDriver)
}

func adminViewerMatchesNothing(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	for _, p := range f.Admins() {
		v := p.Viewer()
		assert.False(t, v.IsUnrestricted(), p.Name)
		for _, d := range []search.Dialect{search.DialectSQLite, search.DialectPostgres} {
			sql, args := v.Predicate(d)
			assert.Equal(t, sharing.MatchNothing, sql, "%s on %s", p.Name, d)
			assert.Empty(t, args, "%s on %s", p.Name, d)
		}
		for _, capability := range []string{storage.CapabilityEntities, storage.CapabilityMCP, storage.CapabilitySearchPrefix + sharing.SearchFTS, storage.CapabilitySearchPrefix + sharing.SearchGraph} {
			assert.False(t, v.Allows(capability), "%s holds %s", p.Name, capability)
		}
	}
}

// A fetch of any seeded object, direct, by id, is 404 for the
// administrator whatever the row is (a contact another viewer sees, a
// hidden note, an owner-only classed contact) and 200 for the owner:
// hidden means absent, never 403, which would confirm the row.
func TestAdminDirectFetchIs404(t *testing.T) { adminDirectFetchIs404(t, storageutil.NewTestDriver) }

func adminDirectFetchIs404(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	for _, p := range f.Admins() {
		for _, id := range corpusRows {
			for _, suffix := range []string{"", "/related", "/edges"} {
				r := fetch(t, f, p, "/objects/"+id+suffix)
				assert.Equal(t, http.StatusNotFound, r.Status, "%s GET /objects/%s%s: %s", p.Name, id, suffix, r.Body)
				assert.Contains(t, string(r.Body), httpserver.CodeNotFound, "%s %s%s", p.Name, id, suffix)
			}
		}
	}
	for _, id := range corpusRows {
		assert.Equal(t, http.StatusOK, fetch(t, f, f.Owner(), "/objects/"+id).Status, "owner GET /objects/%s", id)
	}
}

// The listings and counts are zero for the administrator: no object,
// no total, no facet value, no search hit, and the statistics count
// nothing. A leaked count would confirm that hidden rows exist.
func TestAdminListsAndCountsAreEmpty(t *testing.T) {
	adminListsAndCountsAreEmpty(t, storageutil.NewTestDriver)
}

func adminListsAndCountsAreEmpty(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	for _, p := range f.Admins() {
		list := fetch(t, f, p, "/objects?limit=100")
		require.Equal(t, http.StatusOK, list.Status, "%s: %s", p.Name, list.Body)
		var objs struct {
			Data  []json.RawMessage `json:"data"`
			Total int               `json:"total"`
		}
		require.NoError(t, json.Unmarshal(list.Body, &objs))
		assert.Empty(t, objs.Data, p.Name)
		assert.Zero(t, objs.Total, p.Name)

		facets := fetch(t, f, p, "/objects/facets")
		require.Equal(t, http.StatusOK, facets.Status, "%s: %s", p.Name, facets.Body)
		var fc struct {
			Data map[string]int `json:"data"`
		}
		require.NoError(t, json.Unmarshal(facets.Body, &fc))
		for facet, n := range fc.Data {
			assert.Zero(t, n, "%s facet %q", p.Name, facet)
		}

		stats := fetch(t, f, p, "/stats")
		require.Equal(t, http.StatusOK, stats.Status, "%s: %s", p.Name, stats.Body)
		var st struct {
			Objects int `json:"knowledge_objects"`
			Jobs    struct {
				Total int `json:"total"`
			} `json:"jobs"`
		}
		require.NoError(t, json.Unmarshal(stats.Body, &st))
		assert.Zero(t, st.Objects, p.Name)
		assert.Zero(t, st.Jobs.Total, p.Name)

		find, err := f.HTTP(context.Background(), p, http.MethodPost, "/find", map[string]any{"query": "Sarah", "mode": "fts"})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, find.Status, "%s: %s", p.Name, find.Body)
		var found struct {
			Objects []json.RawMessage `json:"objects"`
			Total   int               `json:"total"`
		}
		require.NoError(t, json.Unmarshal(find.Body, &found))
		assert.Empty(t, found.Objects, p.Name)
		assert.Zero(t, found.Total, p.Name)
	}
}

// Every route a capability or the owner-only table closes is 403
// NOT_SHARED for the administrator, whatever scopes its credential
// holds: the scope opens the route to the role, the table opens it to
// the viewer, and the administrator's viewer is nobody's. The routes
// are the entity, graph and RSQL searches, the MCP mount and the
// owner's operational stores. /events is not among them: it is open to
// every viewer and filtered per event, and the every-route test above
// holds it to showing the administrator nothing.
func TestAdminClosedRoutesAreNotShared(t *testing.T) {
	adminClosedRoutesAreNotShared(t, storageutil.NewTestDriver)
}

func adminClosedRoutesAreNotShared(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	paths := []string{
		"/entities", "/entities/" + EntitySarah, "/entities/" + EntitySarah + "/backlinks", "/entities/resolve?mention=sarah",
		"/search?q=type==contact", "/search/graph?q=sarah",
		"/jobs", "/jobs/" + f.JobID, "/inbox", "/inbox/queue", "/capture/recent", "/pipelines", "/steps",
		"/watches", "/feeds", "/pages", "/topic-index", "/system/reminders",
	}
	for _, p := range f.Admins() {
		for _, path := range paths {
			ctx, cancel := context.WithTimeout(context.Background(), StreamWindow)
			r, err := f.HTTP(ctx, p, http.MethodGet, path, nil)
			cancel()
			require.NoError(t, err)
			assert.Equal(t, http.StatusForbidden, r.Status, "%s GET %s: %s", p.Name, path, r.Body)
			assert.True(t, refusedClosed(r.Body), "%s GET %s: %s", p.Name, path, r.Body)
		}
		mcp, err := f.HTTP(context.Background(), p, http.MethodPost, "/mcp", jsonRPCToolsList)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, mcp.Status, "%s MCP: %s", p.Name, mcp.Body)
		assert.True(t, refusedClosed(mcp.Body), "%s MCP: %s", p.Name, mcp.Body)
	}
}

// The gRPC surface answers the key administrator what it answers a
// stranger: the entity and job services are closed, the query service
// returns nothing, and a hidden object is NotFound. The session
// administrator has no gRPC voice: a cookie is no gRPC credential.
func TestAdminRPCsSeeNothing(t *testing.T) { adminRPCsSeeNothing(t, storageutil.NewTestDriver) }

func adminRPCsSeeNothing(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	for _, name := range []string{NameAdminKey, NameAdminDevice} {
		t.Run(name, func(t *testing.T) { adminRPCsSeeNothingAs(t, f, name) })
	}
}

func adminRPCsSeeNothingAs(t *testing.T, f *Fixture, name string) {
	admin := f.Admin(name)
	ctx := f.RPCContext(context.Background(), admin)

	list, err := pb.NewQueryServiceClient(f.Conn).ListObjects(ctx, &pb.ListObjectsRequest{})
	require.NoError(t, err)
	assert.Empty(t, list.GetObjects())
	assert.Zero(t, list.GetTotal())

	_, err = pb.NewQueryServiceClient(f.Conn).GetObject(ctx, &pb.GetObjectRequest{Id: ContactSarah})
	assert.Equal(t, codes.NotFound, codeOf(err), "a hidden object is absent")

	search, err := pb.NewQueryServiceClient(f.Conn).Search(ctx, &pb.SearchRequest{Query: "type==contact"})
	if err == nil {
		assert.Empty(t, search.GetObjects())
	} else {
		assert.Equal(t, codes.PermissionDenied, codeOf(err))
	}

	_, err = pb.NewEntityServiceClient(f.Conn).GetEntity(ctx, &pb.GetEntityRequest{Slug: EntitySarah})
	assert.Equal(t, codes.PermissionDenied, codeOf(err))
	_, err = pb.NewJobServiceClient(f.Conn).GetJob(ctx, &pb.GetJobRequest{Id: f.JobID})
	assert.Equal(t, codes.PermissionDenied, codeOf(err))

	session := f.Admin(NameAdminSession)
	cookieOnly := f.RPCContext(context.Background(), session)
	_, err = pb.NewQueryServiceClient(f.Conn).ListObjects(cookieOnly, &pb.ListObjectsRequest{})
	assert.Equal(t, codes.Unauthenticated, codeOf(err), "a session cookie is no gRPC credential")
}

// An administrator cannot borrow the owner's bypass: X-Ctxt-View-As is
// the owner's header, so sending it, naming the owner, a viewer or a
// policy, is refused 403 on every read and returns no body of theirs.
func TestAdminCannotViewAs(t *testing.T) { adminCannotViewAs(t, storageutil.NewTestDriver) }

func adminCannotViewAs(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	for _, p := range f.Admins() {
		for _, target := range []string{storage.OwnerPrincipal, ColleagueID, sharing.TemplateColleagues} {
			asked := p
			asked.Header = p.Header.Clone()
			if asked.Header == nil {
				asked.Header = http.Header{}
			}
			asked.Header.Set(sharing.HeaderViewAs, target)
			for _, path := range []string{"/objects", "/objects/" + ContactSarah, "/stats"} {
				r := fetch(t, f, asked, path)
				assert.Equal(t, http.StatusForbidden, r.Status, "%s as %s GET %s: %s", p.Name, target, path, r.Body)
				assert.Contains(t, string(r.Body), httpserver.CodeViewAsOwnerOnly, "%s as %s GET %s", p.Name, target, path)
				assert.NotContains(t, string(r.Body), ContactSarah, "%s as %s GET %s", p.Name, target, path)
			}
		}
	}
	// Over gRPC the same header is metadata, and the same refusal.
	for _, name := range []string{NameAdminKey, NameAdminDevice} {
		asked := f.Admin(name)
		asked.Header = http.Header{sharing.HeaderViewAs: {storage.OwnerPrincipal}}
		_, err := pb.NewQueryServiceClient(f.Conn).ListObjects(f.RPCContext(context.Background(), asked), &pb.ListObjectsRequest{})
		assert.Equal(t, codes.PermissionDenied, codeOf(err), name)
	}
}

// Per-principal state belongs to its principal (ADR-080 §4): the
// owner's saved searches and search history are the owner's, and an
// administrator lists neither, whatever the list route's scope.
func TestAdminSeesNoOneElsesSavedSearches(t *testing.T) {
	adminSeesNoOneElsesSavedSearches(t, storageutil.NewTestDriver)
}

func adminSeesNoOneElsesSavedSearches(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	const searchName = "owner-private-search-canary"
	owner := f.Owner()
	created, err := f.HTTP(context.Background(), owner, http.MethodPost, "/saved-searches", map[string]any{"name": searchName, "query": "type==note"})
	require.NoError(t, err)
	if created.Unsupported() {
		t.Skip("this storage backend has no saved-search store (501 NOT_IMPLEMENTED): there is no owner state to hide")
	}
	require.Equal(t, http.StatusCreated, created.Status, "%s", created.Body)
	require.Equal(t, http.StatusOK, fetch(t, f, owner, "/search?q=tag==owner-private-history-canary").Status)

	require.Contains(t, string(fetch(t, f, owner, "/saved-searches").Body), searchName, "the owner lists its own")
	for _, p := range f.Admins() {
		for _, path := range []string{"/saved-searches", "/saved-searches/" + searchName, "/search-history"} {
			r := fetch(t, f, p, path)
			assert.NotContains(t, string(r.Body), searchName, "%s GET %s", p.Name, path)
			assert.NotContains(t, string(r.Body), "owner-private-history-canary", "%s GET %s", p.Name, path)
		}
		assert.Equal(t, http.StatusNotFound, fetch(t, f, p, "/saved-searches/"+searchName).Status, p.Name)
	}
}

// The read audit is the owner's. An administrator holding admin:audit
// is refused 403 NOT_SHARED on the receipts and on the object audit
// log, with or without a filter naming a principal or an object: the
// receipts carry the ids of what other principals read, and the object
// log carries payloads.
func TestAdminCannotReadTheAccessLog(t *testing.T) {
	adminCannotReadTheAccessLog(t, storageutil.NewTestDriver)
}

func adminCannotReadTheAccessLog(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	colleague := f.Colleague()
	require.Equal(t, http.StatusOK, fetch(t, f, colleague, "/objects/"+ContactSarah).Status)
	require.NoError(t, f.Server.Stack.Audit.Flush(context.Background()))
	owned := fetch(t, f, f.Owner(), "/share/audit?object="+ContactSarah)
	require.Equal(t, http.StatusOK, owned.Status, "%s", owned.Body)
	require.Contains(t, string(owned.Body), ContactSarah, "the owner reads the object list")

	for _, p := range f.Admins() {
		for _, path := range []string{
			"/share/audit", "/share/audit?principal=" + ColleagueID, "/share/audit?object=" + ContactSarah,
			"/share/audit?decision=denied", "/audit-log", "/audit-log?object_id=" + ContactSarah,
		} {
			r := fetch(t, f, p, path)
			assert.Equal(t, http.StatusForbidden, r.Status, "%s GET %s: %s", p.Name, path, r.Body)
			assert.NotContains(t, string(r.Body), ContactSarah, "%s GET %s", p.Name, path)
			assert.NotContains(t, string(r.Body), ColleagueID, "%s GET %s", p.Name, path)
		}
	}
	// The administrator's own requests are logged like any viewer's.
	require.NoError(t, f.Server.Stack.Audit.Flush(context.Background()))
	for _, id := range []string{AdminKeyID, AdminDeviceID} {
		mine, err := f.Server.Driver.Sharing().AccessLog().Query(context.Background(), storage.AccessFilter{PrincipalID: id})
		require.NoError(t, err)
		assert.NotEmpty(t, mine, "%s: the administrator's requests are in the read audit", id)
	}
}

// refusedClosed reports whether body is the refusal of a closed route:
// NOT_SHARED from the capability tables, or INSUFFICIENT_SCOPE when
// the credential's scopes (a web UI session holds a fixed set) refused
// first. Both name no row.
func refusedClosed(body []byte) bool {
	s := string(body)
	return strings.Contains(s, httpserver.CodeNotShared) || strings.Contains(s, httpserver.CodeInsufficientScope)
}

// codeOf is the gRPC code of err, OK for nil.
func codeOf(err error) codes.Code { return RPCResult(nil, err).Code }

// Find in every mode, with facets and an explanation asked for, and
// with the words of the hidden rows, an entity name and a mention
// operator for a query, finds nothing for the administrator: no object,
// no facet count, no trace of a candidate. A mode whose retrieval
// backend is not up answers 503 and names no row.
func TestAdminFindEveryModeFindsNothing(t *testing.T) {
	adminFindEveryModeFindsNothing(t, storageutil.NewTestDriver)
}

func adminFindEveryModeFindsNothing(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	queries := []string{"Sarah", "Acme", "SARAH-PRIVATE-NOTE-CANARY", SarahEmail, "mention==person.sarah-lee", "type==contact"}
	for _, p := range f.Admins() {
		for _, mode := range []string{"", "fts", "vector", "hybrid"} {
			for _, query := range queries {
				r, err := f.HTTP(context.Background(), p, http.MethodPost, "/find", map[string]any{
					"query": query, "mode": mode, "facets": true, "explain": true, "limit": 100,
				})
				require.NoError(t, err)
				// The answer echoes the query the caller sent; that is
				// the caller's own word, not a leak.
				echoless := r
				echoless.Body = bytes.ReplaceAll(r.Body, []byte(query), nil)
				AssertNoLeak(t, f, p, "POST /find", echoless)
				if r.Status != http.StatusOK {
					assert.Contains(t, []int{http.StatusServiceUnavailable, http.StatusBadRequest}, r.Status, "%s %q %q: %s", p.Name, mode, query, r.Body)
					continue
				}
				var out struct {
					Objects []json.RawMessage `json:"objects"`
					Total   int               `json:"total"`
					Facets  map[string]int    `json:"facets"`
				}
				require.NoError(t, json.Unmarshal(r.Body, &out))
				assert.Empty(t, out.Objects, "%s %q %q", p.Name, mode, query)
				assert.Zero(t, out.Total, "%s %q %q", p.Name, mode, query)
				for facet, n := range out.Facets {
					assert.Zero(t, n, "%s %q %q facet %q", p.Name, mode, query, facet)
				}
			}
		}
	}
}

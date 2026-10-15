package leaktest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Every non-owner request leaves one read audit entry (ADR-080 §9):
// who, through which credential, on which route, under which policy
// versions, with the objects it released. The owner's own requests
// leave none. The owner lists the audit over the API; a reader cannot.
func TestReadAuditOnePerRequest(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	ctx := context.Background()
	colleague, owner := f.Colleague(), f.Owner()

	require.Equal(t, http.StatusOK, fetch(t, f, colleague, "/objects?limit=100").Status)
	require.Equal(t, http.StatusOK, fetch(t, f, colleague, "/objects/"+ContactSarah).Status)
	require.Equal(t, http.StatusNotFound, fetch(t, f, colleague, "/objects/"+NoteSarah).Status)
	require.Equal(t, http.StatusOK, fetch(t, f, owner, "/objects").Status)
	_, err := pb.NewQueryServiceClient(f.Conn).ListObjects(f.RPCContext(ctx, colleague), &pb.ListObjectsRequest{})
	require.NoError(t, err)

	require.NoError(t, f.Server.Stack.Audit.Flush(ctx))
	log := f.Server.Driver.Sharing().AccessLog()
	entries, err := log.Query(ctx, storage.AccessFilter{PrincipalID: ColleagueID})
	require.NoError(t, err)
	byRoute := map[string][]*storage.AccessEntry{}
	for _, e := range entries {
		byRoute[e.Route] = append(byRoute[e.Route], e)
	}
	require.Len(t, byRoute["GET /objects"], 1)
	list := byRoute["GET /objects"][0]
	assert.ElementsMatch(t, f.VisibleObjects(colleague), list.ObjectIDs)
	assert.Equal(t, len(f.VisibleObjects(colleague)), list.ObjectCount)
	assert.Equal(t, colleague.Viewer().PolicyKey(), list.PolicyVersions)
	assert.NotEmpty(t, list.IdentityID, "the key the request authenticated with")
	assert.WithinDuration(t, time.Now(), list.At, time.Minute)

	// Two GET /objects/{id} calls: the visible contact, and the hidden
	// note, which released nothing.
	var released [][]string
	for _, e := range byRoute["GET /objects/{id}"] {
		released = append(released, e.ObjectIDs)
	}
	assert.ElementsMatch(t, [][]string{{ContactSarah}, nil}, released)
	require.Len(t, byRoute[pb.QueryService_ListObjects_FullMethodName], 1)

	ownerEntries, err := log.Query(ctx, storage.AccessFilter{PrincipalID: storage.OwnerPrincipal})
	require.NoError(t, err)
	assert.Empty(t, ownerEntries, "owner requests are not logged")

	// Over the API: the owner asks who saw Sarah and what the colleague saw.
	saw := fetch(t, f, owner, "/share/audit?object="+ContactSarah)
	require.Equal(t, http.StatusOK, saw.Status, "%s", saw.Body)
	var out struct {
		Data  []storage.AccessEntry `json:"data"`
		Total int                   `json:"total"`
	}
	require.NoError(t, json.Unmarshal(saw.Body, &out))
	assert.GreaterOrEqual(t, out.Total, 2)
	for _, e := range out.Data {
		assert.Equal(t, ColleagueID, e.PrincipalID)
		assert.Contains(t, e.ObjectIDs, ContactSarah)
	}
	whatSince := fetch(t, f, owner, "/share/audit?principal="+ColleagueID+"&since=1h&limit=2")
	require.Equal(t, http.StatusOK, whatSince.Status, "%s", whatSince.Body)
	require.NoError(t, json.Unmarshal(whatSince.Body, &out))
	assert.Len(t, out.Data, 2)
	bad := fetch(t, f, owner, "/share/audit?since=soon")
	assert.Equal(t, http.StatusBadRequest, bad.Status)

	refused := fetch(t, f, colleague, "/share/audit?principal="+ColleagueID)
	assert.Equal(t, http.StatusForbidden, refused.Status)
	assert.Contains(t, string(refused.Body), httpserver.CodeInsufficientScope)

	// Retention: pruning drops the id lists, keeps the entries and counts.
	n, err := log.Prune(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Greater(t, n, int64(0))
	after, err := log.Query(ctx, storage.AccessFilter{PrincipalID: ColleagueID})
	require.NoError(t, err)
	require.Len(t, after, len(entries))
	for _, e := range after {
		assert.Empty(t, e.ObjectIDs)
	}
	for _, e := range after {
		if e.Route == "GET /objects" {
			assert.Equal(t, len(f.VisibleObjects(colleague)), e.ObjectCount)
		}
	}
}

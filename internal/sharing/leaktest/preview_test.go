package leaktest

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// previewOf is the owner previewing p: p's name, viewer and checks,
// the owner's token, the header naming p.
func (f *Fixture) previewOf(p Principal) Principal {
	q := p
	q.Token = f.owner.Token
	q.Header = http.Header{sharing.HeaderViewAs: {p.ID}}
	return q
}

// identityRoutes answer about the caller, not about rows: the owner
// previewing sees its own principal there.
func identityRoute(route string) bool {
	for _, prefix := range []string{"GET /whoami", "GET /self/", "GET /principals", "GET /share/", "POST /ui/", "/grpc.reflection"} {
		if strings.HasPrefix(route, prefix) {
			return true
		}
	}
	return false
}

// The owner's preview of a principal answers every knowledge route the
// principal is answered on byte for byte as the principal itself,
// timestamps and request ids aside, passes the principal's leak
// checks, and is audited as the owner's. A route the principal is
// refused on is refused differently (its scopes are its own: the owner
// keeps the owner's, and a capability refusal names no row either
// way), so only the principal's 2xx answers are compared. The agent is
// a key's narrowing of the partner, which no principal id names, so it
// is left out: a preview by principal id gives the partner's view.
func TestPreviewEqualsThePrincipalsOwnView(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	for _, p := range f.Viewers() {
		if p.Name == NameAgent {
			continue
		}
		preview := f.previewOf(p)
		for _, c := range Registry() {
			if identityRoute(c.Route) {
				continue
			}
			t.Run(p.Name+"/"+c.Route, func(t *testing.T) {
				own := Run(t, f, p, c)
				if answered := own.Status/100 == 2 || (own.Status == 0 && own.Code == codes.OK); !answered {
					return
				}
				pre := Run(t, f, preview, c)
				assert.Equal(t, own.Status, pre.Status, "status")
				assert.Equal(t, own.Code, pre.Code, "code")
				assert.Equal(t, normalizeFor(c.Route, own.Body), normalizeFor(c.Route, pre.Body), "the preview differs from the principal's own answer")
			})
		}
	}

	ctx := context.Background()
	require.NoError(t, f.Server.Stack.Audit.Flush(ctx))
	entries, err := f.Server.Driver.Sharing().AccessLog().Query(ctx, storage.AccessFilter{PrincipalID: storage.OwnerPrincipal})
	require.NoError(t, err)
	require.NotEmpty(t, entries, "previews are audited as the owner's requests")
	for _, e := range entries {
		assert.True(t, strings.HasPrefix(e.PolicyVersions, "preview="), e.PolicyVersions)
	}
}

// A policy name previews the policy as a grant to the owner: the
// colleagues template shows what the colleague sees of the contacts.
func TestPreviewByPolicyName(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	colleague := f.Colleague()
	byPolicy := f.previewOf(colleague)
	byPolicy.Header = http.Header{sharing.HeaderViewAs: {sharing.TemplateColleagues}}
	for _, path := range []string{"/objects?limit=100", "/objects/" + ContactSarah, "/entities", "/search/graph?q=sarah"} {
		own := fetch(t, f, colleague, path)
		pre := fetch(t, f, byPolicy, path)
		assert.Equal(t, own.Status, pre.Status, path)
		assert.Equal(t, normalize(own.Body), normalize(pre.Body), path)
	}
	// The owner previewing the owner is the owner.
	asOwner := f.previewOf(colleague)
	asOwner.Header = http.Header{sharing.HeaderViewAs: {storage.OwnerPrincipal}}
	r := fetch(t, f, asOwner, "/objects/"+NoteSarah)
	assert.Equal(t, http.StatusOK, r.Status)
}

// The header is the owner's: a restricted principal is refused with
// 403; a write route is refused with 400; an unknown target is 404.
// Over gRPC the same, in its codes.
func TestPreviewRefusals(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	ctx := context.Background()
	colleague := f.Colleague()

	sneaky := colleague
	sneaky.Header = http.Header{sharing.HeaderViewAs: {PartnerID}}
	r := fetch(t, f, sneaky, "/objects")
	assert.Equal(t, http.StatusForbidden, r.Status)
	assert.Contains(t, string(r.Body), httpserver.CodeViewAsOwnerOnly)
	assert.NotContains(t, string(r.Body), ContactPersonalTag)

	owner := f.Owner()
	owner.Header = http.Header{sharing.HeaderViewAs: {ColleagueID}}
	w, err := f.HTTP(ctx, owner, http.MethodPost, "/analyze", map[string]any{"content": "x"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, w.Status)
	assert.Contains(t, string(w.Body), httpserver.CodeViewAsReadOnly)
	find, err := f.HTTP(ctx, owner, http.MethodPost, "/find", map[string]any{"query": "sarah", "mode": "fts"})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, find.Status, "POST /find reads: %s", find.Body)

	unknown := f.Owner()
	unknown.Header = http.Header{sharing.HeaderViewAs: {"nobody-here"}}
	assert.Equal(t, http.StatusNotFound, fetch(t, f, unknown, "/objects").Status)

	_, err = pb.NewQueryServiceClient(f.Conn).ListObjects(f.RPCContext(ctx, sneaky), &pb.ListObjectsRequest{})
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = pb.NewQueryServiceClient(f.Conn).ListObjects(f.RPCContext(ctx, unknown), &pb.ListObjectsRequest{})
	assert.Equal(t, codes.NotFound, status.Code(err))
	_, err = pb.NewAnalyzeServiceClient(f.Conn).Analyze(f.RPCContext(ctx, owner), &pb.AnalyzeRequest{Content: "x"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	resp, err := pb.NewQueryServiceClient(f.Conn).ListObjects(f.RPCContext(ctx, owner), &pb.ListObjectsRequest{Limit: 100})
	require.NoError(t, err)
	assert.Len(t, resp.Objects, len(f.VisibleObjects(colleague)))
}

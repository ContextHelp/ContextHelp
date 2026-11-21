package leaktest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	pb "github.com/ideacrafterslabs/ctxt/internal/server/grpc/pb"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// driverFunc opens a fresh store for one fixture: SQLite here, Postgres
// in the integration-tagged runs of the same tests.
type driverFunc func(*testing.T) storage.StorageDriver

// Per-object rendering (ADR-080 §5): a viewer under two grants sees
// each object through the grants that cover it, never through the
// union of every grant it holds. The principal below holds a grant
// showing the titles of team notes and a grant showing every field of
// public rows; a team note that is not public renders its title only,
// on every read path, while a team note that is also public renders
// whole.
const (
	perObjectID      = "prn_leak_per_object"
	namePerObject    = "per-object"
	perObjectPrivate = "pob-note-private"
	perObjectShared  = "pob-note-shared"
	// perObjectEntity has no dot: the backlinks route reads a dotted
	// slug as namespace/id, which names another edge target.
	perObjectEntity   = "pob-launch"
	perObjectTitlePrv = "Team roadmap draft"
	perObjectTitleShr = "Release announcement"
	// perObjectCanary is the private note's body: only the public grant
	// shows a note's body, and it does not cover the private note.
	perObjectCanary = "POB-PRIVATE-BODY-CANARY"
	// perObjectShown is the shared note's body: the public grant
	// covers it and shows every field.
	perObjectShown = "POB-SHARED-BODY-VISIBLE"
)

// teamTitles shows the title of every team note and opens no entity.
func teamTitles() sharing.Policy {
	return sharing.Policy{
		Name: "pob-team-titles", Data: "type==note;tag==team",
		Fields:       map[string][]string{"note": {sharing.FieldTitle}},
		Capabilities: sharing.Capabilities{Search: sharing.SearchModes, MCP: true},
	}
}

// publicEverything shows every field of every public note and opens
// the entities.
func publicEverything() sharing.Policy {
	return sharing.Policy{
		Name: "pob-public-everything", Data: "tag==public",
		Fields:       map[string][]string{"note": {sharing.FieldAll}},
		Capabilities: sharing.Capabilities{Search: sharing.SearchModes, Entities: true, MCP: true},
	}
}

// perObjectPrincipal seeds the two notes, the entity both mention and
// the principal granted teamTitles and publicEverything, and returns
// it.
func perObjectPrincipal(t *testing.T, f *Fixture) Principal {
	t.Helper()
	ctx := context.Background()
	driver := f.Server.Driver
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	note := func(id, title, body string, tags ...string) {
		o := &storage.KnowledgeObject{
			ID: id, Type: "note", Status: storage.ObjectStatusActive, RawContent: title + ": " + body,
			ContentHash: "sha256:" + id, Metadata: map[string]any{"title": title}, CreatedAt: at, UpdatedAt: at,
		}
		for _, tag := range tags {
			o.Tags = append(o.Tags, storage.Tag{Label: tag})
		}
		f.create(t, o)
		for _, e := range []*storage.Edge{
			{ID: "pob-mention-" + id, FromType: "object", FromID: id, ToType: "entity", ToID: "ctxt://entity/" + perObjectEntity, EdgeType: storage.EdgeTypeMentions, Weight: 1, CreatedAt: at},
			{ID: "pob-mentioned-in-" + id, FromType: "entity", FromID: "ctxt://entity/" + perObjectEntity, ToType: "object", ToID: id, EdgeType: storage.EdgeTypeMentionedIn, Weight: 1, CreatedAt: at},
		} {
			require.NoError(t, driver.Edges().Create(ctx, e))
		}
	}
	require.NoError(t, driver.Entities().Upsert(ctx, &storage.Entity{Slug: perObjectEntity, Title: "Launch", Namespace: "topic", CreatedAt: at, UpdatedAt: at}))
	note(perObjectPrivate, perObjectTitlePrv, perObjectCanary, "team")
	note(perObjectShared, perObjectTitleShr, perObjectShown, "team", "public")

	sharingtest.EnsurePrincipal(t, driver, perObjectID, storage.PrincipalKindDevice, authn.RoleReader)
	require.NoError(t, driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
		Kind: storage.EntitlementInbound, RegistryName: perObjectID, Plan: "inbound", Namespaces: []string{"*"}, FetchedAt: time.Now(),
	}))
	sharingtest.Grant(t, driver, teamTitles(), perObjectID)
	sharingtest.Grant(t, driver, publicEverything(), perObjectID)
	secret, _, err := authn.NewKeysOver(driver.Auth(), nil).Mint(ctx, authn.MintKey{PrincipalID: perObjectID, Name: "leaktest " + namePerObject})
	require.NoError(t, err)
	v, err := sharing.CompileFor(ctx, driver.Sharing(), perObjectID, "", time.Now())
	require.NoError(t, err)
	require.Len(t, v.Plan().Grants, 2)
	return Principal{Name: namePerObject, ID: perObjectID, Token: secret, viewer: v}
}

// perObjectRead is one rendered read path called as the per-object
// principal.
type perObjectRead struct {
	name string
	// titled reports whether the path's answer carries metadata, so a
	// released note shows its title there (the gRPC objects carry none).
	titled bool
	call   func(ctx context.Context, f *Fixture, p Principal) (Result, error)
}

// perObjectReads lists the read paths that release the two notes:
// fetch by id, list, the RSQL search (ListBySQL), find in every mode
// (full-text, vector, hybrid), and the gRPC fetch, list, search and
// node-aware search.
func perObjectReads() []perObjectRead {
	httpRead := func(method, path string, body any) func(context.Context, *Fixture, Principal) (Result, error) {
		return func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return f.HTTP(ctx, p, method, path, body)
		}
	}
	reads := []perObjectRead{
		{"GET /objects/{private}", true, httpRead(http.MethodGet, "/objects/"+perObjectPrivate, nil)},
		{"GET /objects/{shared}", true, httpRead(http.MethodGet, "/objects/"+perObjectShared, nil)},
		{"GET /objects", true, httpRead(http.MethodGet, "/objects?limit=100", nil)},
		{"GET /search", true, httpRead(http.MethodGet, "/search?q=tag==team", nil)},
	}
	for _, mode := range []string{"fts", "vector", "hybrid"} {
		reads = append(reads, perObjectRead{"POST /find " + mode, true, httpRead(http.MethodPost, "/find", map[string]any{"query": "body", "mode": mode, "limit": 20})})
	}
	return append(reads,
		perObjectRead{"GetObject", false, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).GetObject(f.RPCContext(ctx, p), &pb.GetObjectRequest{Id: perObjectPrivate}))
		}},
		perObjectRead{"ListObjects", false, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).ListObjects(f.RPCContext(ctx, p), &pb.ListObjectsRequest{Limit: 100}))
		}},
		perObjectRead{"Search", false, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).Search(f.RPCContext(ctx, p), &pb.SearchRequest{Query: "tag==team"}))
		}},
		perObjectRead{"NodeAwareSearch", false, func(ctx context.Context, f *Fixture, p Principal) (Result, error) {
			return rpc(pb.NewQueryServiceClient(f.Conn).NodeAwareSearch(f.RPCContext(ctx, p), &pb.NodeAwareSearchRequest{Query: "tag==team"}))
		}},
	)
}

// Every read path renders the private note through the team grant
// alone (its title, never its body) and the shared note through both
// (its body too). A path that answers the private note at all must
// answer its title; one that answers the shared note must answer its
// body.
func TestPerObjectRenderOnEveryReadPath(t *testing.T) {
	perObjectRenderOnEveryReadPath(t, storageutil.NewTestDriver)
}

func perObjectRenderOnEveryReadPath(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	p := perObjectPrincipal(t, f)
	for _, read := range perObjectReads() {
		t.Run(read.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), CallTimeout)
			defer cancel()
			r, err := read.call(ctx, f, p)
			require.NoError(t, err)
			require.False(t, r.ServerError(), "server error: %s", excerpt(r.Body))
			body := string(r.Body)
			t.Logf("HTTP %d, gRPC %s, %d bytes", r.Status, r.Code, len(r.Body))
			assert.NotContains(t, body, perObjectCanary, "the private note's body shows: only the public grant shows a body, and it does not cover the note")
			if read.titled && strings.Contains(body, perObjectPrivate) {
				assert.Contains(t, body, perObjectTitlePrv, "the team grant shows the private note's title")
			}
			if strings.Contains(body, perObjectShared) {
				assert.Contains(t, body, perObjectShown, "the public grant shows the shared note's body")
				if read.titled {
					assert.Contains(t, body, perObjectTitleShr)
				}
			}
		})
	}
	// The paths that release both notes by construction did.
	for _, path := range []string{"/objects?limit=100", "/search?q=tag==team"} {
		body := string(fetch(t, f, p, path).Body)
		assert.Contains(t, body, perObjectPrivate, path)
		assert.Contains(t, body, perObjectShared, path)
	}
}

// A capability-gated surface answers only the objects a grant holding
// the capability covers: the entities capability comes from the
// public grant alone, so the backlinks of the entity both notes
// mention list the shared note and not the private one, over HTTP and
// gRPC; the open listing still shows both.
func TestPerObjectCapabilityLimitsTheSurface(t *testing.T) {
	perObjectCapabilityLimitsTheSurface(t, storageutil.NewTestDriver)
}

func perObjectCapabilityLimitsTheSurface(t *testing.T, newDriver driverFunc) {
	f := Start(t, newDriver(t))
	p := perObjectPrincipal(t, f)
	require.True(t, p.Allows(storage.CapabilityEntities))

	backlinks := fetch(t, f, p, "/entities/"+perObjectEntity+"/backlinks")
	require.Equal(t, http.StatusOK, backlinks.Status, "%s", backlinks.Body)
	assert.Contains(t, string(backlinks.Body), perObjectShared)
	assert.NotContains(t, string(backlinks.Body), perObjectPrivate, "the team grant opens no entity surface")

	ctx, cancel := context.WithTimeout(context.Background(), CallTimeout)
	defer cancel()
	r, err := rpc(pb.NewEntityServiceClient(f.Conn).GetEntityBacklinks(f.RPCContext(ctx, p), &pb.GetEntityBacklinksRequest{Slug: perObjectEntity}))
	require.NoError(t, err)
	assert.Contains(t, string(r.Body), perObjectShared)
	assert.NotContains(t, string(r.Body), perObjectPrivate, "the team grant opens no entity surface")

	list := string(fetch(t, f, p, "/objects?limit=100").Body)
	assert.Contains(t, list, perObjectPrivate, "the listing is open: every grant's rows")
	assert.Contains(t, list, perObjectShared)
}

package sharing_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// teamTitles shows the title of every team note, masks emails and
// phones, and opens full-text search only.
func teamTitles() sharing.Policy {
	return sharing.Policy{
		Name: "team-titles", Data: "type==note;tag==team",
		Fields:       map[string][]string{"note": {sharing.FieldTitle}},
		Redact:       sharing.Redact{Mask: []string{"email", "phone"}},
		Capabilities: sharing.Capabilities{Search: []string{sharing.SearchFTS}},
	}
}

// publicAll shows every field of every public note, masks emails, and
// opens full-text and RSQL search and the entities.
func publicAll() sharing.Policy {
	return sharing.Policy{
		Name: "public-all", Data: "tag==public",
		Fields:       map[string][]string{"note": {sharing.FieldAll}},
		Redact:       sharing.Redact{Mask: []string{"email"}},
		Capabilities: sharing.Capabilities{Search: []string{sharing.SearchFTS, sharing.SearchRSQL}, Entities: true},
	}
}

// twoGrantViewer compiles a viewer under teamTitles (g1) and publicAll
// (g2), with key bound when set.
func twoGrantViewer(t *testing.T, key *sharing.Policy) sharing.Viewer {
	t.Helper()
	v, err := sharing.Compile(sharing.Input{
		PrincipalID: "prn_two", KeyPolicy: key,
		Grants: []sharing.Grant{{ID: "g1", Policy: teamTitles()}, {ID: "g2", Policy: publicAll()}},
	})
	require.NoError(t, err)
	return v
}

// An object renders the fields of the grants covering it alone: the
// title through the team grant, every field through the public grant,
// both together every field, neither only what is always kept. The
// viewer's plan stays the union over both.
func TestObjectPlanMergesOnlyTheCoveringGrants(t *testing.T) {
	v := twoGrantViewer(t, nil)
	for _, tc := range []struct {
		name     string
		covering []bool
		want     []string
	}{
		{"team only", []bool{true, false}, []string{sharing.FieldTitle}},
		{"public only", []bool{false, true}, []string{sharing.FieldAll}},
		{"both", []bool{true, true}, []string{sharing.FieldAll}},
		{"neither", []bool{false, false}, nil},
		{"no coverage", nil, nil},
	} {
		assert.Equal(t, tc.want, v.ObjectPlan(tc.covering).FieldsFor("note"), tc.name)
	}
	assert.Equal(t, []string{sharing.FieldAll}, v.Plan().FieldsFor("note"), "the plan is the union: the most any one object renders")
}

// The masked categories intersect over the covering grants alone: a
// category stays masked when every grant covering the object masks it.
func TestObjectPlanMasksIntersectTheCoveringGrants(t *testing.T) {
	v := twoGrantViewer(t, nil)
	assert.Equal(t, []string{"email", "phone"}, v.ObjectPlan([]bool{true, false}).Masked)
	assert.Equal(t, []string{"email"}, v.ObjectPlan([]bool{false, true}).Masked)
	assert.Equal(t, []string{"email"}, v.ObjectPlan([]bool{true, true}).Masked)
	assert.Equal(t, []string{"email"}, v.Plan().Masked)
}

// A key policy narrows each object's fields after the per-object merge.
func TestObjectPlanNarrowsByTheKeyPolicy(t *testing.T) {
	key := sharing.Policy{Name: "key", Data: "type==note", Fields: map[string][]string{"note": {sharing.FieldTitle, sharing.FieldSummary}}}
	v := twoGrantViewer(t, &key)
	assert.Equal(t, []string{sharing.FieldTitle}, v.ObjectPlan([]bool{true, false}).FieldsFor("note"))
	assert.Equal(t, []string{sharing.FieldTitle, sharing.FieldSummary}, v.ObjectPlan([]bool{false, true}).FieldsFor("note"))
}

// A grant naming only "*" adds its fields to a type another grant
// names: each grant's fields for a type resolve through its own "*"
// entry before the union, so a public object of that type is not
// narrowed to the other grant's entry.
func TestObjectPlanResolvesEachGrantsFallback(t *testing.T) {
	everyType := sharing.Policy{Name: "every-type", Data: "tag==public", Fields: map[string][]string{"*": {sharing.FieldAll}}}
	v, err := sharing.Compile(sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{{ID: "g1", Policy: teamTitles()}, {ID: "g2", Policy: everyType}}})
	require.NoError(t, err)
	assert.Equal(t, []string{sharing.FieldAll}, v.ObjectPlan([]bool{true, true}).FieldsFor("note"))
	assert.Equal(t, []string{sharing.FieldTitle}, v.ObjectPlan([]bool{true, false}).FieldsFor("note"))
	assert.Equal(t, []string{sharing.FieldAll}, v.ObjectPlan([]bool{false, true}).FieldsFor("note"))
}

// Render projects through the covering grants: the team grant alone
// shows a note's title, never its body.
func TestRenderThroughTheCoveringGrants(t *testing.T) {
	v := twoGrantViewer(t, nil)
	obj := noteObject()
	team := sharing.Render(v, obj, []bool{true, false}, nil)
	assert.Empty(t, team.Object.RawContent)
	assert.Equal(t, map[string]any{"title": "Quarterly planning"}, team.Object.Metadata)
	assert.Contains(t, withheldValues(team), obj.RawContent)

	public := sharing.Render(v, obj, []bool{false, true}, nil)
	assert.Equal(t, obj.RawContent, public.Object.RawContent)

	none := sharing.Render(v, obj, nil, nil)
	assert.Empty(t, none.Object.RawContent)
	assert.Empty(t, none.Object.Metadata)
	assert.Equal(t, obj.ID, none.Object.ID)
}

// A surface a capability opens sees the grants holding it alone: the
// predicate is the RSQL grant's, the plan and the request-level gate
// stay the viewer's, and a capability every grant holds changes
// nothing.
func TestForCapabilityNarrowsThePredicateToItsGrants(t *testing.T) {
	v := twoGrantViewer(t, nil)
	rsql := storage.CapabilitySearchPrefix + sharing.SearchRSQL
	n := v.ForCapability(rsql)

	only, err := sharing.Compile(sharing.Input{PrincipalID: "prn_two", Grants: []sharing.Grant{{ID: "g2", Policy: publicAll()}}})
	require.NoError(t, err)
	for _, d := range []search.Dialect{search.DialectSQLite, search.DialectPostgres} {
		got, gargs := n.Predicate(d)
		want, wargs := only.Predicate(d)
		assert.Equal(t, want, got, "%v predicate", d)
		assert.Equal(t, wargs, gargs, "%v args", d)
	}
	assert.Len(t, n.Plan().Grants, 2, "an object renders through every grant covering it")
	assert.True(t, n.Allows(rsql))
	assert.NotEqual(t, v.PolicyKey(), n.PolicyKey())

	fts := storage.CapabilitySearchPrefix + sharing.SearchFTS
	assert.Equal(t, v.PolicyKey(), v.ForCapability(fts).PolicyKey(), "every grant holds fts")
	full, _ := v.Predicate(search.DialectSQLite)
	same, _ := v.ForCapability(fts).Predicate(search.DialectSQLite)
	assert.Equal(t, full, same)
	assert.True(t, sharing.Unrestricted.ForCapability(rsql).IsUnrestricted())
	assert.True(t, sharing.Viewer{}.ForCapability(rsql).MatchesNothing())
}

// everyRead is a driver whose object store answers the searches that
// need an index (vector, full-text, node-aware) with the rows a List
// returns through the same scope, so every rendered read can be called
// over plain rows; after, when set, runs after each Get's read and
// before it returns.
type everyRead struct {
	storage.StorageDriver
	after func()
}

func (d everyRead) Objects() storage.ObjectStore {
	return everyReadObjects{ObjectStore: d.StorageDriver.Objects(), after: d.after}
}

type everyReadObjects struct {
	storage.ObjectStore
	after func()
}

func (s everyReadObjects) Scoped(sc storage.Scope) storage.ObjectStore {
	return everyReadObjects{ObjectStore: s.ObjectStore.(storage.ObjectScoper).Scoped(sc), after: s.after}
}

func (s everyReadObjects) Get(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	o, err := s.ObjectStore.Get(ctx, id)
	if s.after != nil {
		s.after()
	}
	return o, err
}

func (s everyReadObjects) VectorSearch(ctx context.Context, _ storage.VectorQuery, f storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	objs, _, err := s.List(ctx, f)
	return objs, err
}

func (s everyReadObjects) FTSSearch(ctx context.Context, _ string, f storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	objs, _, err := s.List(ctx, f)
	return objs, err
}

func (s everyReadObjects) nodeAware(ctx context.Context, f storage.ObjectFilter) ([]*storage.NodeAwareResult, error) {
	objs, _, err := s.List(ctx, f)
	out := make([]*storage.NodeAwareResult, 0, len(objs))
	for _, o := range objs {
		out = append(out, &storage.NodeAwareResult{Object: o})
	}
	return out, err
}

func (s everyReadObjects) FTSSearchNodeAware(ctx context.Context, _ string, f storage.ObjectFilter, _ storage.NodeAwareFilter) ([]*storage.NodeAwareResult, error) {
	return s.nodeAware(ctx, f)
}

func (s everyReadObjects) VectorSearchNodeAware(ctx context.Context, _ storage.VectorQuery, f storage.ObjectFilter, _ storage.NodeAwareFilter) ([]*storage.NodeAwareResult, error) {
	return s.nodeAware(ctx, f)
}

const (
	privateBody = "private roadmap body that only the public grant would show"
	sharedBody  = "shared release body the public grant shows"
)

// seedTwoNotes stores a team note and a team note that is also public,
// grants prn_two teamTitles and publicAll, and returns its viewer.
func seedTwoNotes(t *testing.T, driver storage.StorageDriver) sharing.Viewer {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	for _, o := range []*storage.KnowledgeObject{
		{ID: "note-private", Type: "note", Status: storage.ObjectStatusActive, RawContent: privateBody, ContentHash: "sha256:private",
			SourceKey: "src:private", ProfileID: "work", Metadata: map[string]any{"title": "Roadmap"}, Tags: []storage.Tag{{Label: "team"}}, CreatedAt: now, UpdatedAt: now},
		{ID: "note-shared", Type: "note", Status: storage.ObjectStatusActive, RawContent: sharedBody, ContentHash: "sha256:shared",
			SourceKey: "src:shared", ProfileID: "work", Metadata: map[string]any{"title": "Release"}, Tags: []storage.Tag{{Label: "team"}, {Label: "public"}}, CreatedAt: now, UpdatedAt: now},
	} {
		require.NoError(t, driver.Objects().Create(ctx, o))
	}
	sharingtest.EnsurePrincipal(t, driver, "prn_two", storage.PrincipalKindPerson, "writer")
	sharingtest.Grant(t, driver, teamTitles(), "prn_two")
	sharingtest.Grant(t, driver, publicAll(), "prn_two")
	v, err := sharing.CompileFor(ctx, driver.Sharing(), "prn_two", "", time.Now())
	require.NoError(t, err)
	require.Len(t, v.Plan().Grants, 2)
	return v
}

// Every rendered read of the scoped driver renders each object through
// the grants covering it: the private note (team grant only) shows its
// title and never its body, the shared note (both grants) its body.
func TestScopedRendersEachObjectThroughItsGrants(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	v := seedTwoNotes(t, driver)
	ctx := scopedCtx(v)
	objects := sharing.Scoped(ctx, everyRead{StorageDriver: driver}, nil).Objects()
	all := storage.ObjectFilter{Limit: 10}

	one := func(o *storage.KnowledgeObject, err error) []*storage.KnowledgeObject {
		require.NoError(t, err)
		return []*storage.KnowledgeObject{o}
	}
	many := func(objs []*storage.KnowledgeObject, err error) []*storage.KnowledgeObject {
		require.NoError(t, err)
		return objs
	}
	counted := func(objs []*storage.KnowledgeObject, _ int, err error) []*storage.KnowledgeObject {
		require.NoError(t, err)
		return objs
	}
	nodes := func(res []*storage.NodeAwareResult, err error) []*storage.KnowledgeObject {
		require.NoError(t, err)
		out := make([]*storage.KnowledgeObject, 0, len(res))
		for _, r := range res {
			out = append(out, r.Object)
		}
		return out
	}
	reads := map[string][]*storage.KnowledgeObject{
		"Get":                   append(one(objects.Get(ctx, "note-private")), one(objects.Get(ctx, "note-shared"))...),
		"GetByContentHash":      append(one(objects.GetByContentHash(ctx, "sha256:private", "work")), one(objects.GetByContentHash(ctx, "sha256:shared", "work"))...),
		"GetBySourceKey":        append(one(objects.GetBySourceKey(ctx, "src:private", "work")), one(objects.GetBySourceKey(ctx, "src:shared", "work"))...),
		"List":                  counted(objects.List(ctx, all)),
		"ListBySQL":             counted(objects.ListBySQL(ctx, "id LIKE ?", []any{"note-%"}, all)),
		"VectorSearch":          many(objects.VectorSearch(ctx, storage.VectorQuery{}, all)),
		"FTSSearch":             many(objects.FTSSearch(ctx, "body", all)),
		"FTSSearchNodeAware":    nodes(objects.FTSSearchNodeAware(ctx, "body", all, storage.NodeAwareFilter{})),
		"VectorSearchNodeAware": nodes(objects.VectorSearchNodeAware(ctx, storage.VectorQuery{}, all, storage.NodeAwareFilter{})),
	}
	for _, name := range sharing.RenderedReads() {
		if name == "ListDueReminders" || name == "ListPendingReminders" {
			continue // refused for a restricted viewer
		}
		objs, ok := reads[name]
		require.True(t, ok, "rendered read %s is not exercised", name)
		byID := map[string]*storage.KnowledgeObject{}
		for _, o := range objs {
			byID[o.ID] = o
		}
		require.Contains(t, byID, "note-private", name)
		require.Contains(t, byID, "note-shared", name)
		assert.Empty(t, byID["note-private"].RawContent, "%s: the private note's body", name)
		assert.Equal(t, map[string]any{"title": "Roadmap"}, byID["note-private"].Metadata, "%s: the private note's title", name)
		assert.Equal(t, sharedBody, byID["note-shared"].RawContent, "%s: the shared note's body", name)
	}
}

// A write back is allowed only when the grants covering the object
// project every field of its type: the shared note (public grant) may
// be written, the private note (team grant's title only) may not.
func TestScopedUpdateFollowsTheCoveringGrants(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	v := seedTwoNotes(t, driver)
	ctx := scopedCtx(v)
	objects := sharing.Scoped(ctx, driver, nil).Objects()

	shared, err := objects.Get(ctx, "note-shared")
	require.NoError(t, err)
	shared.Tags = append(shared.Tags, storage.Tag{Label: "edited"})
	require.NoError(t, objects.Update(ctx, shared))

	private, err := objects.Get(ctx, "note-private")
	require.NoError(t, err)
	private.Tags = append(private.Tags, storage.Tag{Label: "edited"})
	require.ErrorIs(t, objects.Update(ctx, private), storage.ErrRestrictedViewer)
	raw, err := driver.Objects().Get(context.Background(), "note-private")
	require.NoError(t, err)
	assert.Equal(t, privateBody, raw.RawContent, "the stored row is untouched")
}

// A row that stops matching every grant between the read and the
// attribution query renders only what is always kept (fail closed):
// the shared note loses both its tags after Get read it whole.
func TestScopedRowChangedBeforeAttributionFailsClosed(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	v := seedTwoNotes(t, driver)
	ctx := scopedCtx(v)
	changed := false
	after := func() {
		if changed {
			return
		}
		changed = true
		raw, err := driver.Objects().Get(context.Background(), "note-shared")
		require.NoError(t, err)
		raw.Tags = nil
		require.NoError(t, driver.Objects().Update(context.Background(), raw))
	}
	got, err := sharing.Scoped(ctx, everyRead{StorageDriver: driver, after: after}, nil).Objects().Get(ctx, "note-shared")
	require.NoError(t, err)
	require.True(t, changed)
	assert.Equal(t, "note-shared", got.ID)
	assert.Empty(t, got.RawContent, "covered by no grant: nothing but what is always kept")
	assert.Empty(t, got.Metadata)
	assert.Empty(t, got.Tags)
	assert.False(t, strings.Contains(got.TextContent, "release"))
}

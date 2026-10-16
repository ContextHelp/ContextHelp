package sharing_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Every ObjectStore method that returns objects is rendered by the
// scoped driver: a method added to the interface fails here until it
// is listed in renderedReads and overridden.
func TestRenderedReadsCoverEveryObjectReturningMethod(t *testing.T) {
	ko := reflect.TypeOf((*storage.KnowledgeObject)(nil))
	nar := reflect.TypeOf((*storage.NodeAwareResult)(nil))
	returnsObjects := func(m reflect.Method) bool {
		for i := 0; i < m.Type.NumOut(); i++ {
			out := m.Type.Out(i)
			if out == ko || out == nar || (out.Kind() == reflect.Slice && (out.Elem() == ko || out.Elem() == nar)) {
				return true
			}
		}
		return false
	}
	iface := reflect.TypeOf((*storage.ObjectStore)(nil)).Elem()
	listed := sharing.RenderedReads()
	for i := 0; i < iface.NumMethod(); i++ {
		m := iface.Method(i)
		if returnsObjects(m) {
			assert.Contains(t, listed, m.Name, "ObjectStore.%s returns objects and must be rendered", m.Name)
		}
	}
	for _, name := range listed {
		_, ok := iface.MethodByName(name)
		assert.True(t, ok, "renderedReads names %s, which ObjectStore lacks", name)
	}
}

func scopedCtx(v sharing.Viewer) context.Context {
	return sharing.WithViewer(context.Background(), v)
}

// Through Scoped, a restricted viewer's reads release rendered objects
// only, every read records into the request's ledger, the owner gets
// the driver itself, and a context without a viewer refuses.
func TestScopedRendersAndRecords(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := scopedCtx(sharing.Unrestricted)
	obj := sarah()
	require.NoError(t, driver.Objects().Create(ctx, obj))
	hidden := noteObject()
	hidden.Pipeline = ""
	require.NoError(t, driver.Objects().Create(ctx, hidden))

	sharingtest.EnsurePrincipal(t, driver, "prn_scoped", storage.PrincipalKindPerson, "reader")
	tpl, _ := sharing.Template(sharing.TemplateColleagues)
	sharingtest.Grant(t, driver, tpl, "prn_scoped")
	v, err := sharing.CompileFor(ctx, driver.Sharing(), "prn_scoped", "", time.Now())
	require.NoError(t, err)

	assert.Same(t, driver, sharing.Scoped(ctx, driver, nil), "the owner pays nothing")

	ledger := sharing.NewLedger()
	rctx := sharing.WithLedger(scopedCtx(v), ledger)
	st := sharing.Scoped(rctx, driver, nil)

	got, err := st.Objects().Get(rctx, obj.ID)
	require.NoError(t, err)
	assert.NotContains(t, got.Metadata, "email")
	assert.Equal(t, "Sarah Lee | Head of Ops @ Acme\nBoston\nTags: work", got.RawContent)
	assert.True(t, ledger.HasObject(obj.ID))
	assert.False(t, ledger.HasObject(hidden.ID))
	canaries := ledger.Canaries()
	assert.True(t, slices.ContainsFunc(canaries, func(w sharing.Withheld) bool { return w.Value == renderEmail }))

	objs, total, err := st.Objects().List(rctx, storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, objs, 1)
	assert.Empty(t, objs[0].Summaries)

	_, err = st.Objects().Get(rctx, hidden.ID)
	assert.ErrorIs(t, err, storage.ErrNotFound)

	// A rendered object cannot be written back.
	got.Tags = append(got.Tags, storage.Tag{Label: "edited"})
	err = st.Objects().Update(rctx, got)
	assert.ErrorIs(t, err, storage.ErrRestrictedViewer)
	raw, err := driver.Objects().Get(ctx, obj.ID)
	require.NoError(t, err)
	assert.Equal(t, renderEmail, raw.Metadata["email"], "the stored row is untouched")

	// Without a ledger on the context the reads still render.
	st2 := sharing.Scoped(scopedCtx(v), driver, nil)
	got2, err := st2.Objects().Get(scopedCtx(v), obj.ID)
	require.NoError(t, err)
	assert.NotContains(t, got2.Metadata, "phone")

	// No viewer at all: every read refuses.
	none := sharing.Scoped(context.Background(), driver, nil)
	_, err = none.Objects().Get(context.Background(), obj.ID)
	assert.True(t, errors.Is(err, storage.ErrRestrictedViewer))
}

// A viewer whose policy projects every field may write an object back.
func TestScopedEveryFieldMayUpdate(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := scopedCtx(sharing.Unrestricted)
	obj := noteObject()
	obj.Pipeline = ""
	require.NoError(t, driver.Objects().Create(ctx, obj))
	sharingtest.EnsurePrincipal(t, driver, "prn_all", storage.PrincipalKindPerson, "writer")
	sharingtest.GrantEverything(t, driver, "prn_all")
	v, err := sharing.CompileFor(ctx, driver.Sharing(), "prn_all", "", time.Now())
	require.NoError(t, err)
	rctx := scopedCtx(v)
	st := sharing.Scoped(rctx, driver, nil)
	got, err := st.Objects().Get(rctx, obj.ID)
	require.NoError(t, err)
	assert.Equal(t, obj.Summaries, got.Summaries, "every field: nothing withheld")
	got.Tags = append(got.Tags, storage.Tag{Label: "edited"})
	require.NoError(t, st.Objects().Update(rctx, got))
}

// The ledger records entities, edges and both ends of each edge.
func TestScopedLedgerRecordsEntitiesAndEdges(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := scopedCtx(sharing.Unrestricted)
	obj := sarah()
	require.NoError(t, driver.Objects().Create(ctx, obj))
	now := time.Now().UTC()
	require.NoError(t, driver.Entities().Upsert(ctx, &storage.Entity{Slug: "person.sarah-lee", Title: "Sarah Lee", Namespace: "person", CreatedAt: now, UpdatedAt: now}))
	require.NoError(t, driver.Edges().Create(ctx, &storage.Edge{
		ID: "edge-1", FromType: "object", FromID: obj.ID, ToType: "entity", ToID: "ctxt://entity/person.sarah-lee",
		EdgeType: storage.EdgeTypeMentions, Weight: 1, CreatedAt: now,
	}))
	sharingtest.EnsurePrincipal(t, driver, "prn_edges", storage.PrincipalKindPerson, "reader")
	tpl, _ := sharing.Template(sharing.TemplateColleagues)
	sharingtest.Grant(t, driver, tpl, "prn_edges")
	v, err := sharing.CompileFor(ctx, driver.Sharing(), "prn_edges", "", time.Now())
	require.NoError(t, err)

	ledger := sharing.NewLedger()
	rctx := sharing.WithLedger(scopedCtx(v), ledger)
	st := sharing.Scoped(rctx, driver, nil)
	edges, err := st.Edges().ListFrom(rctx, "object", obj.ID)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.True(t, ledger.HasEdge("edge-1"))
	assert.True(t, ledger.HasObject(obj.ID))
	assert.True(t, ledger.HasEntity("person.sarah-lee"))
	assert.True(t, ledger.HasEntity("ctxt://entity/person.sarah-lee"))

	e, err := st.Entities().Get(rctx, "person.sarah-lee")
	require.NoError(t, err)
	assert.Equal(t, "Sarah Lee", e.Title)
	assert.Equal(t, []string{"person.sarah-lee"}, ledger.Entities())
	assert.Equal(t, []string{"edge-1"}, ledger.Edges())
	assert.Equal(t, []string{obj.ID}, ledger.Objects())
}

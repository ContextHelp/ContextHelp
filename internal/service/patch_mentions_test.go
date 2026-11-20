package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func mentionTargets(t *testing.T, svc *Service, id string) []string {
	t.Helper()
	edges, err := svc.store.Edges().ListFrom(ownerCtx(), "object", id)
	require.NoError(t, err)
	var out []string
	for _, e := range edges {
		if e.EdgeType == storage.EdgeTypeMentions {
			out = append(out, e.ToID)
		}
	}
	return out
}

// An edit that adds mentions persists them as the raw path does: thin
// entity rows and mention edges, so the entity's backlinks list the object.
func TestPatchObjectMentionsWriteEntitiesAndEdges(t *testing.T) {
	svc := newFanOutService(t)
	ctx := ownerCtx()
	seedObjectWithMentions(t, svc, "obj-patch-add", nil)

	ms := []string{"@a.b", "@c.d"}
	_, err := svc.PatchObject(ctx, "obj-patch-add", ObjectPatch{Mentions: &ms})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"ctxt://entity/a/b", "ctxt://entity/c/d"}, mentionTargets(t, svc, "obj-patch-add"))
	requireThinEntity(t, svc, "a/b")
	requireThinEntity(t, svc, "a/b")
	objs, err := svc.EntityBacklinks(ctx, "a/b")
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, "obj-patch-add", objs[0].ID)
}

// Dropping a mention in an edit removes the edges the caller's mentions
// field made, both directions; an edge extracted from the object's text
// stays, and the entity rows stay.
func TestPatchObjectDroppedMentionsRemoveUserEdgesOnly(t *testing.T) {
	svc := newFanOutService(t)
	ctx := ownerCtx()
	seedObjectWithMentions(t, svc, "obj-patch-drop", mentionURIs("a/b", "c/d", "t/text"))
	now := time.Now()
	edge := func(to, field string) []*storage.Edge {
		return []*storage.Edge{
			{ID: "f-" + to, FromType: "object", FromID: "obj-patch-drop", ToType: "entity", ToID: "ctxt://entity/" + to,
				EdgeType: storage.EdgeTypeMentions, Weight: 1, CreatedAt: now,
				Metadata: map[string]any{storage.ProvenanceSourceField: field, storage.ProvenanceSourceNode: ""}},
			{ID: "r-" + to, FromType: "entity", FromID: "ctxt://entity/" + to, ToType: "object", ToID: "obj-patch-drop",
				EdgeType: storage.EdgeTypeMentionedIn, Weight: 1, CreatedAt: now,
				Metadata: map[string]any{storage.ProvenanceSourceField: field, storage.ProvenanceSourceNode: ""}},
		}
	}
	var all []*storage.Edge
	all = append(all, edge("a/b", storage.MentionSourceMentions)...)
	all = append(all, edge("c/d", storage.MentionSourceMentions)...)
	all = append(all, edge("t/text", storage.MentionSourceRawContent)...)
	_, err := svc.store.Edges().CreateMissing(ctx, all)
	require.NoError(t, err)

	ms := []string{"@a.b"}
	_, err = svc.PatchObject(ctx, "obj-patch-drop", ObjectPatch{Mentions: &ms})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"ctxt://entity/a/b", "ctxt://entity/t/text"}, mentionTargets(t, svc, "obj-patch-drop"))
	back, err := svc.store.Edges().ListTo(ctx, "object", "obj-patch-drop")
	require.NoError(t, err)
	var froms []string
	for _, e := range back {
		froms = append(froms, e.FromID)
	}
	assert.ElementsMatch(t, []string{"ctxt://entity/a/b", "ctxt://entity/t/text"}, froms)
	requireThinEntity(t, svc, "a/b")
}

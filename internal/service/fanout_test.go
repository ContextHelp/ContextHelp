package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uri "hop.top/cite/scheme"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func newFanOutService(t *testing.T) *Service {
	t.Helper()
	svc := newTestService(t)
	svc.Cfg.FanOut = config.DefaultFanOutConfig()
	return svc
}

func seedObjectWithMentions(t *testing.T, svc *Service, id string, mentions []uri.URI) {
	t.Helper()
	ctx := context.Background()
	obj := &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: "test content with entities",
		Mentions:   mentions,
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
}

func TestFanOut_CreatesEdges(t *testing.T) {
	svc := newFanOutService(t)
	ctx := context.Background()

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "alice"},
		{Scheme: "ctxt", Namespace: "project", ID: "mobile"},
		{Scheme: "ctxt", Namespace: "org", ID: "acme"},
	}
	seedObjectWithMentions(t, svc, "obj-fan-1", mentions)

	result, err := svc.FanOut(ctx, "obj-fan-1")
	require.NoError(t, err)

	assert.Equal(t, "obj-fan-1", result.ObjectID)
	// 3 forward + 3 reverse = 6 edges
	assert.Equal(t, 6, result.EdgesCreated)
	assert.Equal(t, 0, result.EdgesSkipped)
	assert.Len(t, result.EntitySlugs, 3)

	// Verify forward edges exist.
	fwd, err := svc.Store.Edges().ListFrom(ctx, "object", "obj-fan-1")
	require.NoError(t, err)
	assert.Len(t, fwd, 3)
	for _, e := range fwd {
		assert.Equal(t, "mentions", e.EdgeType)
	}

	// Verify reverse edges exist.
	rev, err := svc.Store.Edges().ListTo(ctx, "object", "obj-fan-1")
	require.NoError(t, err)
	assert.Len(t, rev, 3)
	for _, e := range rev {
		assert.Equal(t, "mentioned_in", e.EdgeType)
	}
}

func TestFanOut_Idempotent(t *testing.T) {
	svc := newFanOutService(t)
	ctx := context.Background()

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "bob"},
	}
	seedObjectWithMentions(t, svc, "obj-fan-2", mentions)

	r1, err := svc.FanOut(ctx, "obj-fan-2")
	require.NoError(t, err)
	assert.Equal(t, 2, r1.EdgesCreated)

	r2, err := svc.FanOut(ctx, "obj-fan-2")
	require.NoError(t, err)
	assert.Equal(t, 0, r2.EdgesCreated)
	assert.Equal(t, 2, r2.EdgesSkipped)
}

func TestFanOut_NoMentions(t *testing.T) {
	svc := newFanOutService(t)
	ctx := context.Background()

	seedObjectWithMentions(t, svc, "obj-fan-3", nil)

	result, err := svc.FanOut(ctx, "obj-fan-3")
	require.NoError(t, err)
	assert.Equal(t, 0, result.EdgesCreated)
	assert.Equal(t, 0, result.AuditEntries)
}

func TestFanOut_AuditLogEntry(t *testing.T) {
	svc := newFanOutService(t)
	ctx := context.Background()

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "concept", ID: "graph-db"},
	}
	seedObjectWithMentions(t, svc, "obj-fan-4", mentions)

	result, err := svc.FanOut(ctx, "obj-fan-4")
	require.NoError(t, err)
	assert.Equal(t, 1, result.AuditEntries)

	history, err := svc.Store.AuditLog().GetObjectHistory(ctx, "obj-fan-4")
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "fanout.completed", history[0].EventType)
	assert.Equal(t, "system", history[0].Actor)
}

func TestFanOut_DisabledEntities(t *testing.T) {
	svc := newFanOutService(t)
	svc.Cfg.FanOut.Entities = false
	ctx := context.Background()

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "carol"},
	}
	seedObjectWithMentions(t, svc, "obj-fan-5", mentions)

	result, err := svc.FanOut(ctx, "obj-fan-5")
	require.NoError(t, err)
	assert.Equal(t, 0, result.EdgesCreated)
}

func TestFanOut_DisabledAuditLog(t *testing.T) {
	svc := newFanOutService(t)
	svc.Cfg.FanOut.AuditLog = false
	ctx := context.Background()

	mentions := []uri.URI{
		{Scheme: "ctxt", Namespace: "person", ID: "dave"},
	}
	seedObjectWithMentions(t, svc, "obj-fan-6", mentions)

	result, err := svc.FanOut(ctx, "obj-fan-6")
	require.NoError(t, err)
	assert.Equal(t, 2, result.EdgesCreated)
	assert.Equal(t, 0, result.AuditEntries)
}

func TestFanOut_ObjectNotFound(t *testing.T) {
	svc := newFanOutService(t)
	ctx := context.Background()

	_, err := svc.FanOut(ctx, "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get object")
}

// TestFanOut_EdgesCarryProvenance pins the provenance both directions of
// a mention edge carry: the entity_mention node of the stored graph that
// holds the mention and the field it came from, or the mentions field
// alone when the graph has no node for it.
func TestFanOut_EdgesCarryProvenance(t *testing.T) {
	svc := newFanOutService(t)
	ctx := context.Background()

	alice := uri.URI{Scheme: "ctxt", Namespace: "person", ID: "alice"}
	mobile := uri.URI{Scheme: "ctxt", Namespace: "project", ID: "mobile"}
	nodeID := storage.NewNodeID("obj-fan-7", storage.NodeTypeEntityMention, 0)
	obj := &storage.KnowledgeObject{
		ID:         "obj-fan-7",
		Type:       "text",
		RawContent: "test content with entities",
		Mentions:   []uri.URI{alice, mobile},
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{{
			ID:       nodeID,
			NodeType: storage.NodeTypeEntityMention,
			Label:    alice.String(),
			Metadata: map[string]any{storage.ProvenanceSourceField: storage.MentionSourceRawContent},
		}}},
	}
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))

	_, err := svc.FanOut(ctx, "obj-fan-7")
	require.NoError(t, err)

	want := map[string]map[string]any{
		alice.String():  {storage.ProvenanceSourceNode: nodeID, storage.ProvenanceSourceField: storage.MentionSourceRawContent},
		mobile.String(): {storage.ProvenanceSourceNode: "", storage.ProvenanceSourceField: storage.MentionSourceMentions},
	}
	fwd, err := svc.Store.Edges().ListFrom(ctx, "object", "obj-fan-7")
	require.NoError(t, err)
	require.Len(t, fwd, 2)
	for _, e := range fwd {
		assert.Equal(t, want[e.ToID], e.Metadata, "mentions edge to %s", e.ToID)
	}
	rev, err := svc.Store.Edges().ListTo(ctx, "object", "obj-fan-7")
	require.NoError(t, err)
	require.Len(t, rev, 2)
	for _, e := range rev {
		assert.Equal(t, want[e.FromID], e.Metadata, "mentioned_in edge from %s", e.FromID)
	}
}

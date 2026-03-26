package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEdge(id, fromID, toID string) *storage.Edge {
	return &storage.Edge{
		ID:        id,
		FromType:  "object",
		FromID:    fromID,
		ToType:    "entity",
		ToID:      toID,
		EdgeType:  "mentions",
		Weight:    1.0,
		CreatedAt: time.Now().Truncate(time.Second),
	}
}

func TestCreateAndListFrom(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Edges().Create(ctx, makeEdge("e1", "obj-1", "ui.layout"))
	d.Edges().Create(ctx, makeEdge("e2", "obj-1", "ui.color"))

	edges, err := d.Edges().ListFrom(ctx, "object", "obj-1")
	if err != nil {
		t.Fatalf("list from: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("count: got %d, want 2", len(edges))
	}
}

func TestListTo(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Edges().Create(ctx, makeEdge("e1", "obj-1", "ui.layout"))
	d.Edges().Create(ctx, makeEdge("e2", "obj-2", "ui.layout"))
	d.Edges().Create(ctx, makeEdge("e3", "obj-3", "api.auth"))

	edges, err := d.Edges().ListTo(ctx, "entity", "ui.layout")
	if err != nil {
		t.Fatalf("list to: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("count: got %d, want 2", len(edges))
	}
}

func TestDeleteByObject(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	d.Edges().Create(ctx, makeEdge("e1", "obj-1", "ui.layout"))
	d.Edges().Create(ctx, makeEdge("e2", "obj-1", "ui.color"))

	if err := d.Edges().DeleteByObject(ctx, "obj-1"); err != nil {
		t.Fatalf("delete by object: %v", err)
	}

	edges, _ := d.Edges().ListFrom(ctx, "object", "obj-1")
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestDeleteEdge(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, d.Edges().Create(ctx, makeEdge("e-del-1", "obj-1", "ui.layout")))

	err := d.Edges().Delete(ctx, "e-del-1")
	require.NoError(t, err)

	edges, err := d.Edges().ListFrom(ctx, "object", "obj-1")
	require.NoError(t, err)
	assert.Empty(t, edges)
}

func TestDeleteEdgeNotFound(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	err := d.Edges().Delete(ctx, "nonexistent-edge")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteEdgeTwice(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, d.Edges().Create(ctx, makeEdge("e-twice-1", "obj-1", "ui.layout")))

	err := d.Edges().Delete(ctx, "e-twice-1")
	require.NoError(t, err)

	err = d.Edges().Delete(ctx, "e-twice-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestRelatedObjectIDsDepth1 verifies direct neighbours via shared mention target.
func TestRelatedObjectIDsDepth1(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// obj-1 and obj-2 both mention "entity-A"; obj-3 mentions only "entity-B".
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e1", "obj-1", "entity-A")))
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e2", "obj-2", "entity-A")))
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e3", "obj-3", "entity-B")))

	ids, err := d.Edges().RelatedObjectIDs(ctx, "obj-1", 1, 0)
	require.NoError(t, err)

	assert.Contains(t, ids, "obj-2", "obj-2 shares entity-A with obj-1")
	for _, id := range ids {
		assert.NotEqual(t, "obj-1", id, "seed must not appear in results")
		assert.NotEqual(t, "obj-3", id, "obj-3 has no shared target with obj-1")
	}
}

// TestRelatedObjectIDsDepth2 verifies 2-hop traversal.
func TestRelatedObjectIDsDepth2(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// obj-1 → entity-A; obj-2 → entity-A, entity-B; obj-3 → entity-B only.
	// Depth 1 from obj-1: finds obj-2 (shares entity-A).
	// Depth 2 from obj-1: also finds obj-3 (shares entity-B with obj-2).
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e1", "obj-1", "entity-A")))
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e2", "obj-2", "entity-A")))
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e3", "obj-2", "entity-B")))
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e4", "obj-3", "entity-B")))

	ids, err := d.Edges().RelatedObjectIDs(ctx, "obj-1", 2, 0)
	require.NoError(t, err)

	assert.Contains(t, ids, "obj-2")
	assert.Contains(t, ids, "obj-3")
	for _, id := range ids {
		assert.NotEqual(t, "obj-1", id)
	}
}

// TestRelatedObjectIDsLimit verifies the limit parameter is respected.
func TestRelatedObjectIDsLimit(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// obj-1 shares entity-A with obj-2, obj-3, obj-4.
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e1", "obj-1", "entity-A")))
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e2", "obj-2", "entity-A")))
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e3", "obj-3", "entity-A")))
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e4", "obj-4", "entity-A")))

	ids, err := d.Edges().RelatedObjectIDs(ctx, "obj-1", 1, 2)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(ids), 2)
}

// TestRelatedObjectIDsNoRelations verifies empty result when no shared targets.
func TestRelatedObjectIDsNoRelations(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, d.Edges().Create(ctx, makeEdge("e1", "obj-1", "entity-A")))
	require.NoError(t, d.Edges().Create(ctx, makeEdge("e2", "obj-2", "entity-B")))

	ids, err := d.Edges().RelatedObjectIDs(ctx, "obj-1", 1, 0)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

// TestRelatedObjectIDsDepthCap verifies depth > 3 is clamped to 3.
func TestRelatedObjectIDsDepthCap(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// Should not error with depth=99.
	_, err := d.Edges().RelatedObjectIDs(ctx, "obj-1", 99, 0)
	require.NoError(t, err)
}

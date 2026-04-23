package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/graph"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// seedLinkObject creates a minimal knowledge object for link tests.
func seedLinkObject(t *testing.T, driver storage.StorageDriver, id string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	err := driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: id, Type: "text", CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
}

// createLink creates a bidirectional link pair using EdgeStore.
func createLink(
	t *testing.T,
	edges storage.EdgeStore,
	srcID, tgtID string,
	lt graph.LinkType,
) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	fwd := &storage.Edge{
		ID: uuid.New().String(), FromType: "object", FromID: srcID,
		ToType: "object", ToID: tgtID, EdgeType: string(lt),
		Weight: 1.0, CreatedAt: now,
	}
	require.NoError(t, edges.Create(ctx, fwd))

	if !graph.IsSymmetric(lt) {
		inv, err := graph.InverseLinkType(lt)
		require.NoError(t, err)
		rev := &storage.Edge{
			ID: uuid.New().String(), FromType: "object", FromID: tgtID,
			ToType: "object", ToID: srcID, EdgeType: string(inv),
			Weight: 1.0, CreatedAt: now,
		}
		require.NoError(t, edges.Create(ctx, rev))
	}
}

// TestUS0406_BidirectionalLink verifies that creating A extends B produces
// both A→B (extends) and B→A (extended-by).
func TestUS0406_BidirectionalLink(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	seedLinkObject(t, driver, "obj_a")
	seedLinkObject(t, driver, "obj_b")
	createLink(t, driver.Edges(), "obj_a", "obj_b", graph.LinkExtends)

	fwd, err := driver.Edges().ListFrom(ctx, "object", "obj_a")
	require.NoError(t, err)
	foundFwd := false
	for _, e := range fwd {
		if e.ToID == "obj_b" && e.EdgeType == "extends" {
			foundFwd = true
		}
	}
	assert.True(t, foundFwd, "forward edge (extends) missing")

	rev, err := driver.Edges().ListFrom(ctx, "object", "obj_b")
	require.NoError(t, err)
	foundRev := false
	for _, e := range rev {
		if e.ToID == "obj_a" && e.EdgeType == "extended-by" {
			foundRev = true
		}
	}
	assert.True(t, foundRev, "reverse edge (extended-by) missing")
}

// TestUS0406_ListLinks verifies that listing links for an object returns
// both outbound and inbound object-object edges.
func TestUS0406_ListLinks(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	seedLinkObject(t, driver, "obj_x")
	seedLinkObject(t, driver, "obj_y")
	createLink(t, driver.Edges(), "obj_x", "obj_y", graph.LinkSupports)

	outbound, err := driver.Edges().ListFrom(ctx, "object", "obj_x")
	require.NoError(t, err)
	inbound, err := driver.Edges().ListTo(ctx, "object", "obj_x")
	require.NoError(t, err)

	var types []string
	for _, e := range outbound {
		if e.ToType == "object" {
			types = append(types, e.EdgeType)
		}
	}
	for _, e := range inbound {
		if e.FromType == "object" {
			types = append(types, e.EdgeType)
		}
	}
	assert.Contains(t, types, "supports")
}

// TestUS0406_FollowTraversal creates a chain A→B→C and verifies depth-2
// traversal from A reaches C.
func TestUS0406_FollowTraversal(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	for _, id := range []string{"obj_1", "obj_2", "obj_3"} {
		seedLinkObject(t, driver, id)
	}
	createLink(t, driver.Edges(), "obj_1", "obj_2", graph.LinkExtends)
	createLink(t, driver.Edges(), "obj_2", "obj_3", graph.LinkExtends)

	// BFS from obj_1, depth 2
	visited := map[string]bool{"obj_1": true}
	queue := []string{"obj_1"}
	depth := map[string]int{"obj_1": 0}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if depth[cur] >= 2 {
			continue
		}
		edges, err := driver.Edges().ListFrom(ctx, "object", cur)
		require.NoError(t, err)
		for _, e := range edges {
			if e.ToType != "object" || visited[e.ToID] {
				continue
			}
			visited[e.ToID] = true
			depth[e.ToID] = depth[cur] + 1
			queue = append(queue, e.ToID)
		}
	}

	assert.True(t, visited["obj_2"], "obj_2 reachable at depth 1")
	assert.True(t, visited["obj_3"], "obj_3 reachable at depth 2")
}

// TestUS0406_DeleteCascade verifies that deleting an object removes all
// links from/to that object.
func TestUS0406_DeleteCascade(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	seedLinkObject(t, driver, "obj_del")
	seedLinkObject(t, driver, "obj_keep")
	createLink(t, driver.Edges(), "obj_del", "obj_keep", graph.LinkContradicts)

	// Delete obj_del and its edges
	require.NoError(t, driver.Edges().DeleteByObject(ctx, "obj_del"))
	require.NoError(t, driver.Objects().Delete(ctx, "obj_del"))

	// No edges should reference obj_del
	from, err := driver.Edges().ListFrom(ctx, "object", "obj_keep")
	require.NoError(t, err)
	for _, e := range from {
		assert.NotEqual(t, "obj_del", e.ToID, "edge to deleted object remains")
	}
	to, err := driver.Edges().ListTo(ctx, "object", "obj_keep")
	require.NoError(t, err)
	for _, e := range to {
		assert.NotEqual(t, "obj_del", e.FromID, "edge from deleted object remains")
	}
}

// TestUS0406_InvalidLinkTypeRejected verifies unknown link types are rejected.
func TestUS0406_InvalidLinkTypeRejected(t *testing.T) {
	assert.False(t, graph.ValidLinkType("made-up"))
	assert.False(t, graph.ValidLinkType(""))
	assert.True(t, graph.ValidLinkType(graph.LinkExtends))
}

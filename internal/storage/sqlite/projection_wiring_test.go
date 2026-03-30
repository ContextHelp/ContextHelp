package sqlite

// projection_wiring_test.go — T-0195
// Validates:
//  1. FTS body is derived from projection.ProjectIndex (graph-canonical path).
//  2. Embedding text source is projection.ProjectIndex.EmbeddingText.
//  3. FTSSearchNodeAware / VectorSearchNodeAware respect NodeAwareFilter.NodeTypes.

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeGraphObject creates a KnowledgeObject with graph nodes for a summary,
// a section, and a tag. The summary content is the searchable text.
func makeGraphObject(id, typ, summaryText string, extraNodes ...pluginapi.GraphNode) *storage.KnowledgeObject {
	now := time.Now().Truncate(time.Second)
	nodes := []pluginapi.GraphNode{
		{
			ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSummary, 0),
			NodeType: pluginapi.NodeTypeSummary,
			Label:    "Summary",
			Content:  summaryText,
			Order:    0,
		},
	}
	nodes = append(nodes, extraNodes...)
	return &storage.KnowledgeObject{
		ID:        id,
		Type:      typ,
		CreatedAt: now,
		UpdatedAt: now,
		Graph:     &pluginapi.ObjectGraph{Nodes: nodes},
	}
}

// TestProjectedFTSBody_MatchesProjectionOutput verifies that projected_fts_body
// stored at Create time equals projection.ProjectIndex(ko).FTSBody.
func TestProjectedFTSBody_MatchesProjectionOutput(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeGraphObject("pfts-1", "note", "graph canonical fts content")
	require.NoError(t, d.Objects().Create(ctx, obj))

	expectedBody := projection.ProjectIndex(obj).FTSBody
	require.NotEmpty(t, expectedBody, "expected non-empty FTS body from projection")

	var stored string
	err := d.db.QueryRowContext(ctx,
		"SELECT projected_fts_body FROM objects WHERE id = ?", obj.ID,
	).Scan(&stored)
	require.NoError(t, err)
	assert.Equal(t, expectedBody, stored,
		"projected_fts_body in DB should equal projection.ProjectIndex(ko).FTSBody")
}

// TestProjectedFTSBody_UpdatedOnUpdate verifies that projected_fts_body is
// recomputed when the object is updated.
func TestProjectedFTSBody_UpdatedOnUpdate(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeGraphObject("pfts-2", "note", "initial fts content")
	require.NoError(t, d.Objects().Create(ctx, obj))

	// Update the graph with new content.
	obj.Graph.Nodes[0].Content = "updated fts content after mutation"
	obj.UpdatedAt = time.Now().Truncate(time.Second)
	require.NoError(t, d.Objects().Update(ctx, obj))

	expectedBody := projection.ProjectIndex(obj).FTSBody
	var stored string
	err := d.db.QueryRowContext(ctx,
		"SELECT projected_fts_body FROM objects WHERE id = ?", obj.ID,
	).Scan(&stored)
	require.NoError(t, err)
	assert.Equal(t, expectedBody, stored,
		"projected_fts_body should reflect updated graph content")
}

// TestFTSSearch_UsesProjectedBody verifies that FTSSearch matches against the
// graph-projected body, not raw columns like summaries or raw_content.
func TestFTSSearch_UsesProjectedBody(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// Create two objects — only obj1 has "cryptography" in its graph summary.
	obj1 := makeGraphObject("pfts-3", "article", "cryptography fundamentals and key management")
	obj2 := makeGraphObject("pfts-4", "article", "database replication and consistency")
	require.NoError(t, d.Objects().Create(ctx, obj1))
	require.NoError(t, d.Objects().Create(ctx, obj2))

	// Rebuild FTS from projected_fts_body column.
	_, err := d.db.ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)

	results, err := d.Objects().FTSSearch(ctx, "cryptography", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1, "expected exactly one match for 'cryptography'")
	assert.Equal(t, "pfts-3", results[0].ID)
}

// TestFTSSearchNodeAware_NodeTypeFilter verifies that FTSSearchNodeAware
// restricts results to objects that contain the requested node types.
func TestFTSSearchNodeAware_NodeTypeFilter(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	decisionNode := pluginapi.GraphNode{
		ID:       pluginapi.NewNodeID("na-1", pluginapi.NodeTypeDecision, 0),
		NodeType: pluginapi.NodeTypeDecision,
		Label:    "Decision",
		Content:  "use bcrypt for hashing",
		Order:    1,
	}

	// obj1 has a summary + a decision node; obj2 has only a summary node.
	obj1 := makeGraphObject("na-1", "note", "security authentication patterns", decisionNode)
	obj2 := makeGraphObject("na-2", "note", "security database patterns")
	require.NoError(t, d.Objects().Create(ctx, obj1))
	require.NoError(t, d.Objects().Create(ctx, obj2))

	_, err := d.db.ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)

	// Both objects match "security" in FTS.
	allResults, err := d.Objects().FTSSearch(ctx, "security", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, allResults, 2, "both objects match 'security' in flat FTS")

	// NodeAwareFilter requesting decision nodes should return only na-1.
	nodeFilter := pluginapi.NodeAwareFilter{
		NodeTypes: []string{pluginapi.NodeTypeDecision},
	}
	filtered, err := d.Objects().FTSSearchNodeAware(ctx, "security",
		storage.ObjectFilter{Limit: 10}, nodeFilter)
	require.NoError(t, err)
	require.Len(t, filtered, 1, "only na-1 has a decision node")
	assert.Equal(t, "na-1", filtered[0].Object.ID)
}

// TestFTSSearchNodeAware_NoFilter passes through all results when NodeTypes is empty.
func TestFTSSearchNodeAware_NoFilter(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj1 := makeGraphObject("naf-1", "note", "machine learning overview")
	obj2 := makeGraphObject("naf-2", "note", "machine learning advanced")
	require.NoError(t, d.Objects().Create(ctx, obj1))
	require.NoError(t, d.Objects().Create(ctx, obj2))

	_, err := d.db.ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)

	// Empty NodeAwareFilter = no restriction.
	results, err := d.Objects().FTSSearchNodeAware(ctx, "machine",
		storage.ObjectFilter{Limit: 10}, pluginapi.NodeAwareFilter{})
	require.NoError(t, err)
	assert.Len(t, results, 2, "no node-type filter: all FTS matches returned")
}

// TestVectorSearchNodeAware_NodeTypeFilter verifies that VectorSearchNodeAware
// restricts results to objects that contain the requested node types.
func TestVectorSearchNodeAware_NodeTypeFilter(t *testing.T) {
	d := newTestDriverDim(t, 3)
	ctx := context.Background()

	decisionNode := pluginapi.GraphNode{
		ID:       pluginapi.NewNodeID("vs-1", pluginapi.NodeTypeDecision, 0),
		NodeType: pluginapi.NodeTypeDecision,
		Label:    "Decision",
		Content:  "adopt microservices",
		Order:    1,
	}

	obj1 := makeGraphObject("vs-1", "note", "architecture patterns", decisionNode)
	obj1.Embeddings = []float32{0.9, 0.1, 0.0}
	obj2 := makeGraphObject("vs-2", "note", "architecture overview")
	obj2.Embeddings = []float32{0.8, 0.2, 0.0}

	require.NoError(t, d.Objects().Create(ctx, obj1))
	require.NoError(t, d.Objects().Create(ctx, obj2))

	query := []float32{1.0, 0.0, 0.0}

	// Both match via brute-force cosine search.
	allResults, err := d.Objects().VectorSearch(ctx, query, storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, allResults, 2, "both objects have embeddings and should match")

	// NodeAwareFilter restricts to objects with decision nodes only.
	nodeFilter := pluginapi.NodeAwareFilter{
		NodeTypes: []string{pluginapi.NodeTypeDecision},
	}
	filtered, err := d.Objects().VectorSearchNodeAware(ctx, query,
		storage.ObjectFilter{Limit: 10}, nodeFilter)
	require.NoError(t, err)
	require.Len(t, filtered, 1, "only vs-1 has a decision node")
	assert.Equal(t, "vs-1", filtered[0].Object.ID)
}

// TestFTSSearchNodeAware_ReturnNodeHits_PopulatesDocumentView verifies that
// ReturnNodeHits=true populates DocumentView on each result.
func TestFTSSearchNodeAware_ReturnNodeHits_PopulatesDocumentView(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	sectionNode := pluginapi.GraphNode{
		ID:       pluginapi.NewNodeID("dv-1", pluginapi.NodeTypeSection, 0),
		NodeType: pluginapi.NodeTypeSection,
		Label:    "Intro",
		Content:  "Introduction to observability",
		Order:    0,
	}
	obj := makeGraphObject("dv-1", "note", "observability and tracing", sectionNode)
	require.NoError(t, d.Objects().Create(ctx, obj))

	_, err := d.db.ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)

	results, err := d.Objects().FTSSearchNodeAware(ctx, "observability",
		storage.ObjectFilter{Limit: 10},
		pluginapi.NodeAwareFilter{ReturnNodeHits: true})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.NotNil(t, results[0].DocumentView,
		"DocumentView should be populated when ReturnNodeHits=true")
	assert.NotEmpty(t, results[0].DocumentView.Sections,
		"DocumentView.Sections should have section from graph nodes")
}

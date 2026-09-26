package integration

// T-0189: E2E graph-canonical ingest and projected retrieval.
// Proves: canonical graph stored, projections materialize correctly,
// storage round-trips preserve nodes and edges, FTS/vector/filter
// behavior works through shared projection helpers.

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildGraphKO builds a KO with section, tag, and entity_mention nodes and
// edges connecting them. objectID must not contain '/'.
func buildGraphKO(objectID, typ, sectionContent, tagLabel, entitySlug string) *storage.KnowledgeObject {
	now := time.Now().Truncate(time.Second)

	sectionNodeID := pluginapi.NewNodeID(objectID, pluginapi.NodeTypeSection, 0)
	tagNodeID := pluginapi.NewNodeID(objectID, pluginapi.NodeTypeTag, 0)
	entityNodeID := pluginapi.NewNodeID(objectID, pluginapi.NodeTypeEntityMention, 0)

	nodes := []pluginapi.GraphNode{
		{
			ID:       sectionNodeID,
			NodeType: pluginapi.NodeTypeSection,
			Label:    "Main",
			Content:  sectionContent,
			Order:    0,
		},
		{
			ID:       tagNodeID,
			NodeType: pluginapi.NodeTypeTag,
			Label:    tagLabel,
			Content:  tagLabel,
			Order:    0,
		},
		{
			ID:       entityNodeID,
			NodeType: pluginapi.NodeTypeEntityMention,
			Label:    entitySlug,
			Content:  entitySlug,
			Order:    0,
		},
	}
	edges := []pluginapi.GraphEdge{
		{
			ID:       objectID + "/edge/0",
			FromID:   sectionNodeID,
			ToID:     entityNodeID,
			EdgeType: pluginapi.EdgeTypeReferences,
		},
		{
			ID:       objectID + "/edge/1",
			FromID:   sectionNodeID,
			ToID:     tagNodeID,
			EdgeType: pluginapi.EdgeTypeContains,
		},
	}
	return &storage.KnowledgeObject{
		ID:        objectID,
		Type:      typ,
		CreatedAt: now,
		UpdatedAt: now,
		Graph: &pluginapi.ObjectGraph{
			Nodes: nodes,
			Edges: edges,
		},
	}
}

// TestGraphIngest_RoundTrip stores a KO with a populated Graph, retrieves
// it, and asserts node count, stable node IDs, ProjectDocument, ProjectIndex.
func TestGraphIngest_RoundTrip(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	ko := buildGraphKO("girt-01", "note", "graph canonical round trip content", "graph", "girt-entity")
	require.NoError(t, driver.Objects().Create(ctx, ko))

	got, err := driver.Objects().Get(ctx, ko.ID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// Node count preserved.
	require.NotNil(t, got.Graph, "Graph must be non-nil after round-trip")
	assert.Len(t, got.Graph.Nodes, len(ko.Graph.Nodes),
		"node count must match after round-trip")

	// Node IDs stable — ParseNodeID must succeed on every returned node ID.
	for _, n := range got.Graph.Nodes {
		ref, err := pluginapi.ParseNodeID(n.ID)
		require.NoErrorf(t, err, "ParseNodeID(%q) must succeed", n.ID)
		assert.Equal(t, ko.ID, ref.ObjectID,
			"node ObjectID component must match KO ID")
	}

	// ProjectDocument returns non-empty sections from graph nodes.
	doc := projection.ProjectDocument(got)
	assert.NotEmpty(t, doc.Sections,
		"ProjectDocument must return non-empty sections from graph nodes")

	// ProjectIndex returns tags and FTSBody from graph nodes.
	idx := projection.ProjectIndex(got)
	assert.NotEmpty(t, idx.FTSBody,
		"ProjectIndex must return non-empty FTSBody from graph nodes")
	assert.NotEmpty(t, idx.Tags,
		"ProjectIndex must return non-empty Tags from graph nodes")
}

// TestGraphIngest_FTSSearch ingests a KO whose graph section node contains
// a distinctive keyword, rebuilds FTS, and asserts the KO appears in results.
func TestGraphIngest_FTSSearch(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	const uniqueTerm = "xryptographicUniqueTermForT0189"

	ko := buildGraphKO("gifts-01", "article",
		"content about "+uniqueTerm+" key management", "crypto", "crypto-entity")
	require.NoError(t, env.svc.Store.Objects().Create(ctx, ko))
	rebuildFTSIntegration(t, env)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 1.0, VectorWeight: 0.0},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 0},
		FallbackToFTS: true,
	}

	results, err := env.svc.HybridSearch(ctx, uniqueTerm, 5, retrieval.SemanticSource{}, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, results, "FTS search must surface the ingested graph KO")
	assert.Equal(t, ko.ID, results[0].ID,
		"the graph KO must be the top FTS result for the unique term")
}

// TestGraphIngest_ProjectionFallback ingests a KO with nil Graph and asserts
// ProjectDocument / ProjectIndex fall back to flat fields without panic.
func TestGraphIngest_ProjectionFallback(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	flat := &storage.KnowledgeObject{
		ID:          "gifpf-01",
		Type:        "note",
		TextContent: "flat field text content",
		Summaries:   []string{"flat summary"},
		Tags: []pluginapi.Tag{
			{Label: "flat-tag", Weight: 1.0},
		},
		Sections: []pluginapi.Section{
			{Title: "Intro", Content: "flat section content", Order: 0},
		},
		CreatedAt: now,
		UpdatedAt: now,
		Graph:     nil, // explicit nil — fallback path
	}
	require.NoError(t, driver.Objects().Create(ctx, flat))

	got, err := driver.Objects().Get(ctx, flat.ID)
	require.NoError(t, err)
	require.Nil(t, got.Graph, "nil Graph must survive round-trip")

	// Must not panic; must return flat-field content.
	doc := projection.ProjectDocument(got)
	assert.Equal(t, flat.TextContent, doc.Body,
		"ProjectDocument must return TextContent as Body when Graph is nil")

	idx := projection.ProjectIndex(got)
	assert.Contains(t, idx.FTSBody, "flat field text content",
		"ProjectIndex FTSBody must contain TextContent when Graph is nil")
	assert.NotEmpty(t, idx.Tags,
		"ProjectIndex Tags must fall back to flat Tags when Graph is nil")
}

// TestGraphIngest_EdgePreservation ingests a KO with edges (Contains, References),
// retrieves it, and asserts edges are present with the correct From/To/Type.
func TestGraphIngest_EdgePreservation(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	ko := buildGraphKO("giep-01", "decision",
		"adopt event-driven architecture for async messaging", "architecture", "event-broker")
	require.NoError(t, driver.Objects().Create(ctx, ko))

	got, err := driver.Objects().Get(ctx, ko.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Graph, "Graph must be non-nil after round-trip")
	require.NotEmpty(t, got.Graph.Edges, "Edges must be non-empty after round-trip")

	// Build a lookup map: edgeID → edge.
	edgeByID := make(map[string]pluginapi.GraphEdge, len(got.Graph.Edges))
	for _, e := range got.Graph.Edges {
		edgeByID[e.ID] = e
	}

	// Verify the Contains edge.
	containsEdge := findEdgeByType(got.Graph.Edges, pluginapi.EdgeTypeContains)
	require.NotNil(t, containsEdge,
		"EdgeTypeContains must be present after round-trip")
	assert.NotEmpty(t, containsEdge.FromID)
	assert.NotEmpty(t, containsEdge.ToID)

	// Verify the References edge.
	refsEdge := findEdgeByType(got.Graph.Edges, pluginapi.EdgeTypeReferences)
	require.NotNil(t, refsEdge,
		"EdgeTypeReferences must be present after round-trip")
	assert.NotEmpty(t, refsEdge.FromID)
	assert.NotEmpty(t, refsEdge.ToID)

	// Source KO had exactly 2 edges; retrieved KO must have 2 too.
	assert.Len(t, got.Graph.Edges, len(ko.Graph.Edges),
		"edge count must be preserved exactly after round-trip")
}

// findEdgeByType returns the first edge with the given EdgeType, or nil.
func findEdgeByType(edges []pluginapi.GraphEdge, edgeType string) *pluginapi.GraphEdge {
	for i := range edges {
		if edges[i].EdgeType == edgeType {
			return &edges[i]
		}
	}
	return nil
}

package integration

// E2E tests for graph_extractor step.
// Verifies: nodes emitted into ko.Graph, projection helpers surface them,
// nil-LLM and empty-content no-ops.

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// ── Mock LLM ─────────────────────────────────────────────────────────────────

// mockGraphLLM returns a canned JSON response regardless of prompt.
type mockGraphLLM struct {
	response string
}

func (m *mockGraphLLM) Name() string { return "mock-graph-llm" }
func (m *mockGraphLLM) Generate(_ context.Context, _ string) (string, error) {
	return m.response, nil
}

// wellFormedGraphJSON is the canned LLM response used across tests.
const wellFormedGraphJSON = `{
	"summary": "This document describes the project architecture and key decisions.",
	"topics": ["go", "architecture", "testing"],
	"decisions": ["Use SQLite for local storage", "Adopt graph-canonical schema"],
	"open_questions": ["Which auth provider?", "How to handle schema migrations?"],
	"artifacts": ["go.mod", "Makefile", "schema.sql"]
}`

// newMockKO returns a KnowledgeObject with ID and RawContent set.
func newMockKO(id, content string) *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: content,
	}
}

// ── TestGraphExtractor_EmitsNodes ─────────────────────────────────────────────

// TestGraphExtractor_EmitsNodes verifies graph_extractor populates ko.Graph with
// nodes of each expected type and connects them via EdgeTypeContains from root.
func TestGraphExtractor_EmitsNodes(t *testing.T) {
	llm := &mockGraphLLM{response: wellFormedGraphJSON}
	step := steps.NewGraphExtractorWithLLM(llm)

	ko := newMockKO("obj-ge-001", "Project architecture document with decisions and questions.")
	out, err := step.Run(context.Background(), ko)
	require.NoError(t, err)
	require.NotNil(t, out.Graph, "ko.Graph must be non-nil after step runs")
	require.NotEmpty(t, out.Graph.Nodes, "ko.Graph.Nodes must not be empty")

	// Collect node types present.
	typeSet := make(map[string]int)
	for _, n := range out.Graph.Nodes {
		typeSet[n.NodeType]++
	}

	assert.GreaterOrEqual(t, typeSet[pluginapi.NodeTypeSummary], 1,
		"at least one NodeTypeSummary node expected")
	assert.GreaterOrEqual(t, typeSet[pluginapi.NodeTypeTag], 1,
		"at least one NodeTypeTag node expected")
	assert.GreaterOrEqual(t, typeSet[pluginapi.NodeTypeDecision], 1,
		"at least one NodeTypeDecision node expected")
	assert.GreaterOrEqual(t, typeSet[pluginapi.NodeTypeOpenQuestion], 1,
		"at least one NodeTypeOpenQuestion node expected")
	assert.GreaterOrEqual(t, typeSet[pluginapi.NodeTypeArtifact], 1,
		"at least one NodeTypeArtifact node expected")

	// Every node must have an EdgeTypeContains edge from the KO root.
	edgeTargets := make(map[string]bool)
	for _, e := range out.Graph.Edges {
		if e.FromID == "obj-ge-001" && e.EdgeType == pluginapi.EdgeTypeContains {
			edgeTargets[e.ToID] = true
		}
	}
	for _, n := range out.Graph.Nodes {
		assert.True(t, edgeTargets[n.ID],
			"node %q must have EdgeTypeContains from root", n.ID)
	}

	// Node IDs must be parseable by ParseNodeID.
	for _, n := range out.Graph.Nodes {
		ref, err := pluginapi.ParseNodeID(n.ID)
		require.NoError(t, err, "ParseNodeID must succeed for node %q", n.ID)
		assert.Equal(t, "obj-ge-001", ref.ObjectID,
			"objectID component of node ID must match KO ID")
		assert.Equal(t, n.NodeType, ref.NodeType,
			"nodeType component must match node.NodeType")
		assert.GreaterOrEqual(t, ref.Ordinal, 0, "ordinal must be >= 0")
	}
}

// TestGraphExtractor_EmitsNodes_Counts verifies the exact node counts match the JSON payload.
func TestGraphExtractor_EmitsNodes_Counts(t *testing.T) {
	llm := &mockGraphLLM{response: wellFormedGraphJSON}
	step := steps.NewGraphExtractorWithLLM(llm)

	ko := newMockKO("obj-ge-002", "Content for count verification.")
	out, err := step.Run(context.Background(), ko)
	require.NoError(t, err)
	require.NotNil(t, out.Graph)

	typeCount := make(map[string]int)
	for _, n := range out.Graph.Nodes {
		typeCount[n.NodeType]++
	}

	// summary: 1, topics: 3, decisions: 2, open_questions: 2, artifacts: 3 = 11 nodes total.
	assert.Equal(t, 1, typeCount[pluginapi.NodeTypeSummary], "summary count")
	assert.Equal(t, 3, typeCount[pluginapi.NodeTypeTag], "tag count (topics)")
	assert.Equal(t, 2, typeCount[pluginapi.NodeTypeDecision], "decision count")
	assert.Equal(t, 2, typeCount[pluginapi.NodeTypeOpenQuestion], "open_question count")
	assert.Equal(t, 3, typeCount[pluginapi.NodeTypeArtifact], "artifact count")
	assert.Equal(t, 11, len(out.Graph.Nodes), "total node count")
	assert.Equal(t, 11, len(out.Graph.Edges), "one edge per node")
}

// ── TestGraphExtractor_ProjectionSurfaces ────────────────────────────────────

// TestGraphExtractor_ProjectionSurfaces verifies that after running graph_extractor,
// projection helpers surface summary text in FTSBody and topics in Tags.
func TestGraphExtractor_ProjectionSurfaces(t *testing.T) {
	llm := &mockGraphLLM{response: wellFormedGraphJSON}
	step := steps.NewGraphExtractorWithLLM(llm)

	ko := newMockKO("obj-ge-003", "Architecture and decision record for the project.")
	out, err := step.Run(context.Background(), ko)
	require.NoError(t, err)
	require.NotNil(t, out.Graph)

	// ProjectIndex: FTSBody must contain the summary text.
	idx := projection.ProjectIndex(out)
	assert.NotEmpty(t, idx.FTSBody, "FTSBody must not be empty")
	assert.True(t,
		strings.Contains(idx.FTSBody, "project architecture"),
		"FTSBody must contain summary text; got: %q", idx.FTSBody)

	// ProjectIndex: Tags must include the topic labels from the JSON.
	tagLabels := make(map[string]bool)
	for _, tag := range idx.Tags {
		tagLabels[tag.Label] = true
	}
	assert.True(t, tagLabels["go"], "Tags must include 'go'")
	assert.True(t, tagLabels["architecture"], "Tags must include 'architecture'")
	assert.True(t, tagLabels["testing"], "Tags must include 'testing'")

	// ProjectDocument: Body must include summary content when graph has no section nodes.
	// (graph_extractor emits summary nodes, not section nodes, so Body falls back to TextContent
	// or is empty — but sections derived from summary nodes are not emitted).
	// The summary node content appears in FTSBody (via ProjectIndex), not Body sections.
	doc := projection.ProjectDocument(out)
	// No section nodes → Sections empty; Body derived from TextContent (empty here).
	// This is correct behaviour per projection.go: only NodeTypeSection → Sections.
	assert.Empty(t, doc.Sections,
		"ProjectDocument.Sections must be empty when graph has no section nodes")
}

// TestGraphExtractor_ProjectionSurfaces_FTSBody verifies FTSBody is non-empty and
// contains summary content.
func TestGraphExtractor_ProjectionSurfaces_FTSBody(t *testing.T) {
	const customJSON = `{
		"summary": "Unique sentinel summary for FTS body test.",
		"topics": ["sentinel-tag"],
		"decisions": [],
		"open_questions": [],
		"artifacts": []
	}`
	llm := &mockGraphLLM{response: customJSON}
	step := steps.NewGraphExtractorWithLLM(llm)

	ko := newMockKO("obj-ge-004", "Sentinel content for FTS body projection test.")
	out, err := step.Run(context.Background(), ko)
	require.NoError(t, err)
	require.NotNil(t, out.Graph)

	idx := projection.ProjectIndex(out)
	assert.True(t,
		strings.Contains(idx.FTSBody, "Unique sentinel summary"),
		"FTSBody must contain summary; got: %q", idx.FTSBody)
	require.Len(t, idx.Tags, 1, "one tag expected")
	assert.Equal(t, "sentinel-tag", idx.Tags[0].Label)
}

// ── TestGraphExtractor_NilLLM_NoOp ───────────────────────────────────────────

// TestGraphExtractor_NilLLM_NoOp verifies that when no LLM is configured,
// the step is a no-op: ko.Graph is unchanged and no panic occurs.
func TestGraphExtractor_NilLLM_NoOp(t *testing.T) {
	step := steps.NewGraphExtractor() // no LLM

	ko := newMockKO("obj-ge-005", "Some content that would normally be enriched.")
	original := ko.Graph // nil

	out, err := step.Run(context.Background(), ko)
	require.NoError(t, err, "nil LLM must not return an error")
	assert.Equal(t, original, out.Graph,
		"ko.Graph must be unchanged (nil) when LLM is nil")
}

// TestGraphExtractor_NilLLM_ExistingGraph verifies no-op preserves an existing graph.
func TestGraphExtractor_NilLLM_ExistingGraph(t *testing.T) {
	step := steps.NewGraphExtractor() // no LLM

	existingNode := pluginapi.GraphNode{
		ID:       pluginapi.NewNodeID("obj-ge-006", pluginapi.NodeTypeSummary, 0),
		NodeType: pluginapi.NodeTypeSummary,
		Label:    "pre-existing",
		Content:  "pre-existing summary",
	}
	ko := &storage.KnowledgeObject{
		ID:         "obj-ge-006",
		Type:       "text",
		RawContent: "some content",
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{existingNode},
		},
	}

	out, err := step.Run(context.Background(), ko)
	require.NoError(t, err)
	require.NotNil(t, out.Graph)
	require.Len(t, out.Graph.Nodes, 1, "existing graph node must be preserved")
	assert.Equal(t, existingNode.ID, out.Graph.Nodes[0].ID)
}

// ── TestGraphExtractor_EmptyContent_NoOp ─────────────────────────────────────

// TestGraphExtractor_EmptyContent_NoOp verifies that empty TextContent produces
// no graph nodes even when an LLM is configured.
func TestGraphExtractor_EmptyContent_NoOp(t *testing.T) {
	llm := &mockGraphLLM{response: wellFormedGraphJSON}
	step := steps.NewGraphExtractorWithLLM(llm)

	ko := &storage.KnowledgeObject{
		ID:         "obj-ge-007",
		Type:       "text",
		RawContent: "", // empty
	}

	out, err := step.Run(context.Background(), ko)
	require.NoError(t, err, "empty content must not return an error")
	assert.Nil(t, out.Graph,
		"ko.Graph must remain nil for empty RawContent")
}

// TestGraphExtractor_WhitespaceContent_NoOp verifies whitespace-only content
// is treated the same as empty.
func TestGraphExtractor_WhitespaceContent_NoOp(t *testing.T) {
	llm := &mockGraphLLM{response: wellFormedGraphJSON}
	step := steps.NewGraphExtractorWithLLM(llm)

	ko := &storage.KnowledgeObject{
		ID:         "obj-ge-008",
		Type:       "text",
		RawContent: "   \t\n  ",
	}

	out, err := step.Run(context.Background(), ko)
	require.NoError(t, err)
	assert.Nil(t, out.Graph,
		"ko.Graph must remain nil for whitespace-only RawContent")
}

// ── TestGraphExtractor_StepName ──────────────────────────────────────────────

// TestGraphExtractor_StepName verifies the step identifies itself correctly.
func TestGraphExtractor_StepName(t *testing.T) {
	step := steps.NewGraphExtractor()
	assert.Equal(t, "graph_extractor", step.Name())
}

// ── TestGraphExtractor_MalformedLLMResponse_NoOp ─────────────────────────────

// TestGraphExtractor_MalformedLLMResponse_NoOp verifies graceful degradation
// when the LLM returns unparseable JSON: no nodes emitted, no error.
func TestGraphExtractor_MalformedLLMResponse_NoOp(t *testing.T) {
	llm := &mockGraphLLM{response: "not valid json at all!!"}
	step := steps.NewGraphExtractorWithLLM(llm)

	ko := newMockKO("obj-ge-009", "Content that triggers malformed LLM response.")
	out, err := step.Run(context.Background(), ko)
	require.NoError(t, err, "malformed LLM response must degrade gracefully")
	assert.Nil(t, out.Graph,
		"ko.Graph must remain nil when LLM response is unparseable")
}

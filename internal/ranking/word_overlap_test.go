package ranking_test

import (
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ranking"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
)

// makeObjWithContent returns a flat-field object (no Graph) with given text content.
func makeObjWithContent(id, content string) *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID:          id,
		Type:        "text",
		TextContent: content,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// makeObjWithGraphContent returns an object whose content lives in a Section graph node;
// flat TextContent is intentionally empty to test the graph path.
func makeObjWithGraphContent(id, sectionContent string) *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID:   id,
		Type: "document",
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSection, 0),
					NodeType: pluginapi.NodeTypeSection,
					Label:    "Main",
					Content:  sectionContent,
					Order:    0,
				},
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// TestWordOverlapScore_ExactMatch — all query terms present in projection body → score == 1.0.
func TestWordOverlapScore_ExactMatch(t *testing.T) {
	scorer := ranking.NewWordOverlapScorer()
	obj := makeObjWithContent("a", "the quick brown fox jumps over the lazy dog")
	score := scorer.Score("quick brown fox", obj)
	assert.InDelta(t, 1.0, score, 1e-9, "all query terms should match → score 1.0")
}

// TestWordOverlapScore_NoMatch — no overlap between query and projection → score == 0.0.
func TestWordOverlapScore_NoMatch(t *testing.T) {
	scorer := ranking.NewWordOverlapScorer()
	obj := makeObjWithContent("b", "the quick brown fox")
	score := scorer.Score("elephant rhinoceros", obj)
	assert.InDelta(t, 0.0, score, 1e-9, "no overlap → score 0.0")
}

// TestWordOverlapScore_PartialMatch — half the query terms match → score == 0.5.
func TestWordOverlapScore_PartialMatch(t *testing.T) {
	scorer := ranking.NewWordOverlapScorer()
	obj := makeObjWithContent("c", "the quick brown fox")
	// "quick" matches, "elephant" does not → 1/2 = 0.5
	score := scorer.Score("quick elephant", obj)
	assert.InDelta(t, 0.5, score, 1e-9, "half overlap → score 0.5")
}

// TestWordOverlapScore_UsesProjection — flat TextContent is empty; content lives in graph
// Section nodes. Scorer must still return a non-zero score via ProjectIndex.
func TestWordOverlapScore_UsesProjection(t *testing.T) {
	scorer := ranking.NewWordOverlapScorer()
	// Graph node contains "distributed systems design" but TextContent is empty.
	obj := makeObjWithGraphContent("d", "distributed systems design")
	score := scorer.Score("systems design", obj)
	assert.Greater(t, score, 0.0, "graph-backed content should be scored via ProjectIndex")
	assert.InDelta(t, 1.0, score, 1e-9, "both query terms present in graph content → score 1.0")
}

// TestWordOverlapScore_EmptyQuery — empty query → score 0.0 without panic.
func TestWordOverlapScore_EmptyQuery(t *testing.T) {
	scorer := ranking.NewWordOverlapScorer()
	obj := makeObjWithContent("e", "some content here")
	score := scorer.Score("", obj)
	assert.InDelta(t, 0.0, score, 1e-9, "empty query → score 0.0")
}

// TestWordOverlapScore_TagsScored — query terms that appear in tags (not body) still score.
func TestWordOverlapScore_TagsScored(t *testing.T) {
	scorer := ranking.NewWordOverlapScorer()
	obj := &storage.KnowledgeObject{
		ID:   "f",
		Type: "text",
		Tags: []pluginapi.Tag{{Label: "golang"}, {Label: "concurrency"}},
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID("f", pluginapi.NodeTypeTag, 0),
					NodeType: pluginapi.NodeTypeTag,
					Label:    "golang",
				},
				{
					ID:       pluginapi.NewNodeID("f", pluginapi.NodeTypeTag, 1),
					NodeType: pluginapi.NodeTypeTag,
					Label:    "concurrency",
				},
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	score := scorer.Score("golang", obj)
	assert.Greater(t, score, 0.0, "tag label in projection → non-zero score")
}

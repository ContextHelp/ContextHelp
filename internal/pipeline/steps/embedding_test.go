package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestEmbeddingGeneratorStubSkipsIndexing(t *testing.T) {
	// Stub provider returns nil vector — VectorIndexed stays false.
	// Note: draft has only RawContent; flatIndexProjection does NOT include RawContent,
	// so EmbeddingText is empty and the step exits early (same observable result).
	step := NewEmbeddingGenerator(nil)
	draft := &storage.KnowledgeObject{Summaries: []string{"some text"}}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.VectorIndexed {
		t.Error("VectorIndexed should be false when stub provider returns no vector")
	}
}

func TestEmbeddingGeneratorEmptyContent(t *testing.T) {
	step := NewEmbeddingGenerator(nil)
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.VectorIndexed {
		t.Error("VectorIndexed should be false when content is empty")
	}
}

// TestEmbeddingGenerator_UsesProjectionEmbeddingText verifies that Run derives
// the embedding text from projection.ProjectIndex(draft).EmbeddingText rather
// than raw fields. A graph-canonical draft with a summary node should produce
// a non-empty EmbeddingText via the graph path; a draft with only RawContent
// (not indexed by projection) should produce an empty EmbeddingText → skip.
func TestEmbeddingGenerator_UsesProjectionEmbeddingText(t *testing.T) {
	step := NewEmbeddingGenerator(nil) // stub provider always returns nil vector

	// Graph-canonical draft: summary node provides EmbeddingText via graph path.
	graphDraft := &storage.KnowledgeObject{
		Summaries: []string{"should not be used"}, // flat field; would be empty if only raw
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID("t1", pluginapi.NodeTypeSummary, 0),
					NodeType: pluginapi.NodeTypeSummary,
					Content:  "graph embedding source text",
					Order:    0,
				},
			},
		},
	}
	embText := projection.ProjectIndex(graphDraft).EmbeddingText
	if embText == "" {
		t.Fatal("expected non-empty EmbeddingText from graph projection")
	}
	// Stub returns nil — VectorIndexed false. But the path is confirmed non-empty.
	got, err := step.Run(context.Background(), graphDraft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.VectorIndexed {
		t.Error("stub provider: VectorIndexed should still be false")
	}

	// RawContent-only draft: projection does not include RawContent in EmbeddingText.
	rawOnlyDraft := &storage.KnowledgeObject{RawContent: "only raw content, not indexed"}
	rawEmbText := projection.ProjectIndex(rawOnlyDraft).EmbeddingText
	if rawEmbText != "" {
		t.Errorf("RawContent alone should produce empty EmbeddingText; got %q", rawEmbText)
	}
	gotRaw, err := step.Run(context.Background(), rawOnlyDraft)
	if err != nil {
		t.Fatalf("run raw: %v", err)
	}
	if gotRaw.VectorIndexed {
		t.Error("VectorIndexed should be false for draft with empty EmbeddingText")
	}
}

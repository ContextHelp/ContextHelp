package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// mockLLM returns a fixed response string.
type mockLLM struct{ resp string }

func (m *mockLLM) Name() string { return "mock" }
func (m *mockLLM) Generate(_ context.Context, _ string) (string, error) {
	return m.resp, nil
}

func TestGraphExtractorName(t *testing.T) {
	if got := NewGraphExtractor().Name(); got != "graph_extractor" {
		t.Errorf("Name() = %q, want %q", got, "graph_extractor")
	}
}

func TestGraphExtractorNoLLM(t *testing.T) {
	step := NewGraphExtractor() // nil LLM
	ko := &storage.KnowledgeObject{
		ID:         "obj-1",
		RawContent: "Some interesting content with decisions and questions.",
	}
	got, err := step.Run(context.Background(), ko)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Graph != nil {
		t.Errorf("expected nil Graph when LLM is nil, got %+v", got.Graph)
	}
}

func TestGraphExtractorNoContent(t *testing.T) {
	step := NewGraphExtractorWithLLM(&mockLLM{resp: `{"summary":"x","topics":[]}`})
	ko := &storage.KnowledgeObject{
		ID:         "obj-2",
		RawContent: "",
	}
	got, err := step.Run(context.Background(), ko)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Graph != nil {
		t.Errorf("expected nil Graph for empty content, got %+v", got.Graph)
	}
}

func TestGraphExtractorEmitsNodes(t *testing.T) {
	jsonResp := `{
		"summary": "A test summary",
		"topics": ["go", "testing"],
		"decisions": ["Use SQLite as default storage"],
		"open_questions": ["Which auth provider?"],
		"artifacts": ["go.mod", "Makefile"]
	}`
	step := NewGraphExtractorWithLLM(&mockLLM{resp: jsonResp})
	ko := &storage.KnowledgeObject{
		ID:         "obj-3",
		RawContent: "We decided to use SQLite. Which auth provider should we use?",
	}
	got, err := step.Run(context.Background(), ko)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("expected non-nil Graph")
	}

	// Count nodes by type.
	counts := make(map[string]int)
	for _, n := range got.Graph.Nodes {
		counts[n.NodeType]++
	}

	want := map[string]int{
		pluginapi.NodeTypeSummary:      1,
		pluginapi.NodeTypeTag:          2, // "go", "testing"
		pluginapi.NodeTypeDecision:     1,
		pluginapi.NodeTypeOpenQuestion: 1,
		pluginapi.NodeTypeArtifact:     2, // "go.mod", "Makefile"
	}
	for nodeType, wantCount := range want {
		if got := counts[nodeType]; got != wantCount {
			t.Errorf("node type %q: got %d, want %d", nodeType, got, wantCount)
		}
	}

	// Each node must have a corresponding EdgeTypeContains edge from root.
	edgeTargets := make(map[string]bool)
	for _, e := range got.Graph.Edges {
		if e.EdgeType != pluginapi.EdgeTypeContains {
			t.Errorf("unexpected edge type %q", e.EdgeType)
		}
		if e.FromID != ko.ID {
			t.Errorf("edge FromID = %q, want %q", e.FromID, ko.ID)
		}
		edgeTargets[e.ToID] = true
	}
	for _, n := range got.Graph.Nodes {
		if !edgeTargets[n.ID] {
			t.Errorf("node %q has no incoming contains-edge", n.ID)
		}
	}
}

func TestGraphExtractorSummaryContent(t *testing.T) {
	jsonResp := `{"summary":"Very concise summary","topics":[],"decisions":[],"open_questions":[],"artifacts":[]}`
	step := NewGraphExtractorWithLLM(&mockLLM{resp: jsonResp})
	ko := &storage.KnowledgeObject{
		ID:         "obj-4",
		RawContent: "content",
	}
	got, _ := step.Run(context.Background(), ko)
	if got.Graph == nil || len(got.Graph.Nodes) != 1 {
		t.Fatalf("expected 1 summary node, got %v", got.Graph)
	}
	n := got.Graph.Nodes[0]
	if n.NodeType != pluginapi.NodeTypeSummary {
		t.Errorf("node type = %q, want %q", n.NodeType, pluginapi.NodeTypeSummary)
	}
	if n.Label != "Very concise summary" {
		t.Errorf("node label = %q, want %q", n.Label, "Very concise summary")
	}
}

func TestGraphExtractorMalformedJSONNoOp(t *testing.T) {
	step := NewGraphExtractorWithLLM(&mockLLM{resp: "this is not json"})
	ko := &storage.KnowledgeObject{
		ID:         "obj-5",
		RawContent: "some content",
	}
	got, err := step.Run(context.Background(), ko)
	if err != nil {
		t.Fatalf("unexpected error on malformed JSON: %v", err)
	}
	// Malformed response → no nodes emitted (graceful degrade).
	if got.Graph != nil && len(got.Graph.Nodes) > 0 {
		t.Errorf("expected no nodes on malformed JSON, got %d", len(got.Graph.Nodes))
	}
}

func TestGraphExtractorContract(t *testing.T) {
	step := NewGraphExtractor()
	c := step.Contract()
	if len(c.Requires) == 0 {
		t.Error("Contract().Requires must not be empty")
	}
	if len(c.Produces) == 0 {
		t.Error("Contract().Produces must not be empty")
	}
	foundLLM := false
	for _, cap := range c.Capabilities {
		if cap == "llm" {
			foundLLM = true
		}
	}
	if !foundLLM {
		t.Error("Contract().Capabilities must include \"llm\"")
	}
}

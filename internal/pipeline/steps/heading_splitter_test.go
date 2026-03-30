package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestHeadingSplitterDefaultDepth(t *testing.T) {
	step := NewHeadingSplitter()
	draft := &storage.KnowledgeObject{
		RawContent: "# H1\n\nContent\n\n## H2\n\nMore\n\n### H3 (ignored)\n\nDeep",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Default depth=2, so H3 is not a split point (it appears in H2's body).
	if len(got.Sections) != 2 {
		t.Errorf("expected 2 sections at depth 2, got %d", len(got.Sections))
	}
}

func TestHeadingSplitterCustomDepth(t *testing.T) {
	step := NewHeadingSplitter(WithMaxHeadingDepth(3))
	draft := &storage.KnowledgeObject{
		RawContent: "# H1\n\n## H2\n\n### H3\n\nContent",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 3 {
		t.Errorf("expected 3 sections at depth 3, got %d", len(got.Sections))
	}
}

func TestHeadingSplitterName(t *testing.T) {
	step := NewHeadingSplitter()
	if step.Name() != "heading_splitter" {
		t.Errorf("name: %q", step.Name())
	}
}

func TestHeadingSplitterEmitsGraphNodes(t *testing.T) {
	step := NewHeadingSplitter()
	draft := &storage.KnowledgeObject{
		ID:         "obj-003",
		RawContent: "# H1\n\nContent H1\n\n## H2\n\nContent H2",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}

	rootID := pluginapi.NewNodeID("obj-003", pluginapi.NodeTypeSummary, 0)
	if got.Graph.FindNode(rootID) == nil {
		t.Errorf("root summary node %q not found", rootID)
	}

	// Two headings → two section nodes.
	for i := 0; i < 2; i++ {
		secID := pluginapi.NewNodeID("obj-003", pluginapi.NodeTypeSection, i)
		if got.Graph.FindNode(secID) == nil {
			t.Errorf("section node %q not found", secID)
		}
	}

	if len(got.Graph.Edges) != 2 {
		t.Errorf("edges: got %d, want 2", len(got.Graph.Edges))
	}
}

func TestHeadingSplitterNoGraphWithoutID(t *testing.T) {
	step := NewHeadingSplitter()
	draft := &storage.KnowledgeObject{RawContent: "# H1\n\ncontent"}
	got, _ := step.Run(context.Background(), draft)
	if got.Graph != nil {
		t.Error("expected nil graph when ID is empty")
	}
}

package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestSectionByHeadings(t *testing.T) {
	step := NewSectioner()
	draft := &storage.KnowledgeObject{
		RawContent: "## Heading One\nContent one\n## Heading Two\nContent two",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("sections: got %d, want 2", len(got.Sections))
	}
	if got.Sections[0].Title != "Heading One" {
		t.Errorf("section 0 title: got %q", got.Sections[0].Title)
	}
	if got.Sections[1].Title != "Heading Two" {
		t.Errorf("section 1 title: got %q", got.Sections[1].Title)
	}
}

func TestSectionSingleBlock(t *testing.T) {
	step := NewSectioner()
	draft := &storage.KnowledgeObject{
		RawContent: "Just some text with no headings at all.",
	}

	got, _ := step.Run(context.Background(), draft)
	if len(got.Sections) != 1 {
		t.Fatalf("sections: got %d, want 1", len(got.Sections))
	}
	if got.Sections[0].Title != "" {
		t.Errorf("title should be empty: got %q", got.Sections[0].Title)
	}
}

func TestSectionPreservesOrder(t *testing.T) {
	step := NewSectioner()
	draft := &storage.KnowledgeObject{
		RawContent: "## A\ncontent a\n## B\ncontent b\n## C\ncontent c",
	}

	got, _ := step.Run(context.Background(), draft)
	for i, s := range got.Sections {
		if s.Order != i {
			t.Errorf("section %d order: got %d", i, s.Order)
		}
	}
}

func TestSectionerEmitsGraphNodes(t *testing.T) {
	step := NewSectioner()
	draft := &storage.KnowledgeObject{
		ID:         "obj-002",
		RawContent: "## Alpha\ncontent alpha\n## Beta\ncontent beta",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}

	rootID := pluginapi.NewNodeID("obj-002", pluginapi.NodeTypeSummary, 0)
	if got.Graph.FindNode(rootID) == nil {
		t.Errorf("root summary node %q not found", rootID)
	}

	// Two sections → two section nodes.
	for i := 0; i < 2; i++ {
		secID := pluginapi.NewNodeID("obj-002", pluginapi.NodeTypeSection, i)
		if got.Graph.FindNode(secID) == nil {
			t.Errorf("section node %q not found", secID)
		}
	}

	// Two contains edges expected.
	if len(got.Graph.Edges) != 2 {
		t.Errorf("edges: got %d, want 2", len(got.Graph.Edges))
	}
	for _, e := range got.Graph.Edges {
		if e.EdgeType != pluginapi.EdgeTypeContains {
			t.Errorf("edge type: got %q, want %q", e.EdgeType, pluginapi.EdgeTypeContains)
		}
		if e.FromID != rootID {
			t.Errorf("edge from: got %q, want %q", e.FromID, rootID)
		}
	}
}

func TestSectionerNoGraphWithoutID(t *testing.T) {
	step := NewSectioner()
	draft := &storage.KnowledgeObject{RawContent: "## A\ncontent"}
	got, _ := step.Run(context.Background(), draft)
	if got.Graph != nil {
		t.Error("expected nil graph when ID is empty")
	}
}

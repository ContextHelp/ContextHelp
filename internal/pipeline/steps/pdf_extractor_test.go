package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestPDFExtractorCreatesPageSections(t *testing.T) {
	step := NewPDFExtractor()
	draft := &storage.KnowledgeObject{
		Source: "/tmp/test.pdf",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) == 0 {
		t.Fatal("expected page sections")
	}
	if got.Metadata["pdf_page_count"] == nil {
		t.Error("expected pdf_page_count")
	}
	if got.RawContent == "" {
		t.Error("expected RawContent to be set")
	}
}

func TestPDFExtractorName(t *testing.T) {
	step := NewPDFExtractor()
	if step.Name() != "pdf_extractor" {
		t.Errorf("name: %q", step.Name())
	}
}

func TestPDFExtractorEmitsGraphNodes(t *testing.T) {
	step := NewPDFExtractor()
	draft := &storage.KnowledgeObject{
		ID:     "obj-pdf-001",
		Source: "/tmp/test.pdf",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}
	rootID := pluginapi.NewNodeID("obj-pdf-001", pluginapi.NodeTypeSummary, 0)
	if got.Graph.FindNode(rootID) == nil {
		t.Errorf("root summary node %q not found", rootID)
	}
	var sections []*pluginapi.GraphNode
	for i := range got.Graph.Nodes {
		if got.Graph.Nodes[i].NodeType == pluginapi.NodeTypeSection {
			sections = append(sections, &got.Graph.Nodes[i])
		}
	}
	if len(sections) == 0 {
		t.Error("expected at least one section node")
	}
	if len(got.Graph.Edges) == 0 {
		t.Error("expected edges between root and section nodes")
	}
}

func TestPDFExtractorNoGraphWithoutID(t *testing.T) {
	step := NewPDFExtractor()
	draft := &storage.KnowledgeObject{Source: "/tmp/test.pdf"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil && len(got.Graph.Nodes) > 0 {
		t.Error("expected no graph nodes when ID is empty")
	}
}

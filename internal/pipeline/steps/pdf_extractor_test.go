package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestPDFExtractorCreatesPageSections(t *testing.T) {
	step := NewPDFExtractor()
	draft := &storage.KnowledgeObject{
		Source:  "/tmp/test.pdf",
		Subtype: "pdf",
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
		ID:      "obj-pdf-001",
		Source:  "/tmp/test.pdf",
		Subtype: "pdf",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}
	for _, n := range got.Graph.Nodes {
		if n.NodeType == storage.NodeTypeSummary {
			t.Errorf("unexpected summary node %+v: the extracted text lives in the page nodes", n)
		}
	}
	var sections []*storage.GraphNode
	for i := range got.Graph.Nodes {
		if got.Graph.Nodes[i].NodeType == storage.NodeTypeSection {
			sections = append(sections, &got.Graph.Nodes[i])
		}
	}
	if len(sections) == 0 {
		t.Fatal("expected at least one section node")
	}
	if sections[0].Metadata["representation"] != "extracted_text" || sections[0].Metadata["page_number"] == nil {
		t.Errorf("section metadata: %v", sections[0].Metadata)
	}
}

func TestPDFExtractorNoGraphWithoutID(t *testing.T) {
	step := NewPDFExtractor()
	draft := &storage.KnowledgeObject{Source: "/tmp/test.pdf", Subtype: "pdf"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil && len(got.Graph.Nodes) > 0 {
		t.Error("expected no graph nodes when ID is empty")
	}
}

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestOfficeExtractorCreatesPageSections(t *testing.T) {
	step := NewOfficeExtractor()
	draft := &storage.KnowledgeObject{
		Source:  "/tmp/test.docx",
		Subtype: "office",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) == 0 {
		t.Fatal("expected page sections")
	}
	if got.Metadata["office_page_count"] == nil {
		t.Error("expected office_page_count")
	}
	if got.RawContent == "" {
		t.Error("expected RawContent to be set")
	}
}

func TestOfficeExtractorName(t *testing.T) {
	step := NewOfficeExtractor()
	if step.Name() != "office_extractor" {
		t.Errorf("name: %q", step.Name())
	}
}

func TestOfficeExtractorEmitsGraphNodes(t *testing.T) {
	step := NewOfficeExtractor()
	draft := &storage.KnowledgeObject{
		ID:      "obj-office-001",
		Source:  "/tmp/test.docx",
		Subtype: "office",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}
	rootID := pluginapi.NewNodeID("obj-office-001", pluginapi.NodeTypeSummary, 0)
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

func TestOfficeExtractorNoGraphWithoutID(t *testing.T) {
	step := NewOfficeExtractor()
	draft := &storage.KnowledgeObject{Source: "/tmp/test.docx", Subtype: "office"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil && len(got.Graph.Nodes) > 0 {
		t.Error("expected no graph nodes when ID is empty")
	}
}

// A failed extraction stops the pipeline: the draft still holds the file
// bytes filereader read, and nothing downstream may embed them.
func TestOfficeExtractorFailsWhenExtractionFails(t *testing.T) {
	raw := "Quokka survey report, not a zip archive"
	path := filepath.Join(t.TempDir(), "survey.docx")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	step := NewOfficeExtractor(WithOfficeDocumentProvider(providers.NewGolibDocumentProvider()))
	draft := &storage.KnowledgeObject{Source: path, Subtype: "office", RawContent: raw}
	got, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatalf("extraction of a corrupt .docx succeeded; RawContent=%q", got.RawContent)
	}
	if got != nil {
		t.Errorf("returned a draft alongside the error: RawContent=%q", got.RawContent)
	}
}

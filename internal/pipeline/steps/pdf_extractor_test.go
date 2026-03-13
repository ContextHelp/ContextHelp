package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
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

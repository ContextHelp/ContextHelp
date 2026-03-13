package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestOfficeExtractorCreatesPageSections(t *testing.T) {
	step := NewOfficeExtractor()
	draft := &storage.KnowledgeObject{
		Source: "/tmp/test.docx",
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

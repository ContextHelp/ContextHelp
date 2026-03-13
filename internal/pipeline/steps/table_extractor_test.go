package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestTableExtractorFindsTables(t *testing.T) {
	step := NewTableExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "# Doc\n\n| Name | Value |\n|------|-------|\n| foo  | bar   |\n\nSome text\n\n| A | B |\n|---|---|\n| 1 | 2 |",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["table_count"] != 2 {
		t.Errorf("table_count: %v", got.Metadata["table_count"])
	}
	if len(got.Sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(got.Sections))
	}
	for _, sec := range got.Sections {
		if sec.Metadata["type"] != "table" {
			t.Errorf("section type: %v", sec.Metadata["type"])
		}
	}
}

func TestTableExtractorNoTables(t *testing.T) {
	step := NewTableExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "No tables here, just text.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["table_count"] != 0 {
		t.Errorf("table_count: %v", got.Metadata["table_count"])
	}
}

func TestTableExtractorName(t *testing.T) {
	step := NewTableExtractor()
	if step.Name() != "table_extractor" {
		t.Errorf("name: %q", step.Name())
	}
}

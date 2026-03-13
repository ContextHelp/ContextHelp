package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestJSONLParserBasic(t *testing.T) {
	content := `{"content":"first record","type":"note"}
{"content":"second record"}
`
	step := NewJSONLParser()
	draft := &storage.KnowledgeObject{RawContent: content}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	records, ok := got.Metadata["import_records"].([]map[string]any)
	if !ok {
		t.Fatalf("import_records wrong type: %T", got.Metadata["import_records"])
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}
	if got.Metadata["record_count"] != 2 {
		t.Errorf("record_count: got %v", got.Metadata["record_count"])
	}
}

func TestJSONLParserSkipsBlankLines(t *testing.T) {
	content := `{"content":"a"}

{"content":"b"}
`
	step := NewJSONLParser()
	draft := &storage.KnowledgeObject{RawContent: content}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	records := got.Metadata["import_records"].([]map[string]any)
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}
}

func TestJSONLParserInvalidJSON(t *testing.T) {
	content := `{"content":"ok"}
not json`
	step := NewJSONLParser()
	draft := &storage.KnowledgeObject{RawContent: content}
	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for invalid JSON line")
	}
}

func TestJSONLParserEmpty(t *testing.T) {
	step := NewJSONLParser()
	draft := &storage.KnowledgeObject{RawContent: ""}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	records := got.Metadata["import_records"].([]map[string]any)
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

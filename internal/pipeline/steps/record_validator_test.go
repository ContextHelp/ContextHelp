package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestRecordValidatorJSONL(t *testing.T) {
	records := []map[string]any{
		{"content": "valid content"},       // valid: content field present and non-empty
		{"content": ""},                    // invalid: content field empty
		{"other": "some other value"},      // valid: has a non-empty string value
		{"empty_field": ""},               // invalid: all string values empty
	}
	step := NewRecordValidator()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"import_records": records},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Records 1 and 3 are valid; records 2 and 4 are invalid.
	if got.Metadata["valid_count"] != 2 {
		t.Errorf("valid_count: got %v, want 2", got.Metadata["valid_count"])
	}
	if got.Metadata["error_count"] != 2 {
		t.Errorf("error_count: got %v, want 2", got.Metadata["error_count"])
	}
}

func TestRecordValidatorCSV(t *testing.T) {
	rows := []map[string]string{
		{"content": "hello"},
		{"content": ""},
	}
	step := NewRecordValidator()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"csv_rows": rows},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["valid_count"] != 1 {
		t.Errorf("valid_count: got %v", got.Metadata["valid_count"])
	}
	if got.Metadata["error_count"] != 1 {
		t.Errorf("error_count: got %v", got.Metadata["error_count"])
	}
}

func TestRecordValidatorEmpty(t *testing.T) {
	step := NewRecordValidator()
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["valid_count"] != 0 || got.Metadata["error_count"] != 0 {
		t.Errorf("expected 0/0, got %v/%v", got.Metadata["valid_count"], got.Metadata["error_count"])
	}
}

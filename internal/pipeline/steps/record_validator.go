package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RecordValidator validates import records, splitting them into valid and errored sets.
// It accepts records from draft.Metadata["import_records"] ([]map[string]any)
// or draft.Metadata["csv_rows"] ([]map[string]string).
type RecordValidator struct {
	pipeline.BaseContract
}

// NewRecordValidator creates a RecordValidator.
func NewRecordValidator() *RecordValidator {
	return &RecordValidator{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *RecordValidator) Name() string { return "record_validator" }

func (s *RecordValidator) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	validCount := 0
	errorCount := 0
	var errs []string

	// Check import_records (JSONL source).
	if raw, ok := draft.Metadata["import_records"]; ok {
		records, ok := raw.([]map[string]any)
		if !ok {
			return nil, fmt.Errorf("record_validator: import_records has unexpected type %T", raw)
		}
		for i, rec := range records {
			if hasContent(rec) {
				validCount++
			} else {
				errorCount++
				errs = append(errs, fmt.Sprintf("record %d: missing non-empty content field", i+1))
			}
		}
	}

	// Check csv_rows (CSV source).
	if raw, ok := draft.Metadata["csv_rows"]; ok {
		rows, ok := raw.([]map[string]string)
		if !ok {
			return nil, fmt.Errorf("record_validator: csv_rows has unexpected type %T", raw)
		}
		for i, row := range rows {
			if hasContentStr(row) {
				validCount++
			} else {
				errorCount++
				errs = append(errs, fmt.Sprintf("csv row %d: all fields are empty", i+1))
			}
		}
	}

	draft.Metadata["valid_count"] = validCount
	draft.Metadata["error_count"] = errorCount
	draft.Metadata["validation_errors"] = errs
	return draft, nil
}

// hasContent checks that a JSON record has at least one non-empty string value.
func hasContent(rec map[string]any) bool {
	if v, ok := rec["content"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return true
		}
	}
	// Fallback: any non-empty string value.
	for _, v := range rec {
		if s, ok := v.(string); ok && s != "" {
			return true
		}
	}
	return false
}

// hasContentStr checks that a CSV row has at least one non-empty value.
func hasContentStr(row map[string]string) bool {
	for _, v := range row {
		if v != "" {
			return true
		}
	}
	return false
}

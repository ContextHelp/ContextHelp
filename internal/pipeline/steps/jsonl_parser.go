package steps

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// JSONLParser reads draft.RawContent line by line, unmarshaling each as JSON.
type JSONLParser struct {
	pipeline.BaseContract
}

// NewJSONLParser creates a JSONLParser.
func NewJSONLParser() *JSONLParser {
	return &JSONLParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Binary:   true,
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *JSONLParser) Name() string { return "jsonl_parser" }

func (s *JSONLParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	text, err := textInput("jsonl_parser", draft)
	if err != nil {
		return nil, err
	}
	records := make([]map[string]any, 0)
	scanner := bufio.NewScanner(strings.NewReader(text))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("jsonl_parser: line %d: %w", lineNum, err)
		}
		cleaned, _ := cleanJSON(rec).(map[string]any)
		records = append(records, cleaned)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("jsonl_parser: read: %w", err)
	}

	draft.Metadata["import_records"] = records
	draft.Metadata["record_count"] = len(records)
	return draft, nil
}

// cleanJSON is v with every string, map key included, as clean text (see
// cleanText): a JSON \u0000 escape decodes to a NUL a database refuses.
func cleanJSON(v any) any {
	switch t := v.(type) {
	case string:
		return cleanText(t)
	case []any:
		for i, e := range t {
			t[i] = cleanJSON(e)
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[cleanText(k)] = cleanJSON(e)
		}
		return out
	default:
		return v
	}
}

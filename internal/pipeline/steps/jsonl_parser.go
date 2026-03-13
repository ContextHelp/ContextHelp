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

	records := make([]map[string]any, 0)
	scanner := bufio.NewScanner(strings.NewReader(draft.RawContent))
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
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("jsonl_parser: read: %w", err)
	}

	draft.Metadata["import_records"] = records
	draft.Metadata["record_count"] = len(records)
	return draft, nil
}

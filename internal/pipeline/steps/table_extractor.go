package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TableExtractor extracts Markdown tables from content.
type TableExtractor struct {
	pipeline.BaseContract
}

// NewTableExtractor creates a TableExtractor.
func NewTableExtractor() *TableExtractor {
	return &TableExtractor{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Sections"},
		}),
	}
}

func (s *TableExtractor) Name() string { return "table_extractor" }

func (s *TableExtractor) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	lines := strings.Split(draft.RawContent, "\n")
	var tableLines []string
	var tables []string
	inTable := false

	flushTable := func() {
		if len(tableLines) > 0 {
			tables = append(tables, strings.Join(tableLines, "\n"))
			tableLines = nil
		}
		inTable = false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			inTable = true
			tableLines = append(tableLines, line)
		} else {
			if inTable {
				flushTable()
			}
		}
	}
	if inTable {
		flushTable()
	}

	for i, table := range tables {
		draft.Sections = append(draft.Sections, storage.Section{
			Title:   fmt.Sprintf("Table %d", i+1),
			Content: table,
			Order:   len(draft.Sections),
			Metadata: map[string]any{
				"type": "table",
			},
		})
	}

	draft.Metadata["table_count"] = len(tables)

	return draft, nil
}

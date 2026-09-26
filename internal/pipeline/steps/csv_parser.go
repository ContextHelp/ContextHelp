package steps

import (
	"context"
	"encoding/csv"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CSVParser reads draft.RawContent as CSV, using the first row as headers.
// Without an explicit delimiter, a draft whose source ends in .tsv is read
// tab-separated and any other comma-separated.
type CSVParser struct {
	pipeline.BaseContract
	delimiter rune
}

// CSVParserOption configures a CSVParser.
type CSVParserOption func(*CSVParser)

// WithDelimiter sets a custom field delimiter (default picks by source).
func WithDelimiter(d rune) CSVParserOption {
	return func(p *CSVParser) { p.delimiter = d }
}

// NewCSVParser creates a CSVParser with optional configuration.
func NewCSVParser(opts ...CSVParserOption) *CSVParser {
	p := &CSVParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (s *CSVParser) Name() string { return "csv_parser" }

func (s *CSVParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	r := csv.NewReader(strings.NewReader(draft.RawContent))
	r.Comma = s.delimiter
	if r.Comma == 0 {
		r.Comma = ','
		if strings.EqualFold(filepath.Ext(draft.Source), ".tsv") {
			r.Comma = '\t'
		}
	}

	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv_parser: %w", err)
	}

	if len(rows) == 0 {
		draft.Metadata["csv_rows"] = []map[string]string{}
		return draft, nil
	}

	headers := rows[0]
	result := make([]map[string]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		rec := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(row) {
				rec[h] = row[i]
			}
		}
		result = append(result, rec)
	}

	draft.Metadata["csv_rows"] = result
	return draft, nil
}

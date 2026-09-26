package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// BatchEnqueuer stages each import record (Metadata["import_records"] from
// JSONL, or Metadata["csv_rows"] from CSV/TSV) for ingestion as an object of
// its own: one draft.Sections entry each on the container, and one
// Metadata["items_to_enqueue"] entry the worker turns into an item job.
//
// A record's "content" field is its payload; a record without one is
// ingested as its "key: value" lines. Its "source" field, when set, is the
// item's source. Records with no text at all are skipped.
type BatchEnqueuer struct {
	pipeline.BaseContract
}

// NewBatchEnqueuer creates a BatchEnqueuer.
func NewBatchEnqueuer() *BatchEnqueuer {
	return &BatchEnqueuer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Sections", "Metadata"},
		}),
	}
}

func (s *BatchEnqueuer) Name() string { return "batch_enqueuer" }

func (s *BatchEnqueuer) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	records, err := batchRecords(draft.Metadata)
	if err != nil {
		return nil, err
	}

	sections := make([]storage.Section, 0, len(records))
	staged := make([]fanOutItem, 0, len(records))
	for _, rec := range records {
		content := recordContent(rec)
		if strings.TrimSpace(content) == "" {
			continue
		}
		title := content
		if len(title) > 80 {
			title = title[:80]
		}
		sections = append(sections, storage.Section{
			Title:   title,
			Content: content,
			Order:   len(sections),
		})
		staged = append(staged, fanOutItem{
			Title:   title,
			Content: content,
			Source:  firstString(rec, "source"),
		})
	}

	draft.Sections = sections
	draft.Metadata[itemsToEnqueueKey] = []map[string]any{}
	stageItems(draft, staged)
	draft.Metadata["batch_queued"] = len(sections)
	return draft, nil
}

// batchRecords returns the parsed records of a JSONL or CSV/TSV batch.
func batchRecords(meta map[string]any) ([]map[string]any, error) {
	if raw, ok := meta["import_records"]; ok {
		records, ok := raw.([]map[string]any)
		if !ok {
			return nil, fmt.Errorf("batch_enqueuer: import_records has unexpected type %T", raw)
		}
		return records, nil
	}
	if raw, ok := meta["csv_rows"]; ok {
		rows, ok := raw.([]map[string]string)
		if !ok {
			return nil, fmt.Errorf("batch_enqueuer: csv_rows has unexpected type %T", raw)
		}
		records := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			rec := make(map[string]any, len(row))
			for k, v := range row {
				rec[k] = v
			}
			records = append(records, rec)
		}
		return records, nil
	}
	return nil, nil
}

// recordContent is a record's "content" field, or else its non-empty string
// fields as sorted "key: value" lines.
func recordContent(rec map[string]any) string {
	if c := firstString(rec, "content"); c != "" {
		return c
	}
	lines := make([]string, 0, len(rec))
	for k, v := range rec {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			lines = append(lines, k+": "+s)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

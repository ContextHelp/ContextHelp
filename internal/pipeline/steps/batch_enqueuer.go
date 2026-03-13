package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// BatchEnqueuer converts valid import records into Sections on the draft.
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

	raw, ok := draft.Metadata["import_records"]
	if !ok {
		draft.Metadata["batch_queued"] = 0
		return draft, nil
	}

	records, ok := raw.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("batch_enqueuer: import_records has unexpected type %T", raw)
	}

	sections := make([]storage.Section, 0, len(records))
	for i, rec := range records {
		content, _ := rec["content"].(string)
		title := content
		if len(title) > 80 {
			title = title[:80]
		}
		sections = append(sections, storage.Section{
			Title:   title,
			Content: content,
			Order:   i,
		})
	}

	draft.Sections = sections
	draft.Metadata["batch_queued"] = len(sections)
	return draft, nil
}

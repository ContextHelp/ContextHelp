package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ItemEnqueuer reads new feed items and stages them for ingestion by populating
// draft.Sections and draft.Metadata["items_to_enqueue"].
// It does NOT write to a job store to avoid circular dependencies.
type ItemEnqueuer struct {
	pipeline.BaseContract
}

// NewItemEnqueuer creates an ItemEnqueuer.
func NewItemEnqueuer() *ItemEnqueuer {
	return &ItemEnqueuer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Sections", "Metadata"},
		}),
	}
}

func (s *ItemEnqueuer) Name() string { return "item_enqueuer" }

func (s *ItemEnqueuer) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	rawItems, ok := draft.Metadata["feed_items_new"]
	if !ok {
		draft.Metadata["items_to_enqueue"] = []map[string]any{}
		return draft, nil
	}

	items, ok := rawItems.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("item_enqueuer: feed_items_new has unexpected type %T", rawItems)
	}

	sections := make([]storage.Section, 0, len(items))
	pending := make([]map[string]any, 0, len(items))

	for i, item := range items {
		title, _ := item["title"].(string)
		link, _ := item["link"].(string)
		content, _ := item["content"].(string)

		sectionContent := link
		if content != "" {
			sectionContent = content
		}

		sections = append(sections, storage.Section{
			Title:   title,
			Content: sectionContent,
			Order:   i,
		})
		pending = append(pending, item)
	}

	draft.Sections = sections
	draft.Metadata["items_to_enqueue"] = pending
	return draft, nil
}

package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ItemEnqueuer stages new items (Metadata["feed_items_new"]) for ingestion as
// objects of their own: one draft.Sections entry each on the container, and
// one Metadata["items_to_enqueue"] entry the worker turns into an item job.
// An item runs its own "pipeline" when set, else the text pipeline on its
// content, else the URL pipeline on its link. It does NOT write to a job
// store to avoid circular dependencies.
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
		draft.Metadata[itemsToEnqueueKey] = []map[string]any{}
		return draft, nil
	}

	items, ok := rawItems.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("item_enqueuer: feed_items_new has unexpected type %T", rawItems)
	}

	sections := make([]storage.Section, 0, len(items))
	staged := make([]fanOutItem, 0, len(items))
	for i, item := range items {
		title, _ := item["title"].(string)
		link, _ := item["link"].(string)
		content, _ := item["content"].(string)

		payload := link
		if content != "" {
			payload = content
		}

		sections = append(sections, storage.Section{
			Title:   title,
			Content: payload,
			Order:   i,
		})
		staged = append(staged, fanOutItem{
			Title:    title,
			Content:  payload,
			Source:   firstString(item, "source", "link", "guid"),
			Pipeline: firstString(item, "pipeline"),
		})
	}

	draft.Sections = sections
	draft.Metadata[itemsToEnqueueKey] = []map[string]any{}
	stageItems(draft, staged)
	return draft, nil
}

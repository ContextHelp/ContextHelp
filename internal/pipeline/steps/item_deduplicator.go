package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ItemDeduplicator filters out feed items already seen by checking GUIDs against the FeedItemStore.
type ItemDeduplicator struct {
	pipeline.BaseContract
	store storage.FeedItemStore
}

// NewItemDeduplicator creates an ItemDeduplicator. Pass nil for stub/passthrough mode.
func NewItemDeduplicator(store storage.FeedItemStore) *ItemDeduplicator {
	return &ItemDeduplicator{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Metadata"},
		}),
		store: store,
	}
}

func (s *ItemDeduplicator) Name() string { return "item_deduplicator" }

func (s *ItemDeduplicator) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	rawItems, ok := draft.Metadata["feed_items"]
	if !ok {
		draft.Metadata["feed_items_new"] = []map[string]any{}
		return draft, nil
	}

	items, ok := rawItems.([]map[string]any)
	if !ok {
		return nil, fmt.Errorf("item_deduplicator: feed_items has unexpected type %T", rawItems)
	}

	// In stub mode (no store), pass all items through.
	if s.store == nil {
		draft.Metadata["feed_items_new"] = items
		return draft, nil
	}

	feedID, _ := draft.Metadata["feed_id"].(string)

	newItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		guid, _ := item["guid"].(string)
		if guid == "" {
			// Items without a GUID are treated as new.
			newItems = append(newItems, item)
			continue
		}
		exists, err := s.store.ExistsByGUID(ctx, feedID, guid)
		if err != nil {
			return nil, fmt.Errorf("item_deduplicator: check guid %q: %w", guid, err)
		}
		if !exists {
			newItems = append(newItems, item)
		}
	}

	draft.Metadata["feed_items_new"] = newItems
	return draft, nil
}

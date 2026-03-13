package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestItemDeduplicatorStubPassthrough(t *testing.T) {
	items := []map[string]any{
		{"guid": "g1", "title": "Item 1"},
		{"guid": "g2", "title": "Item 2"},
	}
	step := NewItemDeduplicator(nil)
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"feed_items": items},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	newItems, ok := got.Metadata["feed_items_new"].([]map[string]any)
	if !ok {
		t.Fatalf("feed_items_new missing or wrong type")
	}
	if len(newItems) != 2 {
		t.Errorf("expected 2 items, got %d", len(newItems))
	}
}

func TestItemDeduplicatorNoFeedItems(t *testing.T) {
	step := NewItemDeduplicator(nil)
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	newItems, ok := got.Metadata["feed_items_new"].([]map[string]any)
	if !ok {
		t.Fatalf("feed_items_new missing or wrong type")
	}
	if len(newItems) != 0 {
		t.Errorf("expected 0 items, got %d", len(newItems))
	}
}

// mockFeedItemStore implements storage.FeedItemStore for testing.
type mockFeedItemStore struct {
	existing map[string]bool // key: "feedID:guid"
}

func (m *mockFeedItemStore) Create(_ context.Context, item *storage.FeedItem) error {
	key := item.FeedID + ":" + item.GUID
	m.existing[key] = true
	return nil
}

func (m *mockFeedItemStore) ExistsByGUID(_ context.Context, feedID, guid string) (bool, error) {
	return m.existing[feedID+":"+guid], nil
}

func TestItemDeduplicatorFiltersExisting(t *testing.T) {
	store := &mockFeedItemStore{
		existing: map[string]bool{"feed-1:g1": true},
	}
	items := []map[string]any{
		{"guid": "g1", "title": "Already seen"},
		{"guid": "g2", "title": "New item"},
	}
	step := NewItemDeduplicator(store)
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"feed_items": items,
			"feed_id":    "feed-1",
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	newItems, ok := got.Metadata["feed_items_new"].([]map[string]any)
	if !ok {
		t.Fatalf("feed_items_new wrong type")
	}
	if len(newItems) != 1 {
		t.Errorf("expected 1 new item, got %d", len(newItems))
	}
	if newItems[0]["guid"] != "g2" {
		t.Errorf("expected g2, got %v", newItems[0]["guid"])
	}
}

package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestItemEnqueuerCreatesSection(t *testing.T) {
	items := []map[string]any{
		{"guid": "g1", "title": "First", "link": "https://example.com/1", "content": "Body one"},
		{"guid": "g2", "title": "Second", "link": "https://example.com/2", "content": ""},
	}
	step := NewItemEnqueuer()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"feed_items_new": items},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(got.Sections))
	}
	if got.Sections[0].Title != "First" {
		t.Errorf("section[0].Title: got %q", got.Sections[0].Title)
	}
	pending, ok := got.Metadata["items_to_enqueue"].([]map[string]any)
	if !ok || len(pending) != 2 {
		t.Errorf("items_to_enqueue: got %T len=%d", got.Metadata["items_to_enqueue"], len(pending))
	}
}

func TestItemEnqueuerNoItems(t *testing.T) {
	step := NewItemEnqueuer()
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	pending, ok := got.Metadata["items_to_enqueue"].([]map[string]any)
	if !ok || len(pending) != 0 {
		t.Errorf("expected empty items_to_enqueue")
	}
}

func TestItemEnqueuerLinkFallback(t *testing.T) {
	items := []map[string]any{
		{"guid": "g1", "title": "T", "link": "https://example.com/1", "content": ""},
	}
	step := NewItemEnqueuer()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"feed_items_new": items},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Sections[0].Content != "https://example.com/1" {
		t.Errorf("expected link as content fallback, got %q", got.Sections[0].Content)
	}
}

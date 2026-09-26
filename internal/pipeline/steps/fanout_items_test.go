package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func stagedItems(t *testing.T, d *storage.KnowledgeObject) []map[string]any {
	t.Helper()
	items, ok := d.Metadata["items_to_enqueue"].([]map[string]any)
	if !ok {
		t.Fatalf("items_to_enqueue: %T", d.Metadata["items_to_enqueue"])
	}
	return items
}

// An item with text runs the text pipeline under its own source; a
// link-only item runs the URL pipeline on its link; an item naming its
// pipeline keeps it. Each is named on the container graph.
func TestItemEnqueuerStagesItemJobs(t *testing.T) {
	draft := &storage.KnowledgeObject{ID: "feed-1", Metadata: map[string]any{"feed_items_new": []map[string]any{
		{"title": "Text", "link": "https://example.com/1", "content": "A numbat sighting."},
		{"title": "Link", "link": "https://example.com/2"},
		{"title": "Own", "content": "body", "source": "src:3", "pipeline": "text.short"},
	}}}
	got, err := NewItemEnqueuer().Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]string{
		{"content": "A numbat sighting.", "source": "https://example.com/1", "pipeline": "text.long"},
		{"content": "https://example.com/2", "source": "https://example.com/2", "pipeline": "url.generic"},
		{"content": "body", "source": "src:3", "pipeline": "text.short"},
	}
	items := stagedItems(t, got)
	if len(items) != len(want) {
		t.Fatalf("%d items, want %d", len(items), len(want))
	}
	for i, w := range want {
		for k, v := range w {
			if items[i][k] != v {
				t.Errorf("item %d %s = %v, want %q", i, k, items[i][k], v)
			}
		}
	}

	contains := 0
	for _, e := range got.Graph.Edges {
		if e.EdgeType == pluginapi.EdgeTypeContains && e.FromID == "feed-1" {
			contains++
		}
	}
	if contains != len(want) {
		t.Errorf("%d contains edges, want %d", contains, len(want))
	}
}

func TestBatchEnqueuerStagesCSVRows(t *testing.T) {
	draft := &storage.KnowledgeObject{Metadata: map[string]any{"csv_rows": []map[string]string{
		{"content": "A pademelon grazed at dusk.", "source": "survey:3"},
		{"content": "", "source": ""},
		{"title": "Bettong", "note": "dug for truffles"},
	}}}
	got, err := NewBatchEnqueuer().Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	items := stagedItems(t, got)
	if len(items) != 2 {
		t.Fatalf("%d items, want 2 (the empty row skipped)", len(items))
	}
	if items[0]["content"] != "A pademelon grazed at dusk." || items[0]["source"] != "survey:3" {
		t.Errorf("item 0 = %v", items[0])
	}
	if items[1]["content"] != "note: dug for truffles\ntitle: Bettong" {
		t.Errorf("item 1 content = %q, want its key: value lines", items[1]["content"])
	}
	if got.Metadata["batch_queued"] != 2 {
		t.Errorf("batch_queued = %v, want 2", got.Metadata["batch_queued"])
	}
}

func TestCSVParserReadsTSVSourcesTabSeparated(t *testing.T) {
	draft := &storage.KnowledgeObject{Source: "records.tsv", RawContent: "content\tsource\nA possum, at night.\tsurvey:5\n"}
	got, err := NewCSVParser().Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	rows := got.Metadata["csv_rows"].([]map[string]string)
	if len(rows) != 1 || rows[0]["content"] != "A possum, at night." || rows[0]["source"] != "survey:5" {
		t.Errorf("rows = %v", rows)
	}
}

// A kept message is staged on its route's pipeline under its Message-Id; a
// dropped one is not staged.
func TestEmailEnqueuerStagesKeptMessages(t *testing.T) {
	draft := &storage.KnowledgeObject{Metadata: map[string]any{
		"email_messages": []map[string]any{
			{"subject": "Kept", "text_body": "The quoll raided the bait station.", "message_id": "<m1@example.com>"},
			{"subject": "Dropped", "text_body": "spam", "message_id": "<m2@example.com>"},
		},
		"email_routes": []map[string]any{
			{"message_index": 0, "pipeline": "text.long"},
			{"message_index": 1, "dropped": true},
		},
	}}
	got, err := NewEmailEnqueuer().Run(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	items := stagedItems(t, got)
	if len(items) != 1 {
		t.Fatalf("%d items, want 1", len(items))
	}
	if items[0]["source"] != "email:m1@example.com" || items[0]["pipeline"] != "text.long" ||
		items[0]["content"] != "The quoll raided the bait station." {
		t.Errorf("item = %v", items[0])
	}
}

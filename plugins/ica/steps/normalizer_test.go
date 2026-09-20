package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestICANormalizerNoFeedItems(t *testing.T) {
	step := NewICANormalizer(false)
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"some_key": "val"},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["ica_items_processed"] != nil {
		t.Error("expected no ica_items_processed key")
	}
}

func TestICANormalizerNilMetadata(t *testing.T) {
	step := NewICANormalizer(false)
	draft := &storage.KnowledgeObject{}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata != nil {
		t.Error("expected nil metadata unchanged")
	}
}

func TestICANormalizerSingleItem(t *testing.T) {
	step := NewICANormalizer(true)
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"feed_items": []any{
				map[string]any{
					"id":                  "item-1",
					"title":               "Test Article",
					"canonical_url":       "https://example.com/1",
					"article_body":        "body text",
					"content_hash":        "abc123",
					"media_types_present": []any{"text", "image"},
					"distribution_channels": []any{
						map[string]any{
							"domain": "twitter.com",
							"url":    "https://twitter.com/post/1",
						},
					},
					"extracted_entities": []any{
						map[string]any{
							"type":  "person",
							"value": "Jane Doe",
						},
					},
					"structured_analysis": map[string]any{
						"chapters": []any{
							map[string]any{
								"id":                "ch-1",
								"phase":             "intro",
								"voiceover_summary": "opening",
								"timestamp_range":   "0:00-1:00",
							},
						},
					},
					"embedding": []any{0.1, 0.2, 0.3},
				},
			},
		},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if got.ContentHash != "abc123" {
		t.Errorf("content_hash: got %s", got.ContentHash)
	}

	if got.Metadata["ica_items_processed"] != 1 {
		t.Errorf(
			"ica_items_processed: got %v",
			got.Metadata["ica_items_processed"],
		)
	}

	// tags: media:text + media:image
	if len(got.Tags) < 2 {
		t.Fatalf("tags: got %d, want >= 2", len(got.Tags))
	}

	// sections from chapters
	if len(got.Sections) != 1 {
		t.Fatalf(
			"sections: got %d, want 1", len(got.Sections),
		)
	}
	if got.Sections[0].Title != "intro" {
		t.Errorf("section title: got %s", got.Sections[0].Title)
	}

	// mentions: dist channel + entity (distChannelsAsMentions=true)
	if len(got.Mentions) < 2 {
		t.Fatalf(
			"mentions: got %d, want >= 2", len(got.Mentions),
		)
	}

	// embeddings
	if len(got.Embeddings) != 3 {
		t.Errorf(
			"embeddings: got %d, want 3",
			len(got.Embeddings),
		)
	}

	// metadata merged
	if got.Metadata["title"] != "Test Article" {
		t.Errorf("title: got %v", got.Metadata["title"])
	}
}

func TestICANormalizerMultipleItems(t *testing.T) {
	step := NewICANormalizer(false)
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"feed_items": []any{
				map[string]any{
					"id":                  "a",
					"content_hash":        "h1",
					"media_types_present": []any{"text"},
				},
				map[string]any{
					"id":                  "b",
					"content_hash":        "h2",
					"media_types_present": []any{"audio"},
					"structured_analysis": map[string]any{
						"chapters": []any{
							map[string]any{
								"phase":             "ch1",
								"voiceover_summary": "s1",
							},
						},
					},
				},
			},
		},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if got.Metadata["ica_items_processed"] != 2 {
		t.Errorf(
			"processed: got %v",
			got.Metadata["ica_items_processed"],
		)
	}

	// accumulated tags: media:text + media:audio
	if len(got.Tags) != 2 {
		t.Errorf("tags: got %d, want 2", len(got.Tags))
	}

	// sections from second item only
	if len(got.Sections) != 1 {
		t.Errorf(
			"sections: got %d, want 1", len(got.Sections),
		)
	}

	// last item's hash wins
	if got.ContentHash != "h2" {
		t.Errorf("hash: got %s, want h2", got.ContentHash)
	}
}

func TestICANormalizerDistChannelsFalse(t *testing.T) {
	step := NewICANormalizer(false)
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"feed_items": []any{
				map[string]any{
					"id": "x",
					"distribution_channels": []any{
						map[string]any{
							"domain": "reddit.com",
							"url":    "https://reddit.com/r/test",
						},
					},
					"extracted_entities": []any{
						map[string]any{
							"type":  "org",
							"value": "Acme",
						},
					},
				},
			},
		},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// no mentions when distChannelsAsMentions=false
	if len(got.Mentions) != 0 {
		t.Errorf(
			"mentions: got %d, want 0", len(got.Mentions),
		)
	}
}

func TestICANormalizerDistChannelsTrue(t *testing.T) {
	step := NewICANormalizer(true)
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"feed_items": []any{
				map[string]any{
					"id": "y",
					"distribution_channels": []any{
						map[string]any{
							"domain": "reddit.com",
							"url":    "https://reddit.com/r/test",
						},
					},
					"extracted_entities": []any{
						map[string]any{
							"type":  "org",
							"value": "Acme",
						},
					},
				},
			},
		},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// dist channel + entity = 2 mentions
	if len(got.Mentions) != 2 {
		t.Errorf(
			"mentions: got %d, want 2", len(got.Mentions),
		)
	}
}

func TestICANormalizerContract(t *testing.T) {
	step := NewICANormalizer(false)

	if step.Name() != "ica_normalizer" {
		t.Errorf("name: got %s", step.Name())
	}

	c := step.Contract()
	if len(c.Requires) != 1 || c.Requires[0] != "Metadata" {
		t.Errorf("requires: got %v", c.Requires)
	}
	if len(c.Produces) != 4 {
		t.Errorf("produces: got %v", c.Produces)
	}
	if len(c.Capabilities) != 0 {
		t.Errorf("capabilities: got %v", c.Capabilities)
	}
}

package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestEmailEnqueuerBasic(t *testing.T) {
	msgs := []map[string]any{
		{
			"subject":      "Hello World",
			"from":         "alice@example.com",
			"text_body":    "This is the body text.",
			"message_id":   "msg-001@example.com",
			"content_hash": "abc123",
		},
		{
			"subject":      "Invoice",
			"from":         "billing@stripe.com",
			"text_body":    "Your invoice is ready.",
			"message_id":   "msg-002@stripe.com",
			"content_hash": "def456",
		},
	}
	routes := []map[string]any{
		{
			"message_index": 0,
			"rule_id":       "fallback",
			"matched":       true,
			"dropped":       false,
			"pipeline":      "text.long",
			"set_subtype":   "email",
			"set_tags":      []string{},
		},
		{
			"message_index": 1,
			"rule_id":       "billing",
			"matched":       true,
			"dropped":       false,
			"pipeline":      "email.billing",
			"set_subtype":   "billing",
			"set_tags":      []string{"billing"},
		},
	}

	step := NewEmailEnqueuer()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"email_messages": msgs,
			"email_routes":   routes,
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if got.Metadata["email_queued"] != 2 {
		t.Errorf("email_queued: got %v, want 2", got.Metadata["email_queued"])
	}
	if got.Metadata["email_skipped"] != 0 {
		t.Errorf("email_skipped: got %v", got.Metadata["email_skipped"])
	}
	if len(got.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(got.Sections))
	}
	if got.Sections[0].Title != "Hello World" {
		t.Errorf("section[0].Title: got %q", got.Sections[0].Title)
	}
	if got.Sections[0].Content != "This is the body text." {
		t.Errorf("section[0].Content: got %q", got.Sections[0].Content)
	}
	// Verify pipeline stored in section metadata.
	pipeline, _ := got.Sections[1].Metadata["pipeline"].(string)
	if pipeline != "email.billing" {
		t.Errorf("section[1].Metadata.pipeline: got %q, want email.billing", pipeline)
	}
}

func TestEmailEnqueuerDroppedMessages(t *testing.T) {
	msgs := []map[string]any{
		{
			"subject":      "Keep me",
			"from":         "a@example.com",
			"text_body":    "Keep this.",
			"message_id":   "keep-001",
			"content_hash": "hash1",
		},
		{
			"subject":      "Drop me",
			"from":         "b@example.com",
			"text_body":    "Drop this.",
			"message_id":   "drop-001",
			"content_hash": "hash2",
		},
	}
	routes := []map[string]any{
		{
			"message_index": 0,
			"dropped":       false,
			"pipeline":      "text.long",
		},
		{
			"message_index": 1,
			"dropped":       true,
			"pipeline":      "",
		},
	}

	step := NewEmailEnqueuer()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"email_messages": msgs,
			"email_routes":   routes,
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if got.Metadata["email_queued"] != 1 {
		t.Errorf("email_queued: got %v, want 1", got.Metadata["email_queued"])
	}
	if got.Metadata["email_skipped"] != 1 {
		t.Errorf("email_skipped: got %v, want 1", got.Metadata["email_skipped"])
	}
	if len(got.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(got.Sections))
	}
}

func TestEmailEnqueuerNoRoutes(t *testing.T) {
	// Without routes, all messages should use the default pipeline.
	msgs := []map[string]any{
		{
			"subject":      "Test",
			"from":         "a@example.com",
			"text_body":    "Test body.",
			"message_id":   "test-001",
			"content_hash": "hash1",
		},
	}
	step := NewEmailEnqueuer(WithDefaultEmailPipeline("text.short"))
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"email_messages": msgs},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 1 {
		t.Fatalf("expected 1 section")
	}
	pipeline, _ := got.Sections[0].Metadata["pipeline"].(string)
	if pipeline != "text.short" {
		t.Errorf("pipeline: got %q, want text.short", pipeline)
	}
}

func TestEmailEnqueuerNoMessages(t *testing.T) {
	step := NewEmailEnqueuer()
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["email_queued"] != 0 {
		t.Errorf("email_queued: got %v", got.Metadata["email_queued"])
	}
}

func TestEmailEnqueuerEmptyBodyFallsBackToSubject(t *testing.T) {
	msgs := []map[string]any{
		{
			"subject":      "Only Subject Here",
			"from":         "a@example.com",
			"text_body":    "",
			"html_body":    "",
			"message_id":   "sub-001",
			"content_hash": "hash1",
		},
	}
	step := NewEmailEnqueuer()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{"email_messages": msgs},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 1 {
		t.Fatalf("expected 1 section")
	}
	if got.Sections[0].Content != "Only Subject Here" {
		t.Errorf("content: got %q, expected subject as fallback", got.Sections[0].Content)
	}
}

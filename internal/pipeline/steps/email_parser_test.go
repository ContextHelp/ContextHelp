package steps

import (
	"context"
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestEmailParserSimpleEML(t *testing.T) {
	raw, err := os.ReadFile("testdata/email/simple.eml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	step := NewEmailParser()
	draft := &storage.KnowledgeObject{RawContent: string(raw)}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs, ok := got.Metadata["email_messages"].([]map[string]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %v", got.Metadata["email_messages"])
	}
	msg := msgs[0]
	if msg["subject"] != "Hello World" {
		t.Errorf("subject: got %q", msg["subject"])
	}
	if msg["from"] != "alice@example.com" {
		t.Errorf("from: got %q", msg["from"])
	}
	if msg["from_domain"] != "example.com" {
		t.Errorf("from_domain: got %q", msg["from_domain"])
	}
	if got.Metadata["email_count"] != 1 {
		t.Errorf("email_count: got %v", got.Metadata["email_count"])
	}
	body, _ := msg["text_body"].(string)
	if body == "" {
		t.Error("expected non-empty text_body")
	}
	hash, _ := msg["content_hash"].(string)
	if hash == "" {
		t.Error("expected non-empty content_hash")
	}
}

func TestEmailParserNewsletterEML(t *testing.T) {
	raw, err := os.ReadFile("testdata/email/newsletter.eml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	step := NewEmailParser()
	draft := &storage.KnowledgeObject{RawContent: string(raw)}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs, ok := got.Metadata["email_messages"].([]map[string]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %v", got.Metadata["email_messages"])
	}
	msg := msgs[0]
	if msg["list_id"] == "" {
		t.Error("expected list_id to be set")
	}
	htmlBody, _ := msg["html_body"].(string)
	textBody, _ := msg["text_body"].(string)
	if htmlBody == "" && textBody == "" {
		t.Error("expected at least one body to be set")
	}
}

func TestEmailParserBillingEML(t *testing.T) {
	raw, err := os.ReadFile("testdata/email/billing.eml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	step := NewEmailParser()
	draft := &storage.KnowledgeObject{RawContent: string(raw)}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs, ok := got.Metadata["email_messages"].([]map[string]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %v", got.Metadata["email_messages"])
	}
	msg := msgs[0]
	if msg["from_domain"] != "stripe.com" {
		t.Errorf("from_domain: got %q", msg["from_domain"])
	}
}

func TestEmailParserMbox(t *testing.T) {
	raw, err := os.ReadFile("testdata/email/two_messages.mbox")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	step := NewEmailParser()
	draft := &storage.KnowledgeObject{RawContent: string(raw)}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs, ok := got.Metadata["email_messages"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", got.Metadata["email_messages"])
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages from mbox, got %d", len(msgs))
	}
	subjects := []string{}
	for _, m := range msgs {
		if s, ok := m["subject"].(string); ok {
			subjects = append(subjects, s)
		}
	}
	if len(subjects) != 2 {
		t.Errorf("expected 2 subjects, got %v", subjects)
	}
}

func TestEmailParserEmptyContent(t *testing.T) {
	step := NewEmailParser()
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	msgs, ok := got.Metadata["email_messages"].([]map[string]any)
	if !ok || len(msgs) != 0 {
		t.Errorf("expected empty messages, got %v", got.Metadata["email_messages"])
	}
}

func TestEmailParserInlineEML(t *testing.T) {
	raw := "From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\nMessage-Id: <inline-001@example.com>\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n\r\nBody text here.\r\n"

	step := NewEmailParser()
	draft := &storage.KnowledgeObject{RawContent: raw}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	msgs, ok := got.Metadata["email_messages"].([]map[string]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %v", got.Metadata["email_messages"])
	}
	if msgs[0]["subject"] != "Test" {
		t.Errorf("subject: got %q", msgs[0]["subject"])
	}
}

func TestEmailParserMaxBodyBytes(t *testing.T) {
	// Build a message with a large body.
	body := make([]byte, 5000)
	for i := range body {
		body[i] = 'x'
	}
	raw := "From: a@example.com\r\nTo: b@example.com\r\nSubject: Big\r\nMessage-Id: <big-001@example.com>\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n\r\n" + string(body)

	step := NewEmailParser(WithEmailMaxBodyBytes(100))
	draft := &storage.KnowledgeObject{RawContent: raw}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	msgs, _ := got.Metadata["email_messages"].([]map[string]any)
	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
	textBody, _ := msgs[0]["text_body"].(string)
	if len(textBody) > 100 {
		t.Errorf("body not truncated: len=%d", len(textBody))
	}
}

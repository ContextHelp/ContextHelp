package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestDetectText(t *testing.T) {
	step := NewTypeDetector()
	draft := &storage.KnowledgeObject{RawContent: "some plain text"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Type != "text" {
		t.Errorf("Type: got %q, want text", got.Type)
	}
}

func TestDetectURL(t *testing.T) {
	step := NewTypeDetector()
	draft := &storage.KnowledgeObject{RawContent: "https://example.com/page"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Type != "url" {
		t.Errorf("Type: got %q, want url", got.Type)
	}
}

func TestDetectShortText(t *testing.T) {
	step := NewTypeDetector()
	draft := &storage.KnowledgeObject{RawContent: "short text"}
	got, _ := step.Run(context.Background(), draft)
	if got.Subtype != "short" {
		t.Errorf("Subtype: got %q, want short", got.Subtype)
	}
}

func TestDetectLongText(t *testing.T) {
	step := NewTypeDetector()
	draft := &storage.KnowledgeObject{RawContent: strings.Repeat("word ", 200)}
	got, _ := step.Run(context.Background(), draft)
	if got.Subtype != "long" {
		t.Errorf("Subtype: got %q, want long", got.Subtype)
	}
}

package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestTextCleanerNormalizesSpaces(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "hello    world\t\ttabs"}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "hello world tabs" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerNormalizesNewlines(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "a\n\n\n\nb"}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "a\n\nb" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerTrimsWhitespace(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "  hello  "}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "hello" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerCRLF(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "line1\r\nline2"}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "line1\nline2" {
		t.Errorf("got %q", got.RawContent)
	}
}

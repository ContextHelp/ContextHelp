package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestNoopPassthrough(t *testing.T) {
	step := NewNoop()
	draft := &storage.KnowledgeObject{
		ID:         "obj-1",
		RawContent: "hello",
		Type:       "text",
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.ID != "obj-1" {
		t.Errorf("ID changed: got %q", got.ID)
	}
	if got.RawContent != "hello" {
		t.Errorf("RawContent changed: got %q", got.RawContent)
	}
	if got.Type != "text" {
		t.Errorf("Type changed: got %q", got.Type)
	}
}

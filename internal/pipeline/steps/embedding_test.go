package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestEmbeddingGeneratorStubSkipsIndexing(t *testing.T) {
	// Stub provider returns nil vector — VectorIndexed stays false.
	step := NewEmbeddingGenerator(nil)
	draft := &storage.KnowledgeObject{RawContent: "some text"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.VectorIndexed {
		t.Error("VectorIndexed should be false when stub provider returns no vector")
	}
}

func TestEmbeddingGeneratorEmptyContent(t *testing.T) {
	step := NewEmbeddingGenerator(nil)
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.VectorIndexed {
		t.Error("VectorIndexed should be false when content is empty")
	}
}

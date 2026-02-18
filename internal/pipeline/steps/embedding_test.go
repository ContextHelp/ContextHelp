package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestEmbeddingGeneratorSetsFlag(t *testing.T) {
	step := NewEmbeddingGenerator()
	draft := &storage.KnowledgeObject{RawContent: "some text"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !got.VectorIndexed {
		t.Error("VectorIndexed should be true")
	}
}

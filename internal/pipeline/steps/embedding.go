package steps

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type EmbeddingGenerator struct {
	pipeline.BaseContract
}

func NewEmbeddingGenerator() *EmbeddingGenerator {
	return &EmbeddingGenerator{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Embeddings", "VectorIndexed"},
		}),
	}
}

func (s *EmbeddingGenerator) Name() string { return "embedding_generator" }

func (s *EmbeddingGenerator) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// Stub: mark as ready for vector indexing. Real implementation
	// will call an embedding provider (OpenAI, local model, etc.).
	draft.VectorIndexed = true
	return draft, nil
}

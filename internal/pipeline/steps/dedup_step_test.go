package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubObjectStoreForDedup struct {
	storage.ObjectStore
	similar []*storage.KnowledgeObject
}

func (s *stubObjectStoreForDedup) VectorSearch(_ context.Context, _ []float32, _ storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	return s.similar, nil
}

func TestDedupStep_NoDuplicatePassesThrough(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "warn"}
	step := NewDedupStep(&stubObjectStoreForDedup{similar: nil}, cfg)

	draft := &storage.KnowledgeObject{
		ID:         "new-1",
		Embeddings: []float32{1, 0, 0},
		Metadata:   map[string]any{},
	}
	out, err := step.Run(context.Background(), draft)
	require.NoError(t, err)
	assert.Equal(t, "new-1", out.ID)
	assert.Nil(t, out.Metadata["duplicate_of"])
}

func TestDedupStep_SimilarFoundWarnPolicy(t *testing.T) {
	existing := &storage.KnowledgeObject{
		ID:         "existing-2",
		Embeddings: []float32{1, 0, 0},
		Metadata:   map[string]any{"score": float64(0.97)},
	}
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "warn"}
	step := NewDedupStep(&stubObjectStoreForDedup{similar: []*storage.KnowledgeObject{existing}}, cfg)

	draft := &storage.KnowledgeObject{
		ID:         "new-2",
		Embeddings: []float32{1, 0, 0},
		Metadata:   map[string]any{},
	}
	out, err := step.Run(context.Background(), draft)
	require.NoError(t, err)
	// warn policy: continues, sets duplicate_of metadata for audit.
	assert.Equal(t, "existing-2", out.Metadata["duplicate_of"])
}

func TestDedupStep_SimilarFoundDropPolicy(t *testing.T) {
	existing := &storage.KnowledgeObject{
		ID:         "existing-3",
		Embeddings: []float32{1, 0, 0},
		Metadata:   map[string]any{"score": float64(0.99)},
	}
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "drop"}
	step := NewDedupStep(&stubObjectStoreForDedup{similar: []*storage.KnowledgeObject{existing}}, cfg)

	draft := &storage.KnowledgeObject{
		ID:         "new-3",
		Embeddings: []float32{1, 0, 0},
		Metadata:   map[string]any{},
	}
	out, err := step.Run(context.Background(), draft)
	require.NoError(t, err)
	// drop policy: sets duplicate_of and signals pipeline to suppress via suppress_output flag.
	assert.Equal(t, "existing-3", out.Metadata["duplicate_of"])
	assert.Equal(t, true, out.Metadata["suppress_output"])
}

func TestDedupStep_CheckSimilarDisabled(t *testing.T) {
	existing := &storage.KnowledgeObject{ID: "existing-4", Metadata: map[string]any{"score": float64(0.99)}}
	cfg := config.DuplicatesConfig{CheckSimilar: false, SimilarityThreshold: 0.95, Policy: "drop"}
	step := NewDedupStep(&stubObjectStoreForDedup{similar: []*storage.KnowledgeObject{existing}}, cfg)

	draft := &storage.KnowledgeObject{
		ID:         "new-4",
		Embeddings: []float32{1, 0, 0},
		Metadata:   map[string]any{},
	}
	out, err := step.Run(context.Background(), draft)
	require.NoError(t, err)
	assert.Nil(t, out.Metadata["duplicate_of"])
}

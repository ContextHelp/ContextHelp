package integration

// US-0051: Semantic Search with Embeddings
// Ingests objects with pre-set embedding vectors, queries via VectorSearch,
// verifies semantically similar objects are ranked higher than unrelated ones.

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeEmbedding returns a unit vector of the given dimension with v[0] set to
// cosineComponent so similarity comparisons are predictable.
func makeEmbedding(dim int, cosineComponent float32) []float32 {
	v := make([]float32, dim)
	v[0] = cosineComponent
	// Remaining dimensions left at 0; magnitude = |cosineComponent|.
	return v
}

// TestUS0051_VectorSearchRetrieves semantically similar objects.
// Uses pre-computed embeddings stored directly on objects; bypasses the
// embedding provider by calling VectorSearch on the store directly.
func TestUS0051_VectorSearchReturnsSimilarObjects(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()
	const dim = 4

	// Object with embedding similar to the query vector.
	similar := &storage.KnowledgeObject{
		ID:            "sem-sim-01",
		Type:          "text",
		Summaries:     []string{"neural network deep learning transformer"},
		Embeddings:    makeEmbedding(dim, 0.99),
		VectorIndexed: true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	// Object with orthogonal embedding (dissimilar).
	dissimilar := &storage.KnowledgeObject{
		ID:            "sem-dis-01",
		Type:          "text",
		Summaries:     []string{"relational database normalisation forms"},
		Embeddings:    makeEmbedding(dim, 0.01),
		VectorIndexed: true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	require.NoError(t, env.svc.Store.Objects().Create(ctx, similar))
	require.NoError(t, env.svc.Store.Objects().Create(ctx, dissimilar))

	// Query vector closely aligned with "similar" object.
	queryVec := makeEmbedding(dim, 1.0)
	results, err := env.svc.Store.Objects().VectorSearch(ctx, queryVec, storage.ObjectFilter{Limit: 5})
	require.NoError(t, err)
	require.NotEmpty(t, results, "vector search must return results when embeddings are present")

	// The most similar object must rank first.
	assert.Equal(t, "sem-sim-01", results[0].ID,
		"object with embedding closest to query vector must rank first")
}

// TestUS0051_VectorSearchSkipsObjectsWithoutEmbeddings verifies that objects
// without embeddings do not appear in vector search results.
func TestUS0051_VectorSearchSkipsObjectsWithoutEmbeddings(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()
	const dim = 4

	withEmbedding := &storage.KnowledgeObject{
		ID:            "sem-has-01",
		Type:          "text",
		Embeddings:    makeEmbedding(dim, 1.0),
		VectorIndexed: true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	withoutEmbedding := &storage.KnowledgeObject{
		ID:        "sem-none-01",
		Type:      "text",
		CreatedAt: now,
		UpdatedAt: now,
	}

	require.NoError(t, env.svc.Store.Objects().Create(ctx, withEmbedding))
	require.NoError(t, env.svc.Store.Objects().Create(ctx, withoutEmbedding))

	queryVec := makeEmbedding(dim, 1.0)
	results, err := env.svc.Store.Objects().VectorSearch(ctx, queryVec, storage.ObjectFilter{Limit: 5})
	require.NoError(t, err)

	assert.True(t, containsIDPtr(results, "sem-has-01"), "object with embedding must appear")
	assert.False(t, containsIDPtr(results, "sem-none-01"), "object without embedding must be excluded")
}

// TestUS0051_VectorSearchLimitHonoured verifies that the Limit field in
// ObjectFilter caps the number of returned results.
func TestUS0051_VectorSearchLimitHonoured(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()
	const dim = 4

	for i := 0; i < 5; i++ {
		id := "sem-lim-" + string(rune('a'+i))
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:            id,
			Type:          "text",
			Embeddings:    makeEmbedding(dim, float32(0.5+float64(i)*0.1)),
			VectorIndexed: true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}))
	}

	queryVec := makeEmbedding(dim, 1.0)
	results, err := env.svc.Store.Objects().VectorSearch(ctx, queryVec, storage.ObjectFilter{Limit: 3})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 3, "result count must not exceed requested limit")
}

package integration

// US-0061: Visual Similarity Search
// Ingests image objects with pre-set visual embedding vectors, queries by
// image embedding, verifies visually similar images are ranked higher than
// dissimilar ones.
//
// Visual embeddings use the same storage mechanism as text embeddings (rows
// under a registered model in the per-model embedding store) with
// type="image". The VectorSearch store method is used directly since a full
// VLM embedding provider is not available in the integration test
// environment.

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeVisualEmbedding returns a unit-length embedding vector of the given
// dimension with the specified component values. Non-specified components are 0.
func makeVisualEmbedding(dim int, components map[int]float32) []float32 {
	v := make([]float32, dim)
	for idx, val := range components {
		if idx < dim {
			v[idx] = val
		}
	}
	return v
}

// TestUS0061_VisuallySimialrImageRanksHigher verifies that an image object
// whose visual embedding is closely aligned to the query vector ranks above
// an image with a dissimilar embedding.
func TestUS0061_VisuallySimialrImageRanksHigher(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()
	const dim = 8
	requireVectorIndex(t, env, dim)

	// Image closely matching the query vector.
	similar := &storage.KnowledgeObject{
		ID:        "vis-sim-01",
		Type:      "image",
		Subtype:   "jpeg",
		Summaries: []string{"authentication flow architecture diagram"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Image with orthogonal embedding.
	dissimilar := &storage.KnowledgeObject{
		ID:        "vis-dis-01",
		Type:      "image",
		Subtype:   "png",
		Summaries: []string{"database schema entity relationship diagram"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	createWithVector(t, env, similar, makeVisualEmbedding(dim, map[int]float32{0: 0.98, 1: 0.1}))
	createWithVector(t, env, dissimilar, makeVisualEmbedding(dim, map[int]float32{3: 0.95, 4: 0.2}))

	// Query vector aligned with "similar" image.
	queryVec := makeVisualEmbedding(dim, map[int]float32{0: 1.0})
	results, err := env.svc.Store.Objects().VectorSearch(ctx, vq(queryVec), storage.ObjectFilter{Limit: 5})
	require.NoError(t, err)
	require.NotEmpty(t, results, "vector search must return results for image embeddings")
	assert.Equal(t, "vis-sim-01", results[0].ID,
		"visually similar image must rank first")
}

// TestUS0061_DualEmbeddingImageAndTextCoexist verifies that an image object
// can store visual embeddings alongside text summaries and be found via both
// RSQL (type filter) and vector search.
func TestUS0061_DualEmbeddingImageAndTextCoexist(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()
	const dim = 4
	requireVectorIndex(t, env, dim)

	imgObj := &storage.KnowledgeObject{
		ID:         "vis-dual-01",
		Type:       "image",
		Subtype:    "png",
		Summaries:  []string{"whiteboard photo of microservices topology"},
		RawContent: "microservices topology",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	createWithVector(t, env, imgObj, makeVisualEmbedding(dim, map[int]float32{0: 0.9, 1: 0.3}))
	rebuildFTSIntegration(t, env)

	// RSQL path must find it.
	objs, total, err := env.svc.SearchObjects(ctx, "type==image", 20, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.True(t, containsIDPtr(objs, "vis-dual-01"), "image must be findable via RSQL")

	// Vector search path must also find it.
	queryVec := makeVisualEmbedding(dim, map[int]float32{0: 1.0})
	vecResults, err := env.svc.Store.Objects().VectorSearch(ctx, vq(queryVec), storage.ObjectFilter{Limit: 5})
	require.NoError(t, err)
	assert.True(t, containsIDPtr(vecResults, "vis-dual-01"), "image must be findable via vector search")
}

// TestUS0061_ImageTypeFilterRestrictsVectorResults verifies that after
// restricting to type==image, only image objects appear (not text objects that
// also have embeddings).
func TestUS0061_ImageTypeFilterRestrictsVectorResults(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()
	const dim = 4
	requireVectorIndex(t, env, dim)

	imgObj := &storage.KnowledgeObject{
		ID:        "vis-img-only-01",
		Type:      "image",
		CreatedAt: now,
		UpdatedAt: now,
	}
	textObj := &storage.KnowledgeObject{
		ID:        "vis-text-01",
		Type:      "text",
		CreatedAt: now,
		UpdatedAt: now,
	}

	createWithVector(t, env, imgObj, makeVisualEmbedding(dim, map[int]float32{0: 0.9}))
	createWithVector(t, env, textObj, makeVisualEmbedding(dim, map[int]float32{0: 0.85}))

	// Vector search filtered to type==image.
	queryVec := makeVisualEmbedding(dim, map[int]float32{0: 1.0})
	results, err := env.svc.Store.Objects().VectorSearch(ctx, vq(queryVec), storage.ObjectFilter{
		Limit: 10,
		Type:  "image",
	})
	require.NoError(t, err)
	for _, r := range results {
		assert.Equal(t, "image", r.Type, "type-filtered vector search must only return image objects")
	}
}

// TestUS0061_NoEmbeddingImageExcludedFromVectorSearch verifies that image
// objects without embeddings do not appear in vector search results.
func TestUS0061_NoEmbeddingImageExcludedFromVectorSearch(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()
	const dim = 4
	requireVectorIndex(t, env, dim)

	withEmb := &storage.KnowledgeObject{
		ID:        "vis-emb-yes",
		Type:      "image",
		CreatedAt: now,
		UpdatedAt: now,
	}
	noEmb := &storage.KnowledgeObject{
		ID:        "vis-emb-no",
		Type:      "image",
		CreatedAt: now,
		UpdatedAt: now,
	}

	createWithVector(t, env, withEmb, makeVisualEmbedding(dim, map[int]float32{0: 1.0}))
	require.NoError(t, env.svc.Store.Objects().Create(ctx, noEmb))

	queryVec := makeVisualEmbedding(dim, map[int]float32{0: 1.0})
	results, err := env.svc.Store.Objects().VectorSearch(ctx, vq(queryVec), storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	assert.True(t, containsIDPtr(results, "vis-emb-yes"), "image with embedding must appear")
	assert.False(t, containsIDPtr(results, "vis-emb-no"), "image without embedding must be excluded")
}

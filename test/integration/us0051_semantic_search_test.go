package integration

// US-0051: Semantic Search with Embeddings
// Ingests objects with pre-set vectors under a registered model, queries via
// VectorSearch on that model, verifies semantically similar objects are
// ranked higher than unrelated ones. Runs once the driver's per-model
// EmbeddingStore is implemented; skips until then.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
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

// vectorModelID names the registered model integration vectors of length
// dim are indexed and queried under.
func vectorModelID(dim int) string { return fmt.Sprintf("it-vectors-%d", dim) }

// requireVectorIndex registers the dim-length model on env's store and
// builds its index. It skips the test while the driver's EmbeddingStore is
// not implemented (errors.ErrUnsupported), so these end-to-end checks run
// as soon as the per-model index lands.
func requireVectorIndex(t *testing.T, env *testEnv, dim int) {
	t.Helper()
	ctx := context.Background()
	reg, err := registry.ForDriver(env.svc.Store)
	require.NoError(t, err)
	m := registry.Model{ModelID: vectorModelID(dim), Provider: "fixture", Dimension: dim, ConfigJSON: "{}"}
	if err := reg.Register(ctx, m, false); err != nil && !errors.Is(err, registry.ErrModelAlreadyRegistered) {
		t.Fatalf("register %s: %v", m.ModelID, err)
	}
	err = env.svc.Store.Embeddings().EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: m.ModelID, Provider: m.Provider, Dimension: dim})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip("EmbeddingStore not implemented by the driver yet (per-model index)")
	}
	require.NoError(t, err)
}

// createWithVector creates obj and stores vec as its single chunk under the
// model for len(vec).
func createWithVector(t *testing.T, env *testEnv, obj *storage.KnowledgeObject, vec []float32) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	require.NoError(t, env.svc.Store.Embeddings().Put(ctx, obj.ID, []storage.ObjectVector{
		{ModelID: vectorModelID(len(vec)), Vector: vec},
	}))
}

// vq queries the model for len(vec).
func vq(vec []float32) storage.VectorQuery {
	return storage.VectorQuery{ModelID: vectorModelID(len(vec)), Vector: vec}
}

// TestUS0051_VectorSearchRetrieves semantically similar objects.
// Uses pre-computed vectors stored through EmbeddingStore.Put; bypasses the
// embedding provider by calling VectorSearch on the store directly.
func TestUS0051_VectorSearchReturnsSimilarObjects(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()
	const dim = 4
	requireVectorIndex(t, env, dim)

	// Object with embedding similar to the query vector.
	similar := &storage.KnowledgeObject{
		ID:        "sem-sim-01",
		Type:      "text",
		Summaries: []string{"neural network deep learning transformer"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Object with orthogonal embedding (dissimilar).
	dissimilar := &storage.KnowledgeObject{
		ID:        "sem-dis-01",
		Type:      "text",
		Summaries: []string{"relational database normalisation forms"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	createWithVector(t, env, similar, makeEmbedding(dim, 0.99))
	createWithVector(t, env, dissimilar, []float32{0.01, 1, 0, 0}) // nearly orthogonal

	// Query vector closely aligned with "similar" object.
	queryVec := makeEmbedding(dim, 1.0)
	results, err := env.svc.Store.Objects().VectorSearch(ctx, vq(queryVec), storage.ObjectFilter{Limit: 5})
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
	requireVectorIndex(t, env, dim)

	withEmbedding := &storage.KnowledgeObject{
		ID:        "sem-has-01",
		Type:      "text",
		CreatedAt: now,
		UpdatedAt: now,
	}
	withoutEmbedding := &storage.KnowledgeObject{
		ID:        "sem-none-01",
		Type:      "text",
		CreatedAt: now,
		UpdatedAt: now,
	}

	createWithVector(t, env, withEmbedding, makeEmbedding(dim, 1.0))
	require.NoError(t, env.svc.Store.Objects().Create(ctx, withoutEmbedding))

	queryVec := makeEmbedding(dim, 1.0)
	results, err := env.svc.Store.Objects().VectorSearch(ctx, vq(queryVec), storage.ObjectFilter{Limit: 5})
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
	requireVectorIndex(t, env, dim)

	for i := 0; i < 5; i++ {
		id := "sem-lim-" + string(rune('a'+i))
		createWithVector(t, env, &storage.KnowledgeObject{
			ID:        id,
			Type:      "text",
			CreatedAt: now,
			UpdatedAt: now,
		}, makeEmbedding(dim, float32(0.5+float64(i)*0.1)))
	}

	queryVec := makeEmbedding(dim, 1.0)
	results, err := env.svc.Store.Objects().VectorSearch(ctx, vq(queryVec), storage.ObjectFilter{Limit: 3})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 3, "result count must not exceed requested limit")
}

package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dedupDefault   = "dedup-default@1"
	dedupCandidate = "dedup-candidate@1"
)

var dedupModels = embeddingtest.Models{
	{ModelID: dedupDefault, Provider: "ollama", Dimension: 3, IsDefault: true},
	{ModelID: dedupCandidate, Provider: "ollama", Dimension: 2},
}

// dedupStore indexes both models and stores existing objects' vectors.
func dedupStore(t *testing.T, existing map[string][]pluginapi.ObjectVector) *embeddingtest.MemStore {
	t.Helper()
	ctx := context.Background()
	s := embeddingtest.NewMemStore()
	for _, m := range dedupModels {
		require.NoError(t, s.EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: m.ModelID, Provider: m.Provider, Dimension: m.Dimension}))
	}
	for id, vs := range existing {
		require.NoError(t, s.Put(ctx, id, vs))
	}
	return s
}

func vec(model string, v ...float32) pluginapi.ObjectVector {
	return pluginapi.ObjectVector{ModelID: model, Vector: v}
}

func dedupDraft(id string, vs ...pluginapi.ObjectVector) *storage.KnowledgeObject {
	return &storage.KnowledgeObject{ID: id, Vectors: vs, Metadata: map[string]any{}}
}

func TestDedupStep_NoDuplicatePassesThrough(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "warn"}
	store := dedupStore(t, map[string][]pluginapi.ObjectVector{"other": {vec(dedupDefault, 0, 1, 0)}})
	out, err := NewDedupStep(dedupModels, store, cfg).Run(context.Background(), dedupDraft("new-1", vec(dedupDefault, 1, 0, 0)))
	require.NoError(t, err)
	assert.Nil(t, out.Metadata["duplicate_of"])
}

func TestDedupStep_SimilarFoundWarnPolicy(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "warn"}
	store := dedupStore(t, map[string][]pluginapi.ObjectVector{"existing-2": {vec(dedupDefault, 1, 0, 0)}})
	out, err := NewDedupStep(dedupModels, store, cfg).Run(context.Background(), dedupDraft("new-2", vec(dedupDefault, 1, 0.01, 0)))
	require.NoError(t, err)
	assert.Equal(t, "existing-2", out.Metadata["duplicate_of"])
	assert.Equal(t, "similar", out.Metadata["duplicate_kind"])
	sim, ok := out.Metadata["duplicate_similarity"].(float64)
	require.True(t, ok)
	assert.InDelta(t, 1.0, sim, 0.001, "similarity is 1 - cosine distance")
	assert.Nil(t, out.Metadata["suppress_output"])
}

func TestDedupStep_SimilarFoundDropPolicy(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "drop"}
	store := dedupStore(t, map[string][]pluginapi.ObjectVector{"existing-3": {vec(dedupDefault, 1, 0, 0)}})
	out, err := NewDedupStep(dedupModels, store, cfg).Run(context.Background(), dedupDraft("new-3", vec(dedupDefault, 1, 0, 0)))
	require.NoError(t, err)
	assert.Equal(t, "existing-3", out.Metadata["duplicate_of"])
	assert.Equal(t, true, out.Metadata["suppress_output"])
}

// The threshold compares similarity (1 - distance), not distance.
func TestDedupStep_BelowThresholdIsNotADuplicate(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "drop"}
	// cos([1,0,0],[1,1,0]) ~ 0.707: distance ~0.29, similarity ~0.71.
	store := dedupStore(t, map[string][]pluginapi.ObjectVector{"near": {vec(dedupDefault, 1, 1, 0)}})
	out, err := NewDedupStep(dedupModels, store, cfg).Run(context.Background(), dedupDraft("new", vec(dedupDefault, 1, 0, 0)))
	require.NoError(t, err)
	assert.Nil(t, out.Metadata["duplicate_of"])
}

// Only the default model's vector and index are consulted: a candidate
// model's near-identical neighbour must not flag the draft.
func TestDedupStep_UsesDefaultModelOnly(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "drop"}
	store := dedupStore(t, map[string][]pluginapi.ObjectVector{
		"cand-twin":   {vec(dedupCandidate, 1, 0)},
		"default-far": {vec(dedupDefault, 0, 0, 1)},
	})
	draft := dedupDraft("new", vec(dedupCandidate, 1, 0), vec(dedupDefault, 1, 0, 0))
	out, err := NewDedupStep(dedupModels, store, cfg).Run(context.Background(), draft)
	require.NoError(t, err)
	assert.Nil(t, out.Metadata["duplicate_of"], "matched through a non-default model")

	// And a default-model twin is found even when the candidate vector
	// comes first on the draft.
	store = dedupStore(t, map[string][]pluginapi.ObjectVector{"default-twin": {vec(dedupDefault, 1, 0, 0)}})
	out, err = NewDedupStep(dedupModels, store, cfg).Run(context.Background(), dedupDraft("new", vec(dedupCandidate, 0, 1), vec(dedupDefault, 1, 0, 0)))
	require.NoError(t, err)
	assert.Equal(t, "default-twin", out.Metadata["duplicate_of"])
}

func TestDedupStep_SkipsSelf(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "drop"}
	store := dedupStore(t, map[string][]pluginapi.ObjectVector{"self": {vec(dedupDefault, 1, 0, 0)}})
	out, err := NewDedupStep(dedupModels, store, cfg).Run(context.Background(), dedupDraft("self", vec(dedupDefault, 1, 0, 0)))
	require.NoError(t, err)
	assert.Nil(t, out.Metadata["duplicate_of"])
}

func TestDedupStep_CheckSimilarDisabled(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: false, SimilarityThreshold: 0.95, Policy: "drop"}
	store := dedupStore(t, map[string][]pluginapi.ObjectVector{"existing-4": {vec(dedupDefault, 1, 0, 0)}})
	out, err := NewDedupStep(dedupModels, store, cfg).Run(context.Background(), dedupDraft("new-4", vec(dedupDefault, 1, 0, 0)))
	require.NoError(t, err)
	assert.Nil(t, out.Metadata["duplicate_of"])
}

func TestDedupStep_NoDefaultOrNoDefaultVectorPassesThrough(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "drop"}
	store := dedupStore(t, map[string][]pluginapi.ObjectVector{"existing": {vec(dedupDefault, 1, 0, 0)}})

	noDefault := embeddingtest.Models{registry.Model{ModelID: dedupCandidate, Dimension: 2}}
	out, err := NewDedupStep(noDefault, store, cfg).Run(context.Background(), dedupDraft("new", vec(dedupDefault, 1, 0, 0)))
	require.NoError(t, err)
	assert.Nil(t, out.Metadata["duplicate_of"])

	out, err = NewDedupStep(dedupModels, store, cfg).Run(context.Background(), dedupDraft("new", vec(dedupCandidate, 1, 0)))
	require.NoError(t, err)
	assert.Nil(t, out.Metadata["duplicate_of"])
}

func TestDedupStep_SearchErrorIsNonFatal(t *testing.T) {
	captureLogs(t)
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "drop"}
	// No index for the default model: Search fails with ErrEmbeddingIndexMissing.
	out, err := NewDedupStep(dedupModels, embeddingtest.NewMemStore(), cfg).Run(context.Background(), dedupDraft("new", vec(dedupDefault, 1, 0, 0)))
	require.NoError(t, err)
	assert.Nil(t, out.Metadata["duplicate_of"])
}

func TestDedupStep_NilDependenciesPassThrough(t *testing.T) {
	cfg := config.DuplicatesConfig{CheckSimilar: true, SimilarityThreshold: 0.95, Policy: "drop"}
	store := dedupStore(t, map[string][]pluginapi.ObjectVector{"existing": {vec(dedupDefault, 1, 0, 0)}})
	for name, step := range map[string]*DedupStep{
		"nil models": NewDedupStep(nil, store, cfg),
		"nil store":  NewDedupStep(dedupModels, nil, cfg),
	} {
		out, err := step.Run(context.Background(), dedupDraft("new", vec(dedupDefault, 1, 0, 0)))
		require.NoError(t, err, name)
		assert.Nil(t, out.Metadata["duplicate_of"], name)
	}
}

func TestDedupStep_RequiresVectors(t *testing.T) {
	c := NewDedupStep(nil, nil, config.DuplicatesConfig{}).Contract()
	assert.Equal(t, []string{"Vectors"}, c.Requires)
}

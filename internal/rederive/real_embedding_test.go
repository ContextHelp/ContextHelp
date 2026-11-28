package rederive

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// embConfig names a loopback Ollama endpoint: local, as locality judges it.
const embConfig = `{"backend":"ollama","model":"fake","endpoint":"http://127.0.0.1:11434"}`

const (
	goodModel = "good-model@default"
	badModel  = "bad-model@x"
)

// embeddingEnv is an env whose pipelines embed with a model that works and
// one whose provider cannot be built, with an object ingested and stored
// vectors under both.
func embeddingEnv(t *testing.T) (*realEnv, *storage.KnowledgeObject) {
	e := newRealEnv(t)
	e.embModels = embeddingtest.Models{
		{ModelID: goodModel, Provider: "ollama", Dimension: 4, IsDefault: true, ConfigJSON: embConfig},
		{ModelID: badModel, Provider: "ollama", Dimension: 4, ConfigJSON: embConfig},
	}
	e.badModels = map[string]bool{badModel: true}
	ctx := context.Background()
	reg, err := registry.ForDriver(e.driver)
	require.NoError(t, err)
	for _, m := range []registry.Model{e.embModels[0], e.embModels[1]} {
		require.NoError(t, reg.Register(ctx, m, m.IsDefault))
		require.NoError(t, e.driver.Embeddings().EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: m.ModelID, Dimension: m.Dimension}))
	}
	e.useLLM(e.llmCfg)
	obj := e.ingest("text.short", "", "a short note about nothing in particular")
	require.Len(t, obj.Vectors, 1, "at ingest only the model that can be built embeds")
	require.NoError(t, e.driver.Embeddings().Put(ctx, obj.ID, obj.Vectors))
	require.NoError(t, e.driver.Embeddings().Put(ctx, obj.ID, []storage.ObjectVector{
		{ModelID: badModel, ChunkIdx: 0, Vector: []float32{9, 9, 9, 9}, Text: "old text"},
	}))
	e.record()
	return e, obj
}

// A model whose provider cannot be built is unusable: locality calls the
// pipeline automatic, and a reanalysis skips that model as ingest does, so
// the object is re-derived, the usable model's vectors are written and the
// unusable model's rows stay as they were.
func TestReal_UnusableEmbeddingModelIsSkippedAtReanalysis(t *testing.T) {
	e, obj := embeddingEnv(t)
	ctx := context.Background()
	oldGood, err := e.driver.Embeddings().Get(ctx, obj.ID, goodModel)
	require.NoError(t, err)
	require.NotEmpty(t, oldGood)

	plan, err := e.svc.ReanalysisLocality(ctx, "text.short", service.ReanalyzeOptions{})
	require.NoError(t, err)
	require.True(t, plan.Locality.Automatic(), "locality %s: %s", plan.Locality, plan.Reason)
	sig := e.bump("text.short", "tagger")

	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed, "%+v", res)
	require.Equal(t, sig, e.sigOf(obj.ID))
	require.Empty(t, e.skips(sig))

	good, err := e.driver.Embeddings().Get(ctx, obj.ID, goodModel)
	require.NoError(t, err)
	require.NotEmpty(t, good, "the usable model's vectors are written")
	bad, err := e.driver.Embeddings().Get(ctx, obj.ID, badModel)
	require.NoError(t, err)
	require.Len(t, bad, 1)
	require.Equal(t, "old text", bad[0].Text, "the unusable model's rows are untouched")
}

// A failed embed CALL of a model that was built fails the object: it is
// stamped current only when its vectors were written.
func TestReal_AFailedEmbedCallFailsTheObject(t *testing.T) {
	e, obj := embeddingEnv(t)
	sig := e.bump("text.short", "tagger")
	before := e.snapshot(obj.ID)
	e.embedFails.Store(true)

	res, err := e.run()
	require.NoError(t, err)
	require.Equal(t, 0, res.Processed, "%+v", res)
	require.Equal(t, 1, res.Transient)
	require.Equal(t, before, e.snapshot(obj.ID))
	require.NotEqual(t, sig, e.sigOf(obj.ID))
	require.Empty(t, e.skips(sig))
}

// A successful reanalysis hands its completed calls back through
// ReanalyzeOptions.Calls: the proof the job uses.
func TestReal_ASuccessfulReanalysisReportsItsCalls(t *testing.T) {
	e, obj := embeddingEnv(t)
	var got map[string]int
	_, _, err := e.svc.ReanalyzeObjectWith(ctxSystem(), obj.ID, service.ReanalyzeOptions{Calls: func(c map[string]int) { got = c }})
	require.NoError(t, err)
	require.Positive(t, got["llm"], "the tagger called the model: %v", got)
	require.Positive(t, got["embedding"], "the usable embedding model was called: %v", got)

	// A failed one reports nothing: the callback is for successes.
	e.embedFails.Store(true)
	called := false
	_, _, err = e.svc.ReanalyzeObjectWith(ctxSystem(), obj.ID, service.ReanalyzeOptions{Calls: func(map[string]int) { called = true }})
	require.Error(t, err)
	require.False(t, called)
}

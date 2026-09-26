package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedEmbedder returns the same query vector for every input.
type fixedEmbedder struct {
	vec []float32
	err error
}

func (f fixedEmbedder) Embed(context.Context, string) ([]float32, error) { return f.vec, f.err }
func (f fixedEmbedder) Dimensions() int                                  { return len(f.vec) }
func (f fixedEmbedder) Name() string                                     { return "fixed" }

// traceModel is the default embedding model the trace fixture indexes.
const traceModel = "trace-m"

// traceModels registers traceModel as the default at dimension 3.
var traceModels = embeddingtest.Models{{ModelID: traceModel, Provider: "fixed", Dimension: 3, IsDefault: true}}

// fixedResolver hands every model the same embedder.
type fixedResolver struct{ ep fixedEmbedder }

func (r fixedResolver) ForModel(context.Context, registry.Model) (providers.EmbeddingProvider, error) {
	return r.ep, nil
}

func (r fixedResolver) ForRegistration(context.Context) (providers.EmbeddingProvider, json.RawMessage, error) {
	return r.ep, nil, nil
}

// traceSemantic is the semantic leg over traceModel's index with ep
// embedding the query.
func traceSemantic(ep fixedEmbedder) retrieval.SemanticSource {
	return retrieval.SemanticSource{
		Models:   traceModels,
		Resolver: fixedResolver{ep: ep},
	}
}

// seedTraceFixture stores objects shaped so one query exercises every
// trace stage and leg combination, with vectors in traceModel's index on
// the real driver. Query "quantum" embeds to [1,0,0].
//
//	both-1, both-2  FTS hit + close embedding → both legs, returned
//	fts-1           FTS hit, no embedding     → fts only, cut by limit (2)
//	vec-1, vec-2    no FTS hit, embedding     → vector only, cut by threshold
//	note-1          FTS + vector hit, type "note" → excluded by Type filter
func seedTraceFixture(t *testing.T, svc *Service) {
	t.Helper()
	ctx := context.Background()
	reg, err := registry.ForDriver(svc.Store)
	require.NoError(t, err)
	require.NoError(t, reg.Register(ctx, traceModels[0], true))
	store := svc.Store.Embeddings()
	require.NoError(t, store.EnsureIndex(ctx, embeddings.SpecFor(traceModels[0])))
	add := func(id, typ, text string, emb []float32) {
		obj := makeSearchObject(id, []string{text}, "")
		obj.Type = typ
		require.NoError(t, svc.Store.Objects().Create(ctx, obj))
		if emb != nil {
			require.NoError(t, store.Put(ctx, id, []storage.ObjectVector{{ModelID: traceModel, Vector: emb}}))
		}
	}
	add("both-1", "text", "quantum computing error correction", []float32{1, 0, 0})
	add("both-2", "text", "quantum entanglement experiments", []float32{0.9, 0.1, 0})
	add("fts-1", "text", "quantum annealing hardware", nil)
	add("vec-1", "text", "photosynthesis chlorophyll pigments", []float32{0.8, 0.2, 0})
	add("vec-2", "text", "medieval pottery glazes", []float32{0, 0, 1})
	add("note-1", "note", "quantum field notes", []float32{1, 0, 0})
	rebuildFTS(t, svc)
}

func traceFixtureConfig() config.SearchConfig {
	return config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
		// FTS hits earn word overlap (0.15) on top of RRF; vector-only
		// hits carry RRF alone (~0.008) and fall below this threshold.
		MinScore: 0.1,
	}
}

func traceByID(tr *SearchTrace) map[string]TraceCandidate {
	out := make(map[string]TraceCandidate, len(tr.Candidates))
	for _, c := range tr.Candidates {
		out[c.ID] = c
	}
	return out
}

func TestHybridSearchTrace_StagesLegsAndScores(t *testing.T) {
	svc := newTestService(t)
	seedTraceFixture(t, svc)
	ctx := context.Background()
	cfg := traceFixtureConfig()
	ep := traceSemantic(fixedEmbedder{vec: []float32{1, 0, 0}})
	filter := storage.ObjectFilter{Limit: 2, Type: "text"}

	env, err := svc.HybridSearchExplainFilteredWithTrace(ctx, "quantum", filter, ep, cfg)
	require.NoError(t, err)
	require.NotNil(t, env.Trace)
	tr := env.Trace

	// Header.
	assert.Equal(t, "quantum", tr.Query)
	assert.Equal(t, SearchModeHybrid, tr.Mode)
	assert.Empty(t, tr.VectorError)
	assert.Equal(t, traceModel, tr.VectorModel, "model whose index the vector leg read")
	assert.Equal(t, retrieval.SemanticOK, tr.SemanticStatus)
	assert.Equal(t, 20, tr.FTSPool)
	assert.Equal(t, 20, tr.VectorPool)
	assert.Equal(t, 60, tr.RRFK)
	assert.InDelta(t, 0.5, tr.FTSWeight, 1e-12)
	assert.InDelta(t, 0.5, tr.VectorWeight, 1e-12)
	assert.InDelta(t, 0.1, tr.Threshold, 1e-12)
	assert.Equal(t, 2, tr.Limit)
	assert.InDelta(t, 0.15, tr.Weights.WordOverlap, 1e-12, "effective reranker weights echoed")

	byID := traceByID(tr)
	_, filtered := byID["note-1"]
	assert.False(t, filtered, "Type filter must exclude note-1 from the trace as from results")
	require.Len(t, tr.Candidates, 5)

	want := map[string]struct {
		legs  TraceLegs
		stage TraceStage
	}{
		"both-1": {TraceLegsBoth, TraceStageReturned},
		"both-2": {TraceLegsBoth, TraceStageReturned},
		"fts-1":  {TraceLegsFTS, TraceStageCutLimit},
		"vec-1":  {TraceLegsVector, TraceStageCutThreshold},
		"vec-2":  {TraceLegsVector, TraceStageCutThreshold},
	}
	for id, w := range want {
		c, ok := byID[id]
		require.True(t, ok, "candidate %s missing from trace", id)
		assert.Equal(t, w.legs, c.Legs, "%s legs", id)
		assert.Equal(t, w.stage, c.Stage, "%s stage", id)
		assert.NotNil(t, c.Object, "%s object", id)

		inFTS := w.legs != TraceLegsVector
		inVec := w.legs != TraceLegsFTS
		assert.Equal(t, inFTS, c.FTSRank > 0, "%s fts rank presence", id)
		assert.Equal(t, inFTS, c.FTSRaw != nil, "%s fts raw presence", id)
		assert.Equal(t, inVec, c.VectorRank > 0, "%s vector rank presence", id)
		assert.Equal(t, inVec, c.VectorRaw != nil, "%s vector raw presence", id)
		assert.InDelta(t, c.Breakdown.FTS+c.Breakdown.Vector, c.RRF, 1e-12, "%s rrf", id)
		if inFTS {
			wantFTS := 0.5 / float64(60+c.FTSRank)
			assert.InDelta(t, wantFTS, c.Breakdown.FTS, 1e-12, "%s fts rrf matches rank", id)
		} else {
			assert.Zero(t, c.Breakdown.FTS)
		}
	}

	// Raw cosine (score = 1 - cosine distance) from the vector leg.
	assert.InDelta(t, 1.0, *byID["both-1"].VectorRaw, 1e-6)
	assert.InDelta(t, 0.0, *byID["vec-2"].VectorRaw, 1e-6)
	assert.Equal(t, 1, byID["both-1"].VectorRank)
	assert.NotZero(t, *byID["fts-1"].FTSRaw, "bm25 raw score recorded")

	// Final rank: dense 1..N over the reranked order; stages follow it.
	for i, c := range tr.Candidates {
		assert.Equal(t, i+1, c.Rank)
		if i > 0 {
			assert.GreaterOrEqual(t, tr.Candidates[i-1].Breakdown.Total, c.Breakdown.Total)
		}
	}

	// Counts.
	cnt := tr.Counts
	assert.Equal(t, 3, cnt.FTSHits)
	assert.Equal(t, 4, cnt.VectorHits)
	assert.Equal(t, 5, cnt.Candidates)
	assert.Equal(t, 2, cnt.Both)
	assert.Equal(t, 1, cnt.FTSOnly)
	assert.Equal(t, 2, cnt.VectorOnly)
	assert.Equal(t, 2, cnt.Returned)
	assert.Equal(t, 1, cnt.CutLimit)
	assert.Equal(t, 2, cnt.CutThreshold)
	assert.Equal(t, env.Diagnostics.CandidateCount, cnt.Candidates)
	assert.Equal(t, env.Diagnostics.BelowThresholdCount, cnt.CutThreshold)

	// Returned candidates carry exactly the breakdown the results report.
	require.Len(t, env.Results, 2)
	for i, r := range env.Results {
		c := tr.Candidates[i]
		assert.Equal(t, TraceStageReturned, c.Stage)
		assert.Equal(t, r.Object.ID, c.ID)
		assert.Equal(t, r.Breakdown, c.Breakdown)
	}
}

// TestHybridSearchTrace_NonTracePathUnchanged: the trace is opt-in. The
// diagnostics envelope carries no trace, and results/diagnostics are
// identical to the traced call on the same fixture.
func TestHybridSearchTrace_NonTracePathUnchanged(t *testing.T) {
	svc := newTestService(t)
	seedTraceFixture(t, svc)
	ctx := context.Background()
	cfg := traceFixtureConfig()
	ep := traceSemantic(fixedEmbedder{vec: []float32{1, 0, 0}})
	filter := storage.ObjectFilter{Limit: 2, Type: "text"}

	plain, err := svc.HybridSearchExplainFilteredWithDiagnostics(ctx, "quantum", filter, ep, cfg)
	require.NoError(t, err)
	assert.Nil(t, plain.Trace, "trace must be nil unless requested")

	raw, err := json.Marshal(plain)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), `"trace"`, "untraced envelope JSON unchanged")

	traced, err := svc.HybridSearchExplainFilteredWithTrace(ctx, "quantum", filter, ep, cfg)
	require.NoError(t, err)
	assert.Equal(t, plain.Diagnostics, traced.Diagnostics)
	require.Len(t, traced.Results, len(plain.Results))
	for i := range plain.Results {
		assert.Equal(t, plain.Results[i].Object.ID, traced.Results[i].Object.ID)
		assert.Equal(t, plain.Results[i].Breakdown, traced.Results[i].Breakdown)
	}

	// With the same inputs, the untraced wrappers still agree.
	explained, err := svc.HybridSearchExplainFiltered(ctx, "quantum", filter, ep, cfg)
	require.NoError(t, err)
	require.Len(t, explained, 2)
	assert.Equal(t, plain.Results[0].Breakdown, explained[0].Breakdown)
}

// TestHybridSearchTrace_CutScoresMatchUncutExplain: a
// candidate cut by threshold or limit carries the same breakdown the
// explain path reports once the cut is lifted.
func TestHybridSearchTrace_CutScoresMatchUncutExplain(t *testing.T) {
	svc := newTestService(t)
	seedTraceFixture(t, svc)
	ctx := context.Background()
	ep := traceSemantic(fixedEmbedder{vec: []float32{1, 0, 0}})

	cfg := traceFixtureConfig()
	env, err := svc.HybridSearchExplainFilteredWithTrace(ctx, "quantum",
		storage.ObjectFilter{Limit: 2, Type: "text"}, ep, cfg)
	require.NoError(t, err)

	open := cfg
	open.MinScore = 0
	all, err := svc.HybridSearchExplainFiltered(ctx, "quantum",
		storage.ObjectFilter{Type: "text"}, ep, open)
	require.NoError(t, err)
	require.Len(t, all, len(env.Trace.Candidates))

	for i, r := range all {
		c := env.Trace.Candidates[i]
		assert.Equal(t, r.Object.ID, c.ID, "rank %d", i+1)
		assert.Equal(t, r.Breakdown, c.Breakdown, "%s breakdown", c.ID)
	}
}

func TestHybridSearchTrace_FTSOnlyModes(t *testing.T) {
	svc := newTestService(t)
	seedTraceFixture(t, svc)
	ctx := context.Background()
	cfg := traceFixtureConfig()
	filter := storage.ObjectFilter{Limit: 10, Type: "text"}

	t.Run("no default model", func(t *testing.T) {
		env, err := svc.HybridSearchExplainFilteredWithTrace(ctx, "quantum", filter, retrieval.SemanticSource{}, cfg)
		require.NoError(t, err)
		tr := env.Trace
		assert.Equal(t, SearchModeFTSOnly, tr.Mode)
		assert.Empty(t, tr.VectorError)
		assert.Empty(t, tr.VectorModel)
		assert.Equal(t, retrieval.SemanticNoDefaultModel, tr.SemanticStatus)
		assert.Equal(t, 0, tr.VectorPool)
		assert.Equal(t, 0, tr.Counts.VectorHits)
		assert.Equal(t, 3, tr.Counts.FTSOnly)
		for _, c := range tr.Candidates {
			assert.Equal(t, TraceLegsFTS, c.Legs)
			assert.Nil(t, c.VectorRaw)
		}
	})

	t.Run("vector leg error degrades", func(t *testing.T) {
		ep := traceSemantic(fixedEmbedder{err: errors.New("embedder down")})
		env, err := svc.HybridSearchExplainFilteredWithTrace(ctx, "quantum", filter, ep, cfg)
		require.NoError(t, err)
		assert.Equal(t, SearchModeFTSFallback, env.Trace.Mode)
		assert.Contains(t, env.Trace.VectorError, "embedder down")
		assert.Equal(t, traceModel, env.Trace.VectorModel)
		assert.Equal(t, retrieval.SemanticProviderError, env.Trace.SemanticStatus)
		assert.Equal(t, 20, env.Trace.VectorPool, "the leg was attempted")
		assert.Equal(t, 0, env.Trace.Counts.VectorHits)
	})

	t.Run("dimension mismatch degrades", func(t *testing.T) {
		ep := traceSemantic(fixedEmbedder{vec: []float32{1, 0}})
		env, err := svc.HybridSearchExplainFilteredWithTrace(ctx, "quantum", filter, ep, cfg)
		require.NoError(t, err)
		assert.Equal(t, SearchModeFTSFallback, env.Trace.Mode)
		assert.Equal(t, retrieval.SemanticDimensionMismatch, env.Trace.SemanticStatus)
		assert.Contains(t, env.Trace.VectorError, "2 dimensions")
	})

	t.Run("no fallback fails instead of tracing", func(t *testing.T) {
		strict := cfg
		strict.FallbackToFTS = false
		ep := traceSemantic(fixedEmbedder{err: errors.New("embedder down")})
		_, err := svc.HybridSearchExplainFilteredWithTrace(ctx, "quantum", filter, ep, strict)
		require.Error(t, err)
	})
}

func TestHybridSearchTrace_JSONShape(t *testing.T) {
	svc := newTestService(t)
	seedTraceFixture(t, svc)
	env, err := svc.HybridSearchExplainFilteredWithTrace(context.Background(), "quantum",
		storage.ObjectFilter{Limit: 2, Type: "text"}, traceSemantic(fixedEmbedder{vec: []float32{1, 0, 0}}), traceFixtureConfig())
	require.NoError(t, err)

	raw, err := json.Marshal(env.Trace)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	for _, k := range []string{"query", "mode", "vector_model", "semantic_status", "fts_pool", "vector_pool", "rrf_k", "threshold", "limit", "weights", "counts", "candidates"} {
		assert.Contains(t, decoded, k)
	}
	cands := decoded["candidates"].([]any)
	first := cands[0].(map[string]any)
	for _, k := range []string{"id", "legs", "fts_rank", "vector_rank", "fts_raw", "vector_raw", "rrf", "score_breakdown", "rank", "stage"} {
		assert.Contains(t, first, k)
	}
	assert.NotContains(t, first, "object", "full object stays out of the JSON trace")
}

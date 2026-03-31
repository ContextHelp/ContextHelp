package integration

// US-0021: Search with Result Explanation
// Runs search with explain=true (HybridSearchExplain), verifies each result
// has a score breakdown with non-zero total and per-signal fields.

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUS0021_ExplainReturnsScoreBreakdownPerResult verifies that every result
// from HybridSearchExplain carries a non-zero total and consistent breakdown.
func TestUS0021_ExplainReturnsScoreBreakdownPerResult(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for _, tc := range []struct{ id, summary string }{
		{"exp-01", "observability tracing distributed systems jaeger"},
		{"exp-02", "structured logging contextual metadata"},
		{"exp-03", "metrics alerting prometheus grafana"},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      "text",
			Summaries: []string{tc.summary},
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}
	rebuildFTSIntegration(t, env)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}
	results, err := env.svc.HybridSearchExplain(ctx, "observability", 10, nil, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, results, "explain search must return at least one result")

	for _, r := range results {
		assert.NotNil(t, r.Object, "every result must have an object")
		assert.Greater(t, r.Breakdown.Total, 0.0, "total score must be positive for %s", r.Object.ID)

		// Total must equal the sum of all signal contributions.
		wantTotal := r.Breakdown.FTS + r.Breakdown.Vector +
			r.Breakdown.MentionBoost + r.Breakdown.GraphRelevance +
			r.Breakdown.WordOverlap
		assert.InDelta(t, wantTotal, r.Breakdown.Total, 1e-9, "total must equal sum of signals for %s", r.Object.ID)
	}
}

// TestUS0021_ExplainFTSContributesWhenNoEmbeddingProvider verifies that in
// FTS-fallback mode the FTS signal is non-zero and vector signal is zero.
func TestUS0021_ExplainFTSContributesWhenNoEmbeddingProvider(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID:        "exp-fts-01",
		Type:      "text",
		Summaries: []string{"rate limiting throttling token bucket"},
		CreatedAt: now,
		UpdatedAt: now,
	}))
	rebuildFTSIntegration(t, env)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}
	results, err := env.svc.HybridSearchExplain(ctx, "rate limiting", 5, nil, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	r := results[0]
	assert.Equal(t, "exp-fts-01", r.Object.ID)
	assert.Greater(t, r.Breakdown.FTS, 0.0, "FTS must contribute to score in fallback mode")
	assert.Equal(t, 0.0, r.Breakdown.Vector, "vector must be 0 without embedding provider")
}

// TestUS0021_ExplainTotalMatchesHybridSearchRRFScore verifies that the plain
// HybridSearch rrf_score metadata equals the explain total.
func TestUS0021_ExplainTotalMatchesHybridSearchRRFScore(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID:        "exp-cmp-01",
		Type:      "text",
		Summaries: []string{"zero-downtime deployment blue green canary"},
		CreatedAt: now,
		UpdatedAt: now,
	}))
	rebuildFTSIntegration(t, env)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	plain, err := env.svc.HybridSearch(ctx, "blue green deployment", 5, nil, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, plain)

	explained, err := env.svc.HybridSearchExplain(ctx, "blue green deployment", 5, nil, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, explained)

	rrfScore, ok := plain[0].Metadata["rrf_score"].(float64)
	require.True(t, ok, "rrf_score must be float64")
	assert.InDelta(t, rrfScore, explained[0].Breakdown.Total, 1e-9,
		"plain rrf_score and explain total must match")
}

// TestUS0021_ExplainNoMatchReturnsEmptyList verifies that an unmatched query
// returns an empty list (not an error) from HybridSearchExplain.
func TestUS0021_ExplainNoMatchReturnsEmptyList(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		FallbackToFTS: true,
	}
	results, err := env.svc.HybridSearchExplain(ctx, "zzz_no_match_xyzzy", 5, nil, cfg)
	require.NoError(t, err)
	assert.Empty(t, results)
}

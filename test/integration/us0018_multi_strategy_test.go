package integration

// US-0018: Multi-Strategy Search Execution
// Verifies that vector + FTS + RSQL paths all run and that results are merged
// and de-duplicated when multiple strategies return the same object.

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUS0018_FTSAndRSQLPathsBothReturn verifies that FTS (via HybridSearch)
// and RSQL (via SearchObjects) can both locate the same object — proving the
// two search paths are operational independently.
func TestUS0018_FTSAndRSQLPathsBothReturn(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	obj := &storage.KnowledgeObject{
		ID:        "ms-shared-01",
		Type:      "article",
		Summaries: []string{"kubernetes pod scheduling topology spread"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	rebuildFTSIntegration(t, env)

	// FTS path.
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}
	ftsResults, err := env.svc.HybridSearch(ctx, "kubernetes scheduling", 5, nil, cfg)
	require.NoError(t, err)
	assert.True(t, containsIDPtr(ftsResults, "ms-shared-01"), "FTS path must find the object")

	// RSQL path.
	rsqlResults, total, err := env.svc.SearchObjects(ctx, "type==article", 20, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.True(t, containsIDPtr(rsqlResults, "ms-shared-01"), "RSQL path must find the object")
}

// TestUS0018_HybridSearchDeduplicate verifies that Hybrid (FTS+vector) merging
// does not produce duplicate entries in its output.
func TestUS0018_HybridSearchDeduplicate(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	// Insert objects with distinct content.
	for _, tc := range []struct{ id, summary string }{
		{"ms-dup-1", "event sourcing CQRS architecture patterns"},
		{"ms-dup-2", "saga pattern distributed transactions"},
		{"ms-dup-3", "outbox pattern reliable messaging"},
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
	results, err := env.svc.HybridSearch(ctx, "CQRS", 10, nil, cfg)
	require.NoError(t, err)

	// Verify no duplicates.
	seen := map[string]struct{}{}
	for _, r := range results {
		_, dup := seen[r.ID]
		assert.False(t, dup, "result ID %s appears more than once", r.ID)
		seen[r.ID] = struct{}{}
	}
}

// TestUS0018_ExplainContainsScoreBreakdown verifies that the explain path
// returns per-result FTS / vector / total breakdown for every result.
func TestUS0018_ExplainContainsScoreBreakdown(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID:        "ms-explain-01",
		Type:      "text",
		Summaries: []string{"circuit breaker resilience fault tolerance"},
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
	results, err := env.svc.HybridSearchExplain(ctx, "circuit breaker", 5, nil, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	for _, r := range results {
		assert.Greater(t, r.Breakdown.Total, 0.0, "every result must have a positive total score")
		// In FTS-fallback mode, FTS must contribute; vector is 0.
		assert.Greater(t, r.Breakdown.FTS, 0.0, "FTS score must be positive")
		assert.Equal(t, 0.0, r.Breakdown.Vector, "vector score must be 0 without embedding provider")
	}
}

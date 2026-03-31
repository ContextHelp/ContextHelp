package service

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeSearchObject builds a graph-canonical KO for search tests.
// The first element of summaries becomes the graph summary node content,
// driving FTS via ProjectIndex. rawContent is stored for flat-fallback tests.
func makeSearchObject(id string, summaries []string, rawContent string) *storage.KnowledgeObject {
	content := rawContent
	if len(summaries) > 0 && summaries[0] != "" {
		content = summaries[0]
	}
	obj := storageutil.BuildGraphKO(id, "text", content)
	// Preserve RawContent for tests that check it directly.
	if rawContent != "" {
		obj.RawContent = rawContent
	}
	return obj
}

func rebuildFTS(t *testing.T, svc *Service) {
	t.Helper()
	d, ok := svc.Store.(*sqlite.Driver)
	require.True(t, ok, "store must be *sqlite.Driver for FTS rebuild")
	_, err := d.DB().ExecContext(context.Background(), "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)
}

func TestHybridSearch_FTSOnly_WhenNoEmbeddingProvider(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("hs-1", []string{"distributed systems fault tolerance"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	results, err := svc.HybridSearch(ctx, "distributed", 10, nil, cfg)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hs-1", results[0].ID)
}

func TestFindByText_UsesFTS(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("fbt-1", []string{"microservices resilience patterns"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	results, err := svc.FindByText(ctx, "resilience", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "fbt-1", results[0].ID)
}

func TestHybridSearch_ErrorWhenNoProvider_FallbackDisabled(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		FallbackToFTS: false,
	}
	_, err := svc.HybridSearch(ctx, "anything", 10, nil, cfg)
	require.Error(t, err)
}

func TestHybridSearchExplain_ReturnsBreakdown(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("exp-1", []string{"observability tracing distributed systems"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	results, err := svc.HybridSearchExplain(ctx, "observability", 10, nil, cfg)
	require.NoError(t, err)
	require.Len(t, results, 1)

	r := results[0]
	assert.Equal(t, "exp-1", r.Object.ID)

	// FTS leg should have contributed (no vector provider — fallback mode).
	assert.Greater(t, r.Breakdown.FTS, 0.0)
	// Vector leg should be zero — no embedding provider.
	assert.Equal(t, 0.0, r.Breakdown.Vector)
	// Total must equal sum of all signals.
	wantTotal := r.Breakdown.FTS + r.Breakdown.Vector + r.Breakdown.MentionBoost + r.Breakdown.GraphRelevance + r.Breakdown.WordOverlap
	assert.InDelta(t, wantTotal, r.Breakdown.Total, 1e-9)
}

func TestHybridSearchExplain_ErrorWhenNoProvider_FallbackDisabled(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		FallbackToFTS: false,
	}
	_, err := svc.HybridSearchExplain(ctx, "anything", 10, nil, cfg)
	require.Error(t, err)
}

func TestHybridSearchExplain_TotalMatchesHybridSearchRRFScore(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	obj := makeSearchObject("cmp-1", []string{"event sourcing CQRS architecture patterns"}, "")
	require.NoError(t, svc.Store.Objects().Create(ctx, obj))
	rebuildFTS(t, svc)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	plain, err := svc.HybridSearch(ctx, "event sourcing", 10, nil, cfg)
	require.NoError(t, err)
	require.Len(t, plain, 1)

	explained, err := svc.HybridSearchExplain(ctx, "event sourcing", 10, nil, cfg)
	require.NoError(t, err)
	require.Len(t, explained, 1)

	// The rrf_score stored in Metadata by HybridSearch must equal the explain total.
	rrfScore, ok := plain[0].Metadata["rrf_score"].(float64)
	require.True(t, ok, "rrf_score must be float64")
	assert.InDelta(t, rrfScore, explained[0].Breakdown.Total, 1e-9)
}

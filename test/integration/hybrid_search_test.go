//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHybridSearch_EndToEnd(t *testing.T) {
	env := startTestEnv(t)
	ctx := context.Background()

	// Access the underlying SQLite driver for FTS rebuild.
	d, ok := env.svc.Store.(*sqlite.Driver)
	require.True(t, ok, "store must be *sqlite.Driver")

	// Insert graph-canonical objects. ProjectIndex derives FTSBody from graph
	// summary nodes, so projected_fts_body is populated on Create.
	for _, tc := range []struct{ id, summary string }{
		{"hs-auth", "oauth2 authentication flow"},
		{"hs-cache", "redis caching strategies"},
		{"hs-db", "postgres query optimisation"},
	} {
		obj := storageutil.BuildGraphKO(tc.id, "text", tc.summary)
		obj.CreatedAt = nowTrunc()
		obj.UpdatedAt = nowTrunc()
		require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	}

	// Rebuild FTS content table.
	_, err := d.DB().ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	// FTS fallback (no embedding provider).
	results, err := env.svc.HybridSearch(ctx, "authentication", 5, retrieval.SemanticSource{}, cfg)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hs-auth", results[0].ID)
	assert.Contains(t, results[0].Metadata, "rrf_score")

	// FTS-only mode via FindByText.
	results, err = env.svc.FindByText(ctx, "caching", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hs-cache", results[0].ID)

	// MinScore filter — set high to exclude all.
	cfgHighScore := cfg
	cfgHighScore.MinScore = 999.0
	results, err = env.svc.HybridSearch(ctx, "postgres", 5, retrieval.SemanticSource{}, cfgHighScore)
	require.NoError(t, err)
	assert.Empty(t, results)
}

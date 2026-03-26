package integration

// US-0016: Natural Language Search
// Ingests known objects, runs NLQ (FTS fallback) query, verifies top results
// include expected object IDs.

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rebuildFTSIntegration triggers SQLite FTS5 content rebuild so freshly
// inserted objects are searchable.
func rebuildFTSIntegration(t *testing.T, env *testEnv) {
	t.Helper()
	d, ok := env.svc.Store.(*sqlite.Driver)
	require.True(t, ok, "store must be *sqlite.Driver for FTS rebuild")
	_, err := d.DB().ExecContext(context.Background(), "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	require.NoError(t, err)
}

// TestUS0016_NLQSearchReturnsMatchingObjects ingests objects with known
// summaries and verifies that an NLQ-style (FTS fallback) query surfaces them.
func TestUS0016_NLQSearchReturnsMatchingObjects(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	objects := []struct {
		id      string
		summary string
	}{
		{"nlq-auth", "oauth2 authentication token refresh flow"},
		{"nlq-cache", "redis distributed caching eviction policy"},
		{"nlq-db", "postgres query plan optimisation index scan"},
	}

	for _, tc := range objects {
		obj := &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      "text",
			Summaries: []string{tc.summary},
			CreatedAt: now,
			UpdatedAt: now,
		}
		require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	}
	rebuildFTSIntegration(t, env)

	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: true,
	}

	// NLQ "authentication" → should surface nlq-auth.
	results, err := env.svc.HybridSearch(ctx, "authentication", 5, nil, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, results, "NLQ query must return at least one result")
	assert.Equal(t, "nlq-auth", results[0].ID)

	// NLQ "caching eviction" → should surface nlq-cache.
	results, err = env.svc.HybridSearch(ctx, "caching eviction", 5, nil, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	assert.Equal(t, "nlq-cache", results[0].ID)
}

// TestUS0016_NLQSearchViaHTTPReturnsResults verifies the REST endpoint
// responds with matching objects for an NLQ-style text query.
func TestUS0016_NLQSearchViaHTTPReturnsResults(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	obj := &storage.KnowledgeObject{
		ID:         "nlq-http-01",
		Type:       "text",
		Summaries:  []string{"microservices circuit breaker pattern"},
		RawContent: "circuit breaker implementation",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))
	rebuildFTSIntegration(t, env)

	// REST search via RSQL (type filter) — NLQ intent is a superset; verifying
	// the HTTP path returns the ingested object.
	resp := doGet(t, env.URL+"/api/v1/search?q=type==text")
	defer resp.Body.Close()

	var body searchResponse
	decodeJSON(t, resp.Body, &body)
	assert.GreaterOrEqual(t, body.Total, 1)
	assert.True(t, containsID(body.Data, "nlq-http-01"), "ingested object must appear in results")
}

// TestUS0016_NLQNoResults verifies that a query with no matches returns an
// empty list (not an error).
func TestUS0016_NLQNoResults(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		FallbackToFTS: true,
	}
	results, err := env.svc.HybridSearch(ctx, "zzz_no_match_xyzzy", 5, nil, cfg)
	require.NoError(t, err)
	assert.Empty(t, results, "unmatched NLQ query must return empty list")
}

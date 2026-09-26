package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Query embeddings replay cassettes recorded against a real Ollama serving
// snowflake-arctic-embed2 (1024 dimensions). Re-record with
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run Semantic ./internal/service/
const semanticCassettes = "testdata/cassettes/query-path"

type semanticFixture struct {
	svc   *Service
	db    *sql.DB
	emb   *storagetest.MemEmbeddingStore
	reg   *registry.Store
	calls *providertest.OllamaCalls
	sem   retrieval.SemanticSource
}

// newSemanticFixture registers sem-a (default, endpoint :11434) and sem-b
// (endpoint :11556), both snowflake-arctic-embed2 at 1024 dimensions, and
// indexes the "vec-a" note only under sem-a and "vec-b" only under sem-b.
// Neither note shares a token with semanticQuery, so any hit on them came
// from the vector leg.
func newSemanticFixture(t *testing.T) *semanticFixture {
	t.Helper()
	ctx := context.Background()
	client, calls := providertest.OllamaClient(t, semanticCassettes)
	emb := storagetest.NewMemEmbeddingStore()
	drv := storagetest.WithEmbeddings(storageutil.NewTestDriver(t), emb)
	db := drv.(interface{ DB() *sql.DB }).DB()
	reg := registry.New(db)
	r := &embeddings.Resolver{LookupEnv: func(string) (string, bool) { return "", false }, HTTPClient: client}
	pr := embeddings.NewProviderResolver(r)
	pipes := pipeline.DefaultRegistry()
	svc := New(drv, jobs.NewQueue(drv.Jobs()), pipes, search.NewEngine(drv), "", nil)
	f := &semanticFixture{
		svc: svc, db: db, emb: emb, reg: reg, calls: calls,
		sem: retrieval.SemanticSource{Models: reg, Resolver: pr},
	}

	for _, m := range []registry.Model{
		{
			ModelID: "sem-a", Provider: "ollama", Dimension: 1024,
			ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://127.0.0.1:11434"}`,
		},
		{
			ModelID: "sem-b", Provider: "ollama", Dimension: 1024,
			ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://127.0.0.1:11556"}`,
		},
	} {
		require.NoError(t, reg.Register(ctx, m, m.ModelID == "sem-a"))
		require.NoError(t, emb.EnsureIndex(ctx, embeddings.SpecFor(m)))
	}
	for id, model := range map[string]string{"vec-a": "sem-a", "vec-b": "sem-b"} {
		text := "Vectors for " + id + " describe embedding migration between registered encoders."
		require.NoError(t, drv.Objects().Create(ctx, makeSearchObject(id, []string{text}, "")))
		m, err := reg.Get(ctx, model)
		require.NoError(t, err)
		p, err := pr.ForModel(ctx, *m)
		require.NoError(t, err)
		vec, err := p.Embed(ctx, text)
		require.NoError(t, err)
		require.NoError(t, emb.Put(ctx, id, []storage.ObjectVector{{ModelID: model, Vector: vec}}))
	}
	require.NoError(t, drv.Objects().Create(ctx, makeSearchObject("fts-only", []string{"switching the default model"}, "")))
	return f
}

const semanticQuery = "switching the default model"

func semanticCfg(fallback bool) config.SearchConfig {
	return config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20},
		FallbackToFTS: fallback,
	}
}

func resultIDs(objs []*storage.KnowledgeObject) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.ID
	}
	return out
}

// The hybrid vector leg searches the default model's index, and follows a
// set-default flip made through another registry handle on the next query.
func TestHybridSearch_SemanticLegReadsDefaultModelPerQuery(t *testing.T) {
	f := newSemanticFixture(t)
	ctx := context.Background()

	objs, diag, err := f.svc.HybridSearchFilteredWithDiagnostics(ctx, semanticQuery, storage.ObjectFilter{Limit: 10}, f.sem, semanticCfg(false))
	require.NoError(t, err)
	require.NotNil(t, diag.Semantic)
	assert.Equal(t, retrieval.SemanticOK, diag.Semantic.Status)
	assert.Equal(t, "sem-a", diag.Semantic.ModelID)
	assert.Empty(t, diag.Semantic.Notice)
	ids := resultIDs(objs)
	assert.Contains(t, ids, "fts-only", "FTS leg still runs")
	assert.Contains(t, ids, "vec-a", "vector leg hit from sem-a's index")
	assert.NotContains(t, ids, "vec-b", "sem-b's index must not be searched while sem-a is the default")

	_, err = registry.New(f.db).SetDefault(ctx, "sem-b", 0)
	require.NoError(t, err)

	objs, diag, err = f.svc.HybridSearchFilteredWithDiagnostics(ctx, semanticQuery, storage.ObjectFilter{Limit: 10}, f.sem, semanticCfg(false))
	require.NoError(t, err)
	assert.Equal(t, "sem-b", diag.Semantic.ModelID)
	ids = resultIDs(objs)
	assert.Contains(t, ids, "vec-b")
	assert.NotContains(t, ids, "vec-a")
	urls := f.calls.URLs()
	assert.Equal(t, "http://127.0.0.1:11556/api/embeddings", urls[len(urls)-1], "query embedded by sem-b's provider")
}

// No default model: hybrid returns the FTS results and says why the vector
// leg did not run, in the diagnostics and their JSON form.
func TestHybridSearch_NoDefaultModelFallsBackVisibly(t *testing.T) {
	f := newSemanticFixture(t)
	ctx := context.Background()
	_, err := f.db.Exec(`UPDATE embedding_models SET is_default = 0`)
	require.NoError(t, err)

	objs, diag, err := f.svc.HybridSearchFilteredWithDiagnostics(ctx, semanticQuery, storage.ObjectFilter{Limit: 10}, f.sem, semanticCfg(true))
	require.NoError(t, err)
	assert.Equal(t, []string{"fts-only"}, resultIDs(objs))
	require.NotNil(t, diag.Semantic)
	assert.Equal(t, retrieval.SemanticNoDefaultModel, diag.Semantic.Status)
	assert.Contains(t, diag.Semantic.Notice, "semantic search unavailable (no_default_model)")
	assert.Empty(t, f.emb.Searches(), "no index is searched without a default model")

	raw, err := json.Marshal(diag)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"semantic":{"status":"no_default_model"`)
	assert.Contains(t, string(raw), `"notice":"semantic search unavailable (no_default_model)`)

	_, _, err = f.svc.HybridSearchFilteredWithDiagnostics(ctx, semanticQuery, storage.ObjectFilter{Limit: 10}, f.sem, semanticCfg(false))
	require.Error(t, err, "fallback_to_fts=false must not degrade")
	assert.Contains(t, err.Error(), "no_default_model")
}

// Vector-only search degrades to FTS the same visible way.
func TestSemanticSearchFiltered_FallsBackToFTSVisibly(t *testing.T) {
	f := newSemanticFixture(t)
	ctx := context.Background()

	objs, diag, err := f.svc.SemanticSearchFiltered(ctx, semanticQuery, storage.ObjectFilter{Limit: 10}, f.sem, semanticCfg(true))
	require.NoError(t, err)
	assert.Equal(t, retrieval.SemanticOK, diag.Semantic.Status)
	assert.NotContains(t, resultIDs(objs), "fts-only", "vector-only mode returns index hits only")
	for _, o := range objs {
		score, ok := o.Metadata["score"].(float64)
		assert.True(t, ok && score > 0 && score <= 1, "%s score %v", o.ID, o.Metadata["score"])
	}

	require.NoError(t, f.emb.PurgeModel(ctx, "sem-a"))
	objs, diag, err = f.svc.SemanticSearchFiltered(ctx, semanticQuery, storage.ObjectFilter{Limit: 10}, f.sem, semanticCfg(true))
	require.NoError(t, err)
	assert.Equal(t, retrieval.SemanticIndexMissing, diag.Semantic.Status)
	assert.True(t, strings.HasPrefix(diag.Semantic.Notice, "semantic search unavailable (index_missing)"), diag.Semantic.Notice)
	assert.Equal(t, []string{"fts-only"}, resultIDs(objs))

	_, _, err = f.svc.SemanticSearchFiltered(ctx, semanticQuery, storage.ObjectFilter{Limit: 10}, f.sem, semanticCfg(false))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index_missing")
}

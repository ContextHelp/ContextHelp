package retrieval_test

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Query embeddings replay the query-path cassettes, recorded against a real
// Ollama serving snowflake-arctic-embed2 (1024 dimensions). Re-record with
//
//	XRR_MODE=record go test -tags fts5 -count=1 ./internal/retrieval/
const queryPathCassettes = "testdata/cassettes/query-path"

// Two registered models share the embedding model but not the endpoint, so
// the endpoint a query reached names the model whose provider embedded it.
const (
	modelA    = "arctic-a"
	modelB    = "arctic-b"
	endpointA = "http://127.0.0.1:11434"
	endpointB = "http://127.0.0.1:11556"
	tunnel    = "http://127.0.0.1:11555"
)

func arcticModel(id, endpoint string) registry.Model {
	return registry.Model{
		ModelID:    id,
		Provider:   "ollama",
		Dimension:  1024,
		ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"` + endpoint + `"}`,
	}
}

type queryPathFixture struct {
	drv   storage.StorageDriver
	db    *sql.DB
	emb   *storagetest.MemEmbeddingStore
	reg   *registry.Store
	calls *providertest.OllamaCalls
	r     *embeddings.Resolver
}

// newQueryPathFixture registers arctic-a (default) and arctic-b, indexes
// both, and stores one document under each: doc-a only under arctic-a and
// doc-b only under arctic-b, so the hits name the index a query searched.
func newQueryPathFixture(t *testing.T) *queryPathFixture {
	t.Helper()
	ctx := context.Background()
	client, calls := providertest.OllamaClient(t, queryPathCassettes)
	emb := storagetest.NewMemEmbeddingStore()
	drv := storagetest.WithEmbeddings(storageutil.NewTestDriver(t), emb)
	db := drv.(interface{ DB() *sql.DB }).DB()
	reg := registry.New(db)
	f := &queryPathFixture{
		drv: drv, db: db, emb: emb, reg: reg, calls: calls,
		r: &embeddings.Resolver{
			LookupEnv:  func(string) (string, bool) { return "", false },
			HTTPClient: client,
		},
	}

	for _, m := range []registry.Model{arcticModel(modelA, endpointA), arcticModel(modelB, endpointB)} {
		if err := reg.Register(ctx, m, m.ModelID == modelA); err != nil {
			t.Fatal(err)
		}
		if err := emb.EnsureIndex(ctx, embeddings.SpecFor(m)); err != nil {
			t.Fatal(err)
		}
	}
	f.document(t, "doc-a", modelA, "Per-model vector indexes are rebuilt from canonical rows when the signature changes.")
	f.document(t, "doc-b", modelB, "Registering a candidate model starts dual-writing its vectors at ingest.")
	return f
}

// document creates an object and stores its recorded embedding under model.
func (f *queryPathFixture) document(t *testing.T, id, model, text string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	if err := f.drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: id, Type: "note", RawContent: text, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	m, err := f.reg.Get(ctx, model)
	if err != nil {
		t.Fatal(err)
	}
	p, err := embeddings.NewProviderResolver(f.r).ForModel(ctx, *m)
	if err != nil {
		t.Fatal(err)
	}
	vec, err := p.Embed(ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.emb.Put(ctx, id, []storage.ObjectVector{{ModelID: model, Vector: vec, Text: text}}); err != nil {
		t.Fatal(err)
	}
}

func (f *queryPathFixture) source() retrieval.SemanticSource {
	return retrieval.SemanticSource{Models: f.reg, Resolver: embeddings.NewProviderResolver(f.r)}
}

// lastCall returns the endpoint the most recent embedding request went to.
func (f *queryPathFixture) lastCall(t *testing.T) string {
	t.Helper()
	urls := f.calls.URLs()
	if len(urls) == 0 {
		t.Fatal("no embedding request was made")
	}
	return strings.TrimSuffix(urls[len(urls)-1], "/api/embeddings")
}

func ids(objs []*storage.KnowledgeObject) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.ID
	}
	return out
}

const query = "how are vector indexes kept in sync with their rows"

func TestSemanticSearch_UsesDefaultModel(t *testing.T) {
	f := newQueryPathFixture(t)
	objs, rep, err := f.source().Search(context.Background(), f.drv, query, storage.ObjectFilter{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() || rep.ModelID != modelA || rep.Notice != "" {
		t.Fatalf("report = %+v, want ok under %s with no notice", rep, modelA)
	}
	if got := f.lastCall(t); got != endpointA {
		t.Fatalf("query embedded at %s, want the default model's endpoint %s", got, endpointA)
	}
	searches := f.emb.Searches()
	if len(searches) == 0 || searches[0].ModelID != modelA {
		t.Fatalf("searched %+v, want the default model %s", searches, modelA)
	}
	if len(searches[0].Vector) != 1024 {
		t.Fatalf("query vector has %d dimensions, want 1024", len(searches[0].Vector))
	}
	if got := ids(objs); len(got) != 1 || got[0] != "doc-a" {
		t.Fatalf("hits = %v, want [doc-a] from %s's index", got, modelA)
	}
	hits, err := f.emb.Search(context.Background(), storage.VectorQuery{ModelID: modelA, Vector: searches[0].Vector, TopK: 1})
	if err != nil {
		t.Fatal(err)
	}
	if score := objs[0].Metadata["score"].(float64); math.Abs(score-(1-hits[0].Distance)) > 1e-9 {
		t.Fatalf("score = %v, want 1 - distance = %v", score, 1-hits[0].Distance)
	}
}

// A set-default flip through another registry handle applies to the very
// next query of an existing source: nothing caches the default.
func TestSemanticSearch_FollowsDefaultFlip(t *testing.T) {
	f := newQueryPathFixture(t)
	ctx := context.Background()
	src := f.source()

	if _, rep, err := src.Search(ctx, f.drv, query, storage.ObjectFilter{Limit: 5}); err != nil || rep.ModelID != modelA {
		t.Fatalf("first query: report %+v, err %v; want %s", rep, err, modelA)
	}

	other := registry.New(f.db) // e.g. `ctxt embeddings set-default` in another process
	if err := other.SetDefault(ctx, modelB); err != nil {
		t.Fatal(err)
	}

	objs, rep, err := src.Search(ctx, f.drv, query, storage.ObjectFilter{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() || rep.ModelID != modelB {
		t.Fatalf("report after flip = %+v, want ok under %s", rep, modelB)
	}
	if got := f.lastCall(t); got != endpointB {
		t.Fatalf("query embedded at %s after the flip, want %s's endpoint %s", got, modelB, endpointB)
	}
	searches := f.emb.Searches()
	if last := searches[len(searches)-1]; last.ModelID != modelB {
		t.Fatalf("searched %s after the flip, want %s", last.ModelID, modelB)
	}
	if got := ids(objs); len(got) != 1 || got[0] != "doc-b" {
		t.Fatalf("hits after flip = %v, want [doc-b] from %s's index", got, modelB)
	}
}

// A registered model's endpoint may be overridden per run (a tunnel); its
// backend and model are its identity and may not.
func TestSemanticSearch_OverridesChangeTransportOnly(t *testing.T) {
	ctx := context.Background()

	t.Run("endpoint", func(t *testing.T) {
		f := newQueryPathFixture(t)
		f.r.Flags = embeddings.Overrides{Endpoint: tunnel}
		objs, rep, err := f.source().Search(ctx, f.drv, query, storage.ObjectFilter{Limit: 5})
		if err != nil || !rep.OK() {
			t.Fatalf("report %+v, err %v; want ok", rep, err)
		}
		if got := f.lastCall(t); got != tunnel {
			t.Fatalf("query embedded at %s, want the overriding endpoint %s", got, tunnel)
		}
		if got := ids(objs); len(got) != 1 || got[0] != "doc-a" {
			t.Fatalf("hits = %v, want [doc-a]", got)
		}
	})

	for name, o := range map[string]embeddings.Overrides{
		"model":   {Model: "nomic-embed-text"},
		"backend": {Backend: "stub"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newQueryPathFixture(t)
			before := len(f.calls.URLs())
			f.r.Flags = o
			objs, rep, err := f.source().Search(ctx, f.drv, query, storage.ObjectFilter{Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Status != retrieval.SemanticProviderError || rep.ModelID != modelA || objs != nil {
				t.Fatalf("report %+v, hits %v; want provider_error under %s and no hits", rep, ids(objs), modelA)
			}
			if !strings.Contains(rep.Notice, "semantic search unavailable (provider_error)") {
				t.Fatalf("notice = %q", rep.Notice)
			}
			if len(f.calls.URLs()) != before || len(f.emb.Searches()) != 0 {
				t.Fatal("an identity override still embedded or searched")
			}
		})
	}
}

// Every reason the semantic leg cannot run is reported with a notice, never
// swallowed.
func TestSemanticSearch_UnavailableLegIsReported(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		setup func(t *testing.T, f *queryPathFixture)
		want  retrieval.SemanticStatus
	}{
		"no default model": {
			setup: func(t *testing.T, f *queryPathFixture) {
				if _, err := f.db.Exec(`UPDATE embedding_models SET is_default = 0`); err != nil {
					t.Fatal(err)
				}
			},
			want: retrieval.SemanticNoDefaultModel,
		},
		"index missing": {
			setup: func(t *testing.T, f *queryPathFixture) {
				if err := f.emb.PurgeModel(ctx, modelA); err != nil {
					t.Fatal(err)
				}
			},
			want: retrieval.SemanticIndexMissing,
		},
		"no coverage": {
			setup: func(t *testing.T, f *queryPathFixture) {
				if err := f.emb.Put(ctx, "doc-a", []storage.ObjectVector{{ModelID: modelA, Vector: make([]float32, 1024)}}); err != nil {
					t.Fatal(err)
				}
			},
			want: retrieval.SemanticLowCoverage,
		},
		"dimension mismatch": {
			setup: func(t *testing.T, f *queryPathFixture) {
				if _, err := f.db.Exec(`UPDATE embedding_models SET dimension = 768 WHERE model_id = ?`, modelA); err != nil {
					t.Fatal(err)
				}
			},
			want: retrieval.SemanticDimensionMismatch,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newQueryPathFixture(t)
			tc.setup(t, f)
			objs, rep, err := f.source().Search(ctx, f.drv, query, storage.ObjectFilter{Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Status != tc.want || len(objs) != 0 {
				t.Fatalf("report %+v, hits %v; want %s and no hits", rep, ids(objs), tc.want)
			}
			if !strings.HasPrefix(rep.Notice, "semantic search unavailable ("+string(tc.want)+"): ") ||
				!strings.HasSuffix(rep.Notice, "; results are full-text only") {
				t.Fatalf("notice = %q", rep.Notice)
			}
		})
	}
}

// The session blend hook sees the vector with the model it belongs to, and
// the search uses what it returns.
func TestSemanticSearch_BlendIsKeyedByModel(t *testing.T) {
	f := newQueryPathFixture(t)
	src := f.source()
	var gotModel string
	var gotLen int
	blended := make([]float32, 1024)
	blended[0] = 1
	src.Blend = func(modelID string, vec []float32) []float32 {
		gotModel, gotLen = modelID, len(vec)
		return blended
	}
	if _, rep, err := src.Search(context.Background(), f.drv, query, storage.ObjectFilter{Limit: 5}); err != nil || !rep.OK() {
		t.Fatalf("report %+v, err %v", rep, err)
	}
	if gotModel != modelA || gotLen != 1024 {
		t.Fatalf("Blend got model %q with %d dims, want %s with 1024", gotModel, gotLen, modelA)
	}
	if s := f.emb.Searches(); s[0].Vector[0] != 1 || s[0].Vector[1] != 0 {
		t.Fatal("search did not use the blended vector")
	}
}

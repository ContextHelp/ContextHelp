//go:build integration

// Package integration holds the 100k object stress test + search benchmarks for T-0103.
//
// Run the full simulation (slow, ~minutes):
//
//	INTEGRATION=1 CGO_ENABLED=1 go test -tags fts5 -run TestSimulation_100k -v \
//	    -timeout 600s ./test/integration/
//
// Run benchmarks (set -benchtime higher for large sets):
//
//	INTEGRATION=1 CGO_ENABLED=1 go test -tags fts5 \
//	    -bench=BenchmarkFTS_100k|BenchmarkVectorSearch_100k|BenchmarkHybridSearch_100k \
//	    -benchtime=120s -benchmem -timeout 600s ./test/integration/
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/stretchr/testify/require"
)

// simDim matches the production embedding dimension.
const simDim = 1536

// simObjectCount is the total object count for the simulation.
const simObjectCount = 100_000

// simImageCount is the count of "image" type objects within the corpus.
const simImageCount = 10_000

// simEntityCount is the number of synthetic entities across namespaces.
const simEntityCount = 5_000

// simNamespaces are entity namespace prefixes used in slug generation.
var simNamespaces = []string{"ui", "api", "infra", "security", "data", "ml", "product"}

// simObjectTypes are the Knowledge Object type values cycled through the corpus.
var simObjectTypes = []string{"text", "url", "image"}

// simTags is a fixed vocabulary used for tag assignment and FTS query generation.
var simTags = []string{
	"research", "design", "backend", "frontend", "devops",
	"architecture", "performance", "security", "testing", "documentation",
}

// checkIntegration skips the calling test when the INTEGRATION env var is absent.
func checkIntegration(t testing.TB) {
	t.Helper()
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run 100k simulation tests")
	}
}

// simModelID is the registered default model the corpus is embedded under.
const simModelID = "sim-1536"

// newSimDriver creates a fresh SQLite driver in a temp dir with simModelID
// registered as the default at simDim and indexed. It skips until the
// driver's per-model EmbeddingStore is implemented.
func newSimDriver(t testing.TB) *sqlite.Driver {
	t.Helper()
	dir := t.TempDir()
	d, err := sqlite.New(filepath.Join(dir, "sim.db"))
	if err != nil {
		t.Fatalf("new sim driver: %v", err)
	}
	ctx := context.Background()
	if err := d.Init(ctx); err != nil {
		t.Fatalf("init sim driver: %v", err)
	}
	t.Cleanup(func() { d.Close(context.Background()) })
	m := registry.Model{ModelID: simModelID, Provider: "fixture", Dimension: simDim, ConfigJSON: "{}"}
	if err := registry.New(d.DB()).Register(ctx, m, true); err != nil {
		t.Fatalf("register sim model: %v", err)
	}
	err = d.Embeddings().EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: m.ModelID, Provider: m.Provider, Dimension: simDim})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip("EmbeddingStore not implemented by the driver yet (per-model index)")
	}
	if err != nil {
		t.Fatalf("index sim model: %v", err)
	}
	return d
}

// randVec returns a random float32 slice of length dim in the range [-1, 1].
func randVec(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rng.Float32()*2 - 1
	}
	return v
}

// makeSimObject generates one KnowledgeObject for the simulation corpus.
func makeSimObject(rng *rand.Rand, i int, entitySlugs []string) *storage.KnowledgeObject {
	now := time.Now()

	objType := simObjectTypes[i%len(simObjectTypes)]
	// Last 10k objects are image type to reach the spec target.
	if i >= simObjectCount-simImageCount {
		objType = "image"
	}

	// Pick 1-3 tags from the fixed vocabulary.
	nTags := 1 + rng.Intn(3)
	tags := make([]storage.Tag, nTags)
	for j := range tags {
		tags[j] = storage.Tag{Label: simTags[rng.Intn(len(simTags))]}
	}

	topic := simTags[i%len(simTags)]
	summary := fmt.Sprintf("Synthetic knowledge object %d type=%s topic=%s for stress testing", i, objType, topic)

	return &storage.KnowledgeObject{
		ID:          fmt.Sprintf("sim-%07d", i),
		Type:        objType,
		RawContent:  fmt.Sprintf("raw content for object %d: %s", i, summary),
		TextContent: summary,
		Summaries:   []string{summary},
		Tags:        tags,
		CreatedAt:   now,
		UpdatedAt:   now,
		Status:      "active",
	}
}

// seedEntities inserts simEntityCount synthetic entities and returns their slugs.
func seedEntities(t testing.TB, d *sqlite.Driver) []string {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	slugs := make([]string, simEntityCount)
	for i := 0; i < simEntityCount; i++ {
		ns := simNamespaces[i%len(simNamespaces)]
		slug := fmt.Sprintf("%s.entity-%04d", ns, i)
		slugs[i] = slug
		if err := d.Entities().Upsert(ctx, &storage.Entity{
			Slug:      slug,
			Title:     fmt.Sprintf("Entity %d in %s", i, ns),
			Namespace: ns,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatalf("upsert entity %d: %v", i, err)
		}
	}
	return slugs
}

// ingestObjects creates count objects in the driver and returns wall time elapsed.
func ingestObjects(t testing.TB, d *sqlite.Driver, count int, entitySlugs []string) time.Duration {
	t.Helper()
	ctx := context.Background()
	rng := rand.New(rand.NewSource(42)) //nolint:gosec
	start := time.Now()
	for i := 0; i < count; i++ {
		obj := makeSimObject(rng, i, entitySlugs)
		if err := d.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("create object %d: %v", i, err)
		}
		vec := []storage.ObjectVector{{ModelID: simModelID, Vector: randVec(rng, simDim)}}
		if err := d.Embeddings().Put(ctx, obj.ID, vec); err != nil {
			t.Fatalf("put vector %d: %v", i, err)
		}
	}
	return time.Since(start)
}

// rebuildFTS triggers an FTS5 content-table rebuild.
func rebuildFTS(t testing.TB, d *sqlite.Driver) {
	t.Helper()
	_, err := d.DB().ExecContext(context.Background(),
		"INSERT INTO objects_fts(objects_fts) VALUES('rebuild')")
	if err != nil {
		t.Fatalf("fts rebuild: %v", err)
	}
}

// memStats returns a concise Go heap snapshot string.
func memStats() string {
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	return fmt.Sprintf("heap_alloc=%.1fMB heap_sys=%.1fMB heap_objects=%d",
		float64(ms.HeapAlloc)/(1<<20),
		float64(ms.HeapSys)/(1<<20),
		ms.HeapObjects,
	)
}

// fixedVecProvider is an EmbeddingProvider that always returns the same vector.
// Used in benchmarks to eliminate embedding latency from search measurements.
type fixedVecProvider struct {
	vec []float32
}

func (p *fixedVecProvider) Name() string    { return "fixed-sim" }
func (p *fixedVecProvider) Dimensions() int { return len(p.vec) }
func (p *fixedVecProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return p.vec, nil
}

var _ providers.EmbeddingProvider = (*fixedVecProvider)(nil)

// fixedVecResolver resolves every model to a fixedVecProvider.
type fixedVecResolver struct{ vec []float32 }

func (r fixedVecResolver) ForModel(context.Context, registry.Model) (providers.EmbeddingProvider, error) {
	return &fixedVecProvider{vec: r.vec}, nil
}

func (r fixedVecResolver) ForRegistration(context.Context) (providers.EmbeddingProvider, json.RawMessage, error) {
	return &fixedVecProvider{vec: r.vec}, nil, nil
}

// simSemantic reads the sim driver's default model and embeds every query
// as queryVec.
func simSemantic(d *sqlite.Driver, queryVec []float32) retrieval.SemanticSource {
	return retrieval.SemanticSource{Models: registry.New(d.DB()), Resolver: fixedVecResolver{vec: queryVec}}
}

// TestSimulation_100k is the primary correctness + scale test.
// Ingests 100k objects and verifies all three search paths return results.
func TestSimulation_100k(t *testing.T) {
	checkIntegration(t)

	d := newSimDriver(t)
	ctx := context.Background()

	t.Log("seeding entities...")
	entitySlugs := seedEntities(t, d)
	t.Logf("seeded %d entities; %s", simEntityCount, memStats())

	t.Logf("ingesting %d objects (this may take several minutes)...", simObjectCount)
	dur := ingestObjects(t, d, simObjectCount, entitySlugs)
	t.Logf("ingestion complete: elapsed=%s throughput=%.0f obj/s; %s",
		dur.Round(time.Second),
		float64(simObjectCount)/dur.Seconds(),
		memStats(),
	)

	t.Log("rebuilding FTS index...")
	rebuildFTS(t, d)
	t.Logf("FTS rebuild done; %s", memStats())

	// Verify object count.
	_, total, err := d.Objects().List(ctx, storage.ObjectFilter{Status: "all", Limit: 1})
	require.NoError(t, err)
	require.Equal(t, simObjectCount, total, "total object count")

	// Every object carries a vector under the default model.
	missing, err := d.Embeddings().ListMissing(ctx, simModelID, "", 1)
	require.NoError(t, err)
	require.Empty(t, missing, "objects without a %s vector", simModelID)
	t.Logf("%s coverage complete; %s", simModelID, memStats())

	rng := rand.New(rand.NewSource(99)) //nolint:gosec
	queryVec := randVec(rng, simDim)
	ftsQuery := simTags[rng.Intn(len(simTags))]

	// --- FTS search ---
	t.Log("running FTS search...")
	ftsStart := time.Now()
	ftsResults, err := d.Objects().FTSSearch(ctx, ftsQuery, storage.ObjectFilter{Limit: 20})
	require.NoError(t, err)
	t.Logf("FTS search %q → %d results in %s", ftsQuery, len(ftsResults), time.Since(ftsStart))
	require.NotEmpty(t, ftsResults, "FTS should return results for %q", ftsQuery)

	// --- ANN vector search (EmbeddingStore) ---
	t.Log("running ANN vector search (EmbeddingStore)...")
	annStart := time.Now()
	annHits, err := d.Embeddings().Search(ctx, storage.VectorQuery{ModelID: simModelID, Vector: queryVec, TopK: 20})
	require.NoError(t, err)
	t.Logf("ANN search → %d hits in %s", len(annHits), time.Since(annStart))
	require.NotEmpty(t, annHits, "ANN search should return results")

	// --- ObjectStore.VectorSearch (should delegate to ANN) ---
	t.Log("running ObjectStore.VectorSearch (ANN delegation)...")
	objVecStart := time.Now()
	objVecResults, err := d.Objects().VectorSearch(ctx, storage.VectorQuery{ModelID: simModelID, Vector: queryVec}, storage.ObjectFilter{Limit: 20})
	require.NoError(t, err)
	t.Logf("ObjectStore.VectorSearch → %d results in %s", len(objVecResults), time.Since(objVecStart))
	require.NotEmpty(t, objVecResults, "ObjectStore.VectorSearch should return results")

	// --- Hybrid search (FTS + ANN via service) ---
	t.Log("running hybrid search...")
	eng := search.NewEngine(d)
	svc := service.New(d, nil, nil, eng, "", nil)
	sem := simSemantic(d, queryVec)
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 50, Vector: 50},
		FallbackToFTS: true,
	}
	hybridStart := time.Now()
	hybridResults, err := svc.HybridSearch(ctx, ftsQuery, 20, sem, cfg)
	require.NoError(t, err)
	t.Logf("hybrid search %q → %d results in %s", ftsQuery, len(hybridResults), time.Since(hybridStart))
	require.NotEmpty(t, hybridResults, "hybrid search should return results")

	t.Logf("final memory: %s", memStats())
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkFTS_100k benchmarks FTS5 full-text search over 100k objects.
// Run with: INTEGRATION=1 CGO_ENABLED=1 go test -tags fts5 -bench=BenchmarkFTS_100k -benchtime=120s ./test/integration/
func BenchmarkFTS_100k(b *testing.B) {
	checkIntegration(b)
	d := newSimDriver(b)
	slugs := seedEntities(b, d)
	ingestObjects(b, d, simObjectCount, slugs)
	rebuildFTS(b, d)

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		q := simTags[i%len(simTags)]
		hits, err := d.Objects().FTSSearch(ctx, q, storage.ObjectFilter{Limit: 20})
		if err != nil {
			b.Fatalf("fts search: %v", err)
		}
		_ = hits
	}
}

// BenchmarkVectorSearch_100k benchmarks sqlite-vec ANN KNN search over 100k vectors.
// Run with: INTEGRATION=1 CGO_ENABLED=1 go test -tags fts5 -bench=BenchmarkVectorSearch_100k -benchtime=120s ./test/integration/
func BenchmarkVectorSearch_100k(b *testing.B) {
	checkIntegration(b)
	d := newSimDriver(b)
	slugs := seedEntities(b, d)
	ingestObjects(b, d, simObjectCount, slugs)

	ctx := context.Background()
	rng := rand.New(rand.NewSource(7)) //nolint:gosec
	queryVec := randVec(rng, simDim)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		hits, err := d.Embeddings().Search(ctx, storage.VectorQuery{ModelID: simModelID, Vector: queryVec, TopK: 20})
		if err != nil {
			b.Fatalf("vector search: %v", err)
		}
		_ = hits
	}
}

// BenchmarkHybridSearch_100k benchmarks hybrid RRF (FTS + ANN) search over 100k objects.
// Run with: INTEGRATION=1 CGO_ENABLED=1 go test -tags fts5 -bench=BenchmarkHybridSearch_100k -benchtime=120s ./test/integration/
func BenchmarkHybridSearch_100k(b *testing.B) {
	checkIntegration(b)
	d := newSimDriver(b)
	slugs := seedEntities(b, d)
	ingestObjects(b, d, simObjectCount, slugs)
	rebuildFTS(b, d)

	rng := rand.New(rand.NewSource(13)) //nolint:gosec
	queryVec := randVec(rng, simDim)
	cfg := config.SearchConfig{
		DefaultMode:   "hybrid",
		RRF:           config.RRFConfig{K: 60, FTSWeight: 0.5, VectorWeight: 0.5},
		CandidatePool: config.CandidatePoolConfig{FTS: 50, Vector: 50},
		FallbackToFTS: true,
	}
	eng := search.NewEngine(d)
	svc := service.New(d, nil, nil, eng, "", nil)
	sem := simSemantic(d, queryVec)

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		q := simTags[i%len(simTags)]
		results, err := svc.HybridSearch(ctx, q, 20, sem, cfg)
		if err != nil {
			b.Fatalf("hybrid search: %v", err)
		}
		_ = results
	}
}

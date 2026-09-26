package service

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Ollama calls replay cassettes recorded against a real Ollama serving
// snowflake-arctic-embed2 (1024 dimensions). Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run Reanalyze ./internal/service/
const reanalyzeCassettes = "testdata/cassettes/reanalyze-ollama"

const reanalyzeSnowflakeConfig = `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://127.0.0.1:11434"}`

var (
	reDefault   = registry.Model{ModelID: "snowflake-arctic-embed2@default", Provider: "ollama", Dimension: 1024, IsDefault: true, ConfigJSON: reanalyzeSnowflakeConfig}
	reCandidate = registry.Model{ModelID: "snowflake-arctic-embed2@candidate", Provider: "ollama", Dimension: 1024, ConfigJSON: reanalyzeSnowflakeConfig}
	// reOther is a registered model outside the populate set (deprecated):
	// reanalyze must leave its rows alone.
	reOther = registry.Model{ModelID: "other@1", Provider: "stub", Dimension: 3}
)

type reanalyzeDriver struct {
	storage.StorageDriver
	emb storage.EmbeddingStore
}

func (d *reanalyzeDriver) Embeddings() storage.EmbeddingStore { return d.emb }

// reanalyzeService wires a Service whose pipelines embed with models and
// whose driver persists vectors through store. Objects stay on SQLite.
func reanalyzeService(t *testing.T, driver storage.StorageDriver, models embeddings.ModelSource, store storage.EmbeddingStore) *Service {
	t.Helper()
	client, _ := providertest.OllamaClient(t, reanalyzeCassettes)
	pipes := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{
		Models: models,
		Resolver: embeddings.NewProviderResolver(&embeddings.Resolver{
			LookupEnv:  func(string) (string, bool) { return "", false },
			HTTPClient: client,
		}),
		Embeddings: store,
	})
	d := &reanalyzeDriver{StorageDriver: driver, emb: store}
	return New(d, jobs.NewQueue(driver.Jobs()), pipes, search.NewEngine(driver), "", nil)
}

func seedReanalyzeObject(t *testing.T, driver storage.StorageDriver, id string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	if err := driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: "numbat sightings near the dryandra woodland",
		Pipeline:   "text.long@v0",
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func constVector(dim int, v float32) []float32 {
	out := make([]float32, dim)
	for i := range out {
		out[i] = v
	}
	return out
}

func putStale(t *testing.T, store storage.EmbeddingStore, id string) {
	t.Helper()
	ctx := context.Background()
	for _, m := range []registry.Model{reDefault, reOther} {
		if err := store.Put(ctx, id, []storage.ObjectVector{{ModelID: m.ModelID, Vector: constVector(m.Dimension, 0.5)}}); err != nil {
			t.Fatalf("seed stale %s: %v", m.ModelID, err)
		}
	}
}

func rowsFor(t *testing.T, store storage.EmbeddingStore, id, modelID string) []storage.ObjectVector {
	t.Helper()
	rows, err := store.Get(context.Background(), id, modelID)
	if err != nil {
		t.Fatalf("Get(%s, %s): %v", id, modelID, err)
	}
	return rows
}

func isConst(v []float32, c float32) bool {
	for _, x := range v {
		if x != c {
			return false
		}
	}
	return len(v) > 0
}

// assertReplaced checks that reanalyze replaced the default model's stale
// row, added the candidate's, and left the other model's row untouched.
func assertReplaced(t *testing.T, store storage.EmbeddingStore, id string) {
	t.Helper()
	def := rowsFor(t, store, id, reDefault.ModelID)
	if len(def) != 1 || len(def[0].Vector) != 1024 || isConst(def[0].Vector, 0.5) {
		t.Errorf("default model: %d rows (stale=%v), want one fresh 1024-dim row", len(def), len(def) == 1 && isConst(def[0].Vector, 0.5))
	}
	if cand := rowsFor(t, store, id, reCandidate.ModelID); len(cand) != 1 {
		t.Errorf("candidate model: %d rows, want 1", len(cand))
	}
	if other := rowsFor(t, store, id, reOther.ModelID); len(other) != 1 || !isConst(other[0].Vector, 0.5) {
		t.Errorf("reanalyze touched a model outside the populate set: %+v", other)
	}
}

func TestReanalyzeObject_ReplacesVectors(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	store := embeddingtest.NewMemStore()
	for _, m := range []registry.Model{reDefault, reCandidate, reOther} {
		if err := store.EnsureIndex(context.Background(), embeddings.SpecFor(m)); err != nil {
			t.Fatal(err)
		}
	}
	seedReanalyzeObject(t, driver, "re-1")
	putStale(t, store, "re-1")

	svc := reanalyzeService(t, driver, embeddingtest.Models{reDefault, reCandidate}, store)
	if _, _, err := svc.ReanalyzeObject(context.Background(), "re-1"); err != nil {
		t.Fatalf("reanalyze: %v", err)
	}
	assertReplaced(t, store, "re-1")

	obj, err := driver.Objects().Get(context.Background(), "re-1")
	if err != nil {
		t.Fatal(err)
	}
	if !obj.VectorIndexed || len(obj.Embeddings) != 0 {
		t.Errorf("vector_indexed=%v legacy embeddings=%d, want indexed through the per-model path only", obj.VectorIndexed, len(obj.Embeddings))
	}
}

func TestReanalyzeObject_PutFailureIsNotAnError(t *testing.T) {
	var logs lockedLog
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	driver := storageutil.NewTestDriver(t)
	store := embeddingtest.NewMemStore()
	store.PutErr = fmt.Errorf("model %s: %w", reDefault.ModelID, storage.ErrEmbeddingIndexMissing)
	seedReanalyzeObject(t, driver, "re-2")

	svc := reanalyzeService(t, driver, embeddingtest.Models{reDefault}, store)
	if _, _, err := svc.ReanalyzeObject(context.Background(), "re-2"); err != nil {
		t.Fatalf("a failed vector write failed reanalyze: %v", err)
	}
	if store.Puts() != 1 {
		t.Errorf("Put called %d times, want 1", store.Puts())
	}
	if out := logs.String(); !strings.Contains(out, "object=re-2") || !strings.Contains(out, storage.ErrEmbeddingIndexMissing.Error()) {
		t.Errorf("Put failure not logged with object and cause; logs:\n%s", out)
	}
}

// End to end against the driver's real EmbeddingStore. Skipped while the
// driver's store is the contract stub.
func TestReanalyzeObject_ReplacesVectorsSQLite(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	reg, err := registry.ForDriver(driver)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, m := range []registry.Model{reDefault, reCandidate, reOther} {
		if err := reg.Register(ctx, m, m.IsDefault); err != nil {
			t.Fatalf("register %s: %v", m.ModelID, err)
		}
	}
	if err := reg.Deprecate(ctx, reOther.ModelID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	store := driver.Embeddings()
	embeddingtest.RequireEmbeddingStore(t, store,
		embeddings.SpecFor(reDefault), embeddings.SpecFor(reCandidate), embeddings.SpecFor(reOther))

	seedReanalyzeObject(t, driver, "re-3")
	putStale(t, store, "re-3")

	svc := reanalyzeService(t, driver, reg, store)
	if _, _, err := svc.ReanalyzeObject(ctx, "re-3"); err != nil {
		t.Fatalf("reanalyze: %v", err)
	}
	assertReplaced(t, store, "re-3")
}

type lockedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

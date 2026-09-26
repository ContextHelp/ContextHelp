package jobs

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Ollama calls replay cassettes recorded against a real Ollama serving
// snowflake-arctic-embed2 (1024 dimensions). Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run Embedding ./internal/jobs/
const embeddingCassettes = "testdata/cassettes/embedding-ollama"

// numbatBody runs through text.long, whose sectioner gives the object
// embedding text without an LLM.
const numbatBody = "numbat sightings near the dryandra woodland"

const snowflakeConfig = `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://127.0.0.1:11434"}`

var (
	snowDefault   = registry.Model{ModelID: "snowflake-arctic-embed2@default", Provider: "ollama", Dimension: 1024, IsDefault: true, ConfigJSON: snowflakeConfig}
	snowCandidate = registry.Model{ModelID: "snowflake-arctic-embed2@candidate", Provider: "ollama", Dimension: 1024, ConfigJSON: snowflakeConfig}
	// notPulled names a model the recording Ollama lacks: its embed call
	// fails for real ("model not found").
	notPulled = registry.Model{
		ModelID: "not-pulled-embed@1", Provider: "ollama", Dimension: 1024,
		ConfigJSON: `{"backend":"ollama","model":"ctxt-test-not-pulled-embed","endpoint":"http://127.0.0.1:11434"}`,
	}
)

// embeddingsDriver swaps a real driver's EmbeddingStore for a test double
// while objects, jobs and the registry stay on the real SQLite database.
type embeddingsDriver struct {
	storage.StorageDriver
	emb storage.EmbeddingStore
}

func (d *embeddingsDriver) Embeddings() storage.EmbeddingStore { return d.emb }

func ollamaResolver(t *testing.T) embeddings.ProviderResolver {
	t.Helper()
	client, _ := providertest.OllamaClient(t, embeddingCassettes)
	return embeddings.NewProviderResolver(&embeddings.Resolver{
		LookupEnv:  func(string) (string, bool) { return "", false },
		HTTPClient: client,
	})
}

func indexed(t *testing.T, s storage.EmbeddingStore, models ...registry.Model) {
	t.Helper()
	for _, m := range models {
		if err := s.EnsureIndex(context.Background(), embeddings.SpecFor(m)); err != nil {
			t.Fatalf("EnsureIndex(%s): %v", m.ModelID, err)
		}
	}
}

func captureSlog(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// ingest runs one text.long job through a pool whose pipelines embed with
// models and whose driver persists through store.
func ingest(t *testing.T, driver storage.StorageDriver, models embeddings.ModelSource, store storage.EmbeddingStore, id, body string) *storage.Job {
	t.Helper()
	d := &embeddingsDriver{StorageDriver: driver, emb: store}
	pipes := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{
		Models:     models,
		Resolver:   ollamaResolver(t),
		Embeddings: store,
	})
	q := NewQueue(d.Jobs())
	job := makeJob(id)
	job.Payload = body
	job.Pipeline = "text.long"
	if err := q.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return runUntilDone(t, NewWorkerPool(q, pipes, d, 1, nil, defaultTestJobsCfg()), q, id)
}

func requireCompleted(t *testing.T, job *storage.Job) {
	t.Helper()
	if job.Status != storage.JobCompleted {
		t.Fatalf("job %s: status %q (err=%q), want completed", job.ID, job.Status, job.Error)
	}
}

func TestWorker_WritesVectorsForEveryPopulatingModel(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	store := embeddingtest.NewMemStore()
	indexed(t, store, snowDefault, snowCandidate)

	job := ingest(t, driver, embeddingtest.Models{snowDefault, snowCandidate}, store, "job-emb-two", numbatBody)
	requireCompleted(t, job)

	for _, m := range []registry.Model{snowDefault, snowCandidate} {
		rows, err := store.Get(context.Background(), job.ResultID, m.ModelID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].ChunkIdx != 0 || len(rows[0].Vector) != 1024 {
			t.Errorf("%s: rows %d, want one 1024-dim chunk-0 row for object %s", m.ModelID, len(rows), job.ResultID)
		}
	}
}

func TestWorker_FailingModelStillCompletes(t *testing.T) {
	logs := captureSlog(t)
	driver := storageutil.NewTestDriver(t)
	store := embeddingtest.NewMemStore()
	indexed(t, store, snowDefault, notPulled)

	job := ingest(t, driver, embeddingtest.Models{snowDefault, notPulled}, store, "job-emb-fail", numbatBody)
	requireCompleted(t, job)

	if got := store.ObjectIDs(snowDefault.ModelID); len(got) != 1 || got[0] != job.ResultID {
		t.Errorf("default model rows for %v, want [%s]", got, job.ResultID)
	}
	if got := store.ObjectIDs(notPulled.ModelID); len(got) != 0 {
		t.Errorf("failing model has rows for %v, want none (the missing row is the backfill's record)", got)
	}
	if !strings.Contains(logs.String(), "model_id="+notPulled.ModelID) {
		t.Errorf("failure not logged with its model_id; logs:\n%s", logs.String())
	}
}

// A Put failure (here a dimension mismatch the store rejects) is logged and
// never fails the job.
func TestWorker_PutFailureDoesNotFailJob(t *testing.T) {
	logs := captureSlog(t)
	driver := storageutil.NewTestDriver(t)
	store := embeddingtest.NewMemStore()
	indexed(t, store, snowDefault)
	store.PutErr = fmt.Errorf("model %s: %w", snowDefault.ModelID, storage.ErrEmbeddingDimension)

	job := ingest(t, driver, embeddingtest.Models{snowDefault}, store, "job-emb-putfail", numbatBody)
	requireCompleted(t, job)
	if store.Puts() != 1 {
		t.Fatalf("Put called %d times, want 1", store.Puts())
	}
	out := logs.String()
	if !strings.Contains(out, "object="+job.ResultID) || !strings.Contains(out, storage.ErrEmbeddingDimension.Error()) {
		t.Errorf("Put failure not logged with object and cause; logs:\n%s", out)
	}
}

// A duplicate capture reinforces the existing object; its vectors are
// written under that object's ID, replacing the earlier rows.
func TestWorker_ReinforceWritesVectorsUnderExistingID(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	store := embeddingtest.NewMemStore()
	indexed(t, store, snowDefault)
	models := embeddingtest.Models{snowDefault}

	first := ingest(t, driver, models, store, "job-emb-first", numbatBody)
	requireCompleted(t, first)
	dup := ingest(t, driver, models, store, "job-emb-dup", numbatBody)
	requireCompleted(t, dup)

	if dup.ResultID != first.ResultID {
		t.Fatalf("duplicate result %s, want reinforced object %s", dup.ResultID, first.ResultID)
	}
	if store.Puts() != 2 {
		t.Errorf("Put called %d times, want once per ingest (2)", store.Puts())
	}
	if got := store.ObjectIDs(snowDefault.ModelID); len(got) != 1 || got[0] != first.ResultID {
		t.Errorf("rows for %v, want only the reinforced object %s", got, first.ResultID)
	}
}

func TestWorker_NoVectorsNoPut(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	store := embeddingtest.NewMemStore()

	job := ingest(t, driver, embeddingtest.Models{}, store, "job-emb-none", numbatBody)
	requireCompleted(t, job)
	if store.Puts() != 0 {
		t.Errorf("Put called %d times with no vectors", store.Puts())
	}
}

// End to end against the driver's real EmbeddingStore and registry: two
// registered models, one failing. Skipped while the driver's store is the
// contract stub.
func TestWorker_EmbeddingsEndToEndSQLite(t *testing.T) {
	logs := captureSlog(t)
	driver := storageutil.NewTestDriver(t)
	reg, err := registry.ForDriver(driver)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, m := range []registry.Model{snowDefault, snowCandidate, notPulled} {
		if err := reg.Register(ctx, m, m.IsDefault); err != nil {
			t.Fatalf("register %s: %v", m.ModelID, err)
		}
	}
	store := driver.Embeddings()
	embeddingtest.RequireEmbeddingStore(t, store,
		embeddings.SpecFor(snowDefault), embeddings.SpecFor(snowCandidate), embeddings.SpecFor(notPulled))

	job := ingest(t, driver, reg, store, "job-emb-e2e", numbatBody)
	requireCompleted(t, job)

	for _, m := range []registry.Model{snowDefault, snowCandidate} {
		rows, err := store.Get(ctx, job.ResultID, m.ModelID)
		if err != nil {
			t.Fatalf("Get(%s): %v", m.ModelID, err)
		}
		if len(rows) != 1 || len(rows[0].Vector) != 1024 {
			t.Errorf("%s: %d rows, want one 1024-dim row", m.ModelID, len(rows))
		}
	}
	missing, err := store.ListMissing(ctx, notPulled.ModelID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != job.ResultID {
		t.Errorf("ListMissing(%s) = %v, want the object the failed model skipped", notPulled.ModelID, missing)
	}
	hits, err := store.Search(ctx, storage.VectorQuery{ModelID: snowDefault.ModelID, Vector: mustGet(t, store, job.ResultID, snowDefault.ModelID), TopK: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ObjectID != job.ResultID {
		t.Errorf("Search = %+v, want the ingested object first", hits)
	}
	if !strings.Contains(logs.String(), "model_id="+notPulled.ModelID) {
		t.Errorf("failing model not logged; logs:\n%s", logs.String())
	}
}

func mustGet(t *testing.T, s storage.EmbeddingStore, objectID, modelID string) []float32 {
	t.Helper()
	rows, err := s.Get(context.Background(), objectID, modelID)
	if err != nil || len(rows) == 0 {
		t.Fatalf("Get(%s, %s): %v rows=%d", objectID, modelID, err, len(rows))
	}
	return rows[0].Vector
}

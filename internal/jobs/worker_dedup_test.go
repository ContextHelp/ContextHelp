package jobs

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// numbatNearDup is a near-duplicate of numbatBody: different content (so a
// different content hash, no reinforce) whose snowflake-arctic-embed2
// vector sits above dedupThreshold from numbatBody's.
const numbatNearDup = "Numbat sightings near the Dryandra Woodland."

const dedupThreshold = 0.9

// dedupEnv is a real SQLite driver whose default embedding model is
// snowDefault, with dedup injected under policy.
type dedupEnv struct {
	driver storage.StorageDriver
	store  storage.EmbeddingStore
	q      *Queue
	pool   *WorkerPool
}

func newDedupEnv(t *testing.T, policy string) *dedupEnv {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	reg, err := registry.ForDriver(driver)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(context.Background(), snowDefault, true); err != nil {
		t.Fatalf("register %s: %v", snowDefault.ModelID, err)
	}
	store := driver.Embeddings()
	embeddingtest.RequireEmbeddingStore(t, store, embeddings.SpecFor(snowDefault))

	pipes := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{
		Models:     reg,
		Resolver:   ollamaResolver(t),
		Embeddings: store,
		Duplicates: config.DuplicatesConfig{
			Policy:              policy,
			SimilarityThreshold: dedupThreshold,
			CheckExact:          true,
			CheckSimilar:        true,
		},
	})
	q := NewQueue(driver.Jobs())
	return &dedupEnv{
		driver: driver,
		store:  store,
		q:      q,
		pool:   NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg()),
	}
}

func (e *dedupEnv) ingest(t *testing.T, id, body string) *storage.Job {
	t.Helper()
	job := makeJob(id)
	job.Payload = body
	job.Pipeline = "text.long"
	if err := e.q.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	got := runUntilDone(t, e.pool, e.q, id)
	requireCompleted(t, got)
	return got
}

func (e *dedupEnv) objectIDs(t *testing.T) []string {
	t.Helper()
	objs, _, err := e.driver.Objects().List(context.Background(), storage.ObjectFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(objs))
	for _, o := range objs {
		ids = append(ids, o.ID)
	}
	return ids
}

// Under drop, a near-duplicate is not stored: the job completes with the
// existing object's ID, as analyze answers an exact duplicate, and no
// object, vector or edge is written for it.
func TestWorker_DedupDropDoesNotStoreNearDuplicate(t *testing.T) {
	logs := captureSlog(t)
	e := newDedupEnv(t, "drop")

	first := e.ingest(t, "job-dedup-first", numbatBody)
	dup := e.ingest(t, "job-dedup-near", numbatNearDup)

	if dup.ResultID != first.ResultID {
		t.Errorf("near-duplicate job result %q, want the existing object %q", dup.ResultID, first.ResultID)
	}
	if ids := e.objectIDs(t); len(ids) != 1 || ids[0] != first.ResultID {
		t.Errorf("objects %v, want only the first object %s", ids, first.ResultID)
	}
	missing, err := e.store.ListMissing(context.Background(), snowDefault.ModelID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Errorf("objects without a vector: %v, want none", missing)
	}
	if !strings.Contains(logs.String(), "duplicate_of="+first.ResultID) {
		t.Errorf("drop not logged with duplicate_of; logs:\n%s", logs.String())
	}
}

// warn and keep store the near-duplicate as a new object and record the
// match on its metadata; only warn logs it.
func TestWorker_DedupWarnAndKeepStoreNearDuplicate(t *testing.T) {
	for _, policy := range []string{"warn", "keep"} {
		t.Run(policy, func(t *testing.T) {
			logs := captureSlog(t)
			e := newDedupEnv(t, policy)

			first := e.ingest(t, "job-"+policy+"-first", numbatBody)
			dup := e.ingest(t, "job-"+policy+"-near", numbatNearDup)

			if dup.ResultID == "" || dup.ResultID == first.ResultID {
				t.Fatalf("near-duplicate result %q, want a new object (first %q)", dup.ResultID, first.ResultID)
			}
			if ids := e.objectIDs(t); len(ids) != 2 {
				t.Errorf("objects %v, want both stored", ids)
			}
			obj, err := e.driver.Objects().Get(context.Background(), dup.ResultID)
			if err != nil {
				t.Fatal(err)
			}
			if obj.Metadata["duplicate_of"] != first.ResultID || obj.Metadata["duplicate_kind"] != "similar" {
				t.Errorf("metadata %v, want duplicate_of=%s kind=similar", obj.Metadata, first.ResultID)
			}
			if _, set := obj.Metadata["suppress_output"]; set {
				t.Errorf("suppress_output set under %s", policy)
			}
			warned := strings.Contains(logs.String(), "near-duplicate detected")
			if warned != (policy == "warn") {
				t.Errorf("warning logged = %v under %s; logs:\n%s", warned, policy, logs.String())
			}
		})
	}
}

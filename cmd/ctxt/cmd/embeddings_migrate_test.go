package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/migrate"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
	"hop.top/kit/go/console/output"
)

// Cassettes under testdata/cassettes/embeddings-migrate were recorded
// against a real local Ollama with snowflake-arctic-embed2 (1024
// dimensions) and nomic-embed-text (768) pulled. They key on the exact
// embedding text, so a change to how ingest derives it needs a re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run TestEmbeddingsMigrate ./cmd/ctxt/cmd/
const migrateCassettes = "testdata/cassettes/embeddings-migrate"

const (
	migrateFromID = "ollama-snowflake-arctic-embed2@2026-09-26"
	migrateToID   = "ollama-nomic-embed-text@2026-09-26"
)

// migrateBodies run through text.long, which embeds without an LLM.
var migrateBodies = []string{
	"numbat sightings near the dryandra woodland",
	"quokka colonies on rottnest island",
	"bilby burrows in the pilbara",
	"dugong feeding grounds in shark bay",
}

// digitTable exceeds nomic-embed-text's context even after the provider's
// 8192-byte cut: Ollama rejects it for real.
var digitTable = strings.Repeat("0 1 2 3 4 5 6 7 8 9 ", 500)

type migrateDoc struct {
	ModelID   string  `json:"model_id"`
	JobID     string  `json:"job_id"`
	Status    string  `json:"status"`
	Missing   int     `json:"missing"`
	RateLimit float64 `json:"rate_limit"`
	Batch     int     `json:"batch"`
}

type migrateEnv struct {
	db       *testDB
	calls    *providertest.OllamaCalls
	resolver embeddings.ProviderResolver
	mgr      *upgrade.Manager
	status   *httptest.Server
}

// newMigrateEnv registers the source model through the CLI (the register
// probe hits the recorded Ollama) and ingests bodies under it.
func newMigrateEnv(t *testing.T, bodies ...string) *migrateEnv {
	t.Helper()
	db := setupTestDB(t)
	client, calls := providertest.OllamaClient(t, migrateCassettes)
	useRegisterHTTPClient(t, client)
	e := &migrateEnv{
		db:    db,
		calls: calls,
		resolver: embeddings.NewProviderResolver(&embeddings.Resolver{
			LookupEnv:  func(string) (string, bool) { return "", false },
			HTTPClient: client,
		}),
		mgr: upgrade.NewManager(""),
	}
	// What dpkms serve's /healthz reports.
	e.status = httptest.NewServer(httpserver.Healthz(nil, httpserver.HealthzProbes{
		Upgrade: func(context.Context) *httpserver.UpgradeSnapshot {
			return httpserver.NewUpgradeSnapshot(e.mgr.Snapshot())
		},
	}))
	t.Cleanup(e.status.Close)

	registerJSON(t, db, migrateFromID, "--embedding-model", "snowflake-arctic-embed2", "--embedding-endpoint", registerEndpoint)
	e.ingest(t, bodies...)
	registerJSON(t, db, migrateToID, "--embedding-model", "nomic-embed-text", "--embedding-endpoint", registerEndpoint)
	return e
}

// ingest runs text.long jobs through a worker pool whose embedding step
// writes every populating model, as dpkms does.
func (e *migrateEnv) ingest(t *testing.T, bodies ...string) {
	t.Helper()
	drv := e.db.Driver
	pipes := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{
		Models:     registryOf(t, e.db),
		Resolver:   e.resolver,
		Embeddings: drv.Embeddings(),
	})
	q := jobs.NewQueue(drv.Jobs())
	var ids []string
	for i, body := range bodies {
		job := &storage.Job{
			ID: fmt.Sprintf("ingest-%d", i), Type: "ingest:text", Payload: body, Pipeline: "text.long",
			Source: "test", MaxRetries: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := q.Enqueue(context.Background(), job); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, job.ID)
	}
	e.runPool(t, jobs.NewWorkerPool(q, pipes, drv, 1, nil, migrateJobsCfg()), q, ids...)
}

func migrateJobsCfg() config.JobsConfig {
	return config.JobsConfig{PollInterval: 20 * time.Millisecond, StaleTimeout: 30 * time.Minute, MaxRetries: 3, MaxHops: 5}
}

// runPool runs pool until every job in ids is completed or failed.
func (e *migrateEnv) runPool(t *testing.T, pool *jobs.WorkerPool, q *jobs.Queue, ids ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			settled := 0
			for _, id := range ids {
				if j, _ := q.Get(ctx, id); j != nil && (j.Status == storage.JobCompleted || j.Status == storage.JobFailed) {
					settled++
				}
			}
			if settled == len(ids) {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	_ = pool.Start(ctx)
	for _, id := range ids {
		if j, _ := q.Get(context.Background(), id); j == nil || j.Status != storage.JobCompleted {
			t.Fatalf("job %s: %+v, want completed", id, j)
		}
	}
}

// worker is the in-process stand-in for dpkms serve's pool: the migrate
// handler registered with the daemon's status manager.
func (e *migrateEnv) worker(t *testing.T, clock migrate.Clock) (*jobs.WorkerPool, *jobs.Queue) {
	t.Helper()
	drv := e.db.Driver
	q := jobs.NewQueue(drv.Jobs())
	pool := jobs.NewWorkerPool(q, builtins.Registry(), drv, 1, nil, migrateJobsCfg())
	reg := registryOf(t, e.db)
	pool.Handle(migrate.JobType, (&migrate.Runner{
		Models: reg, Objects: drv.Objects(), Store: drv.Embeddings(),
		Resolver: e.resolver, Progress: e.mgr, Clock: clock,
	}).Handle)
	return pool, q
}

func (e *migrateEnv) migrate(t *testing.T, args ...string) migrateDoc {
	t.Helper()
	out, err := e.db.exec(append([]string{"embeddings", "migrate", "--format", "json"}, args...)...)
	if err != nil {
		t.Fatalf("embeddings migrate: %v\n%s", err, out)
	}
	var doc migrateDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	return doc
}

func (e *migrateEnv) coverage(t *testing.T, modelID string) float64 {
	t.Helper()
	out, err := e.db.exec("embeddings", "list", "--format", "json")
	if err != nil {
		t.Fatalf("embeddings list: %v\n%s", err, out)
	}
	var doc struct {
		Models []embeddingsListItem `json:"models"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	for _, m := range doc.Models {
		if m.ModelID == modelID {
			return m.Coverage
		}
	}
	t.Fatalf("%s not listed: %s", modelID, out)
	return 0
}

// upgradeStatus runs `ctxt upgrade status --format json` against the
// daemon stand-in.
func (e *migrateEnv) upgradeStatus(t *testing.T) (upgradeEnvelope, error) {
	t.Helper()
	out, err := e.db.exec("upgrade", "status", "--server", e.status.URL, "--format", "json")
	var env upgradeEnvelope
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(firstJSON(out))), &env); jerr != nil {
		t.Fatalf("decode status: %v\n%s", jerr, out)
	}
	return env, err
}

// firstJSON returns the first line starting with '{' onward; error text
// may follow a JSON document on the shared output stream.
func firstJSON(out string) string {
	i := strings.Index(out, "{")
	if i < 0 {
		return out
	}
	depth := 0
	for j := i; j < len(out); j++ {
		switch out[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return out[i : j+1]
			}
		}
	}
	return out[i:]
}

// pausingClock blocks the migration at its second rate-limit wait until
// released, so the test can read the status of a run in flight.
type pausingClock struct {
	mu      sync.Mutex
	now     time.Time
	n       int
	paused  chan struct{}
	release chan struct{}
}

func (c *pausingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *pausingClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.n++
	n := c.n
	c.now = c.now.Add(d)
	c.mu.Unlock()
	if n == 2 {
		close(c.paused)
		select {
		case <-c.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Register A, ingest under A, register B, migrate to B in the background:
// B reaches full coverage, ingest vectors for A are untouched, the run is
// visible in `ctxt upgrade status` while in flight, and re-running migrate
// neither queues a second job nor re-embeds anything.
func TestEmbeddingsMigrate_EndToEnd(t *testing.T) {
	e := newMigrateEnv(t, migrateBodies...)
	if c := e.coverage(t, migrateFromID); c != 1 {
		t.Fatalf("coverage of %s after ingest = %.2f, want 1.00", migrateFromID, c)
	}
	if c := e.coverage(t, migrateToID); c != 0 {
		t.Fatalf("coverage of %s before migrate = %.2f, want 0 (registered after ingest)", migrateToID, c)
	}

	doc := e.migrate(t, "--to", migrateToID, "--rate-limit", "100/s", "--batch", "2")
	if doc.Status != "queued" || doc.JobID == "" || doc.Missing != len(migrateBodies) || doc.RateLimit != 100 || doc.Batch != 2 {
		t.Fatalf("migrate = %+v, want queued for %d objects", doc, len(migrateBodies))
	}
	if again := e.migrate(t, "--to", migrateToID); again.Status != "already_queued" || again.JobID != doc.JobID {
		t.Errorf("second migrate = %+v, want already_queued %s", again, doc.JobID)
	}

	before := len(e.calls.URLs())
	clock := &pausingClock{paused: make(chan struct{}), release: make(chan struct{})}
	pool, q := e.worker(t, clock)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.runPool(t, pool, q, doc.JobID)
	}()

	select {
	case <-clock.paused:
	case <-time.After(30 * time.Second):
		t.Fatal("migration never reached its second provider call")
	}
	mid, err := e.upgradeStatus(t)
	if err != nil {
		t.Errorf("upgrade status while migrating: %v", err)
	}
	if mid.State != "in_progress" || mid.Bucket != "embeddings_migrate" || mid.Target != migrateToID ||
		mid.Total != len(migrateBodies) || mid.Done != 2 {
		t.Errorf("status mid-run = %+v, want in_progress embeddings_migrate 2/%d for %s", mid, len(migrateBodies), migrateToID)
	}
	close(clock.release)
	<-done
	if n := len(e.calls.URLs()) - before; n != len(migrateBodies) {
		t.Errorf("migration made %d provider calls, want %d (one per object)", n, len(migrateBodies))
	}

	if c := e.coverage(t, migrateToID); c != 1 {
		t.Errorf("coverage of %s after migrate = %.2f, want 1.00", migrateToID, c)
	}
	if c := e.coverage(t, migrateFromID); c != 1 {
		t.Errorf("coverage of %s after migrate = %.2f, want 1.00 (untouched)", migrateFromID, c)
	}
	if st, err := e.upgradeStatus(t); err != nil || st.State != "idle" {
		t.Errorf("status after = %+v (err %v), want idle", st, err)
	}
	if after := e.migrate(t, "--to", migrateToID); after.Status != migrateUpToDate || after.Missing != 0 || after.JobID != "" {
		t.Errorf("migrate over a complete model = %+v, want up_to_date with no job", after)
	}

	// A job that runs anyway (queued before the last one finished, or
	// replayed) embeds nothing.
	before = len(e.calls.URLs())
	job, err := jobs.NewQueue(e.db.Driver.Jobs()).EnqueueTask(context.Background(), migrate.JobType,
		`{"model_id":"`+migrateToID+`"}`, 1)
	if err != nil {
		t.Fatal(err)
	}
	pool, q = e.worker(t, nil)
	e.runPool(t, pool, q, job.ID)
	if n := len(e.calls.URLs()) - before; n != 0 {
		t.Errorf("replayed migration made %d provider calls, want 0", n)
	}
}

// One object the provider rejects is recorded and skipped; the rest are
// embedded, and `ctxt upgrade status` reports the failure count and exits
// non-zero until a re-run.
func TestEmbeddingsMigrate_FailedObjectIsReported(t *testing.T) {
	e := newMigrateEnv(t, append([]string{digitTable}, migrateBodies...)...)
	doc := e.migrate(t, "--to", migrateToID)
	if doc.Missing != len(migrateBodies)+1 {
		t.Fatalf("migrate = %+v, want %d missing", doc, len(migrateBodies)+1)
	}
	pool, q := e.worker(t, nil)
	e.runPool(t, pool, q, doc.JobID)

	want := float64(len(migrateBodies)) / float64(len(migrateBodies)+1)
	if c := e.coverage(t, migrateToID); c < want-0.001 || c > want+0.001 {
		t.Errorf("coverage = %.3f, want %.3f (all but the rejected object)", c, want)
	}
	st, err := e.upgradeStatus(t)
	if err == nil {
		t.Error("upgrade status exited 0 with a failed object")
	}
	if st.State != "failed" || st.Failed != 1 || st.Done != len(migrateBodies)+1 || st.Target != migrateToID ||
		!strings.Contains(st.LastError, "ctxt embeddings migrate --to "+migrateToID) {
		t.Errorf("status = %+v, want failed with 1 failure and a re-run hint", st)
	}

	out, _ := e.db.exec("upgrade", "status", "--server", e.status.URL)
	for _, want := range []string{"embeddings_migrate", migrateToID, "Failed:"} {
		if !strings.Contains(out, want) {
			t.Errorf("human status lacks %q:\n%s", want, out)
		}
	}
}

func TestEmbeddingsMigrate_Refusals(t *testing.T) {
	db := setupTestDB(t)
	for name, tc := range map[string]struct {
		args []string
		code string
	}{
		"no --to":      {[]string{}, output.CodeUsage},
		"bad id":       {[]string{"--to", "bad id"}, output.CodeUsage},
		"bad rate":     {[]string{"--to", migrateToID, "--rate-limit", "fast"}, output.CodeUsage},
		"bad batch":    {[]string{"--to", migrateToID, "--batch", "5000"}, output.CodeUsage},
		"unregistered": {[]string{"--to", migrateToID}, output.CodeNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := db.exec(append([]string{"embeddings", "migrate"}, tc.args...)...)
			assertClass(t, err, tc.code)
		})
	}
	jobsList, _, err := db.Driver.Jobs().List(context.Background(), storage.JobFilter{Type: migrate.JobType})
	if err != nil || len(jobsList) != 0 {
		t.Errorf("refused migrations queued %d jobs (err %v)", len(jobsList), err)
	}
}

// The migrate leaf passes kit's signature gate (flags, examples, 12fcc
// annotations) and keeps its local flags.
func TestEmbeddingsMigrate_Signature(t *testing.T) {
	target, _, err := rootCmd.Find([]string{"embeddings", "migrate"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"to", "rate-limit", "batch"} {
		if target.LocalFlags().Lookup(f) == nil {
			t.Errorf("embeddings migrate: missing local --%s", f)
		}
	}
	for _, v := range root.ValidateSignature().Violations {
		if strings.Contains(v.Path, "embeddings migrate") {
			t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
		}
	}
}

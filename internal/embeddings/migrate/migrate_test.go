package migrate_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/migrate"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// Cassettes were recorded against a real local Ollama serving
// nomic-embed-text (768 dimensions). Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 ./internal/embeddings/migrate/
const cassettes = "testdata/cassettes/ollama"

var nomic = registry.Model{
	ModelID:    "ollama-nomic-embed-text@2026-09-26",
	Provider:   "ollama",
	Dimension:  768,
	ConfigJSON: `{"backend":"ollama","model":"nomic-embed-text","endpoint":"http://127.0.0.1:11434"}`,
}

// Object texts: stored as text_content, which is what the embedding step
// embeds for an object without a graph.
var texts = []string{
	"numbat sightings near the dryandra woodland",
	"quokka colonies on rottnest island",
	"bilby burrows in the pilbara",
	"dugong feeding grounds in shark bay",
	"cassowary tracks in the daintree rainforest",
	"thorny devil basking near uluru",
}

// longText is a digit table whose first 8192 bytes (all the Ollama
// provider sends) still exceed nomic-embed-text's context: Ollama rejects
// it for real ("the input length exceeds the context length").
var longText = strings.Repeat("0 1 2 3 4 5 6 7 8 9 ", 500)

type fixture struct {
	drv      storage.StorageDriver
	reg      *registry.Store
	calls    *providertest.OllamaCalls
	resolver embeddings.ProviderResolver
	mgr      *upgrade.Manager
	shadow   string
	bus      *recBus
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	drv := storageutil.NewTestDriver(t)
	reg, err := registry.ForDriver(drv)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := reg.Register(ctx, nomic, false); err != nil {
		t.Fatal(err)
	}
	if err := drv.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(nomic)); err != nil {
		t.Fatal(err)
	}
	client, calls := providertest.OllamaClient(t, cassettes)
	shadow := filepath.Join(t.TempDir(), "upgrade-state.json")
	return &fixture{
		drv: drv, reg: reg, calls: calls,
		resolver: embeddings.NewProviderResolver(&embeddings.Resolver{
			LookupEnv:  func(string) (string, bool) { return "", false },
			HTTPClient: client,
		}),
		mgr:    upgrade.NewManager(shadow),
		shadow: shadow,
		bus:    &recBus{},
	}
}

func (f *fixture) object(t *testing.T, id, text string) {
	t.Helper()
	now := time.Now()
	if err := f.drv.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: id, Type: "text", RawContent: text, TextContent: text, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) runner(clock migrate.Clock) *migrate.Runner {
	return &migrate.Runner{
		Models:   f.reg,
		Objects:  f.drv.Objects(),
		Store:    f.drv.Embeddings(),
		Resolver: f.resolver,
		Progress: f.mgr,
		Bus:      f.bus,
		Clock:    clock,
	}
}

// rows maps object_id to its row count under modelID.
func (f *fixture) rows(t *testing.T, modelID string) map[string]int {
	t.Helper()
	db := f.drv.(interface{ DB() *sql.DB }).DB()
	r, err := db.QueryContext(context.Background(),
		`SELECT object_id, COUNT(*) FROM embeddings WHERE model_id = ? GROUP BY object_id`, modelID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := map[string]int{}
	for r.Next() {
		var id string
		var n int
		if err := r.Scan(&id, &n); err != nil {
			t.Fatal(err)
		}
		out[id] = n
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *fixture) coverage(t *testing.T, modelID string) float64 {
	t.Helper()
	models, err := f.reg.ListWithCoverage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range models {
		if m.ModelID == modelID {
			return m.Coverage
		}
	}
	t.Fatalf("model %s not listed", modelID)
	return 0
}

// embedCalls counts provider calls for nomic.
func (f *fixture) embedCalls() int { return len(f.calls.URLs()) }

func requireOneRowEach(t *testing.T, rows map[string]int, ids ...string) {
	t.Helper()
	if len(rows) != len(ids) {
		t.Errorf("rows for %d objects %v, want %d", len(rows), rows, len(ids))
	}
	for _, id := range ids {
		if rows[id] != 1 {
			t.Errorf("object %s has %d rows, want exactly 1", id, rows[id])
		}
	}
}

// recBus records every published event synchronously.
type recBus struct {
	mu  sync.Mutex
	evs []events.Event
}

func (b *recBus) Publish(_ context.Context, e events.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evs = append(b.evs, e)
	return nil
}
func (b *recBus) Subscribe(string, events.Handler) {}
func (b *recBus) Close() error                     { return nil }

func (b *recBus) topics() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.evs))
	for i, e := range b.evs {
		out[i] = e.Type
	}
	return out
}

func (b *recBus) last(topic string) (events.EmbeddingsMigrationPayload, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.evs) - 1; i >= 0; i-- {
		if b.evs[i].Type == topic {
			var p events.EmbeddingsMigrationPayload
			_ = json.Unmarshal(b.evs[i].Data, &p)
			return p, true
		}
	}
	return events.EmbeddingsMigrationPayload{}, false
}

// fakeClock advances only when slept; onSleep sees the 1-based sleep count.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	sleeps  []time.Duration
	onSleep func(ctx context.Context, n int) error
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	n := len(c.sleeps)
	hook := c.onSleep
	c.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, n); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	return nil
}

func (c *fakeClock) slept() (n int, total time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, d := range c.sleeps {
		total += d
	}
	return len(c.sleeps), total
}

func objID(i int) string { return "obj-0" + string(rune('1'+i)) }

// Only objects missing a row are embedded; a second run over a complete
// model embeds nothing and adds no rows.
func TestRun_EmbedsOnlyMissingObjectsAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		f.object(t, objID(i), texts[i])
	}
	res, err := f.runner(nil).Run(ctx, "job-1", migrate.Request{ModelID: nomic.ModelID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 2 || f.embedCalls() != 2 {
		t.Fatalf("first run: %+v, %d provider calls; want 2 embedded", res, f.embedCalls())
	}

	for i := 2; i < 5; i++ {
		f.object(t, objID(i), texts[i])
	}
	res, err = f.runner(nil).Run(ctx, "job-2", migrate.Request{ModelID: nomic.ModelID, Batch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 3 || res.Embedded != 3 || res.Failed != 0 {
		t.Errorf("second run: %+v, want the 3 new objects embedded", res)
	}
	if n := f.embedCalls(); n != 5 {
		t.Errorf("%d provider calls after two runs, want 5: objects with rows were embedded again", n)
	}

	res, err = f.runner(nil).Run(ctx, "job-3", migrate.Request{ModelID: nomic.ModelID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 || res.Done() != 0 || f.embedCalls() != 5 {
		t.Errorf("run over a complete model: %+v, %d calls; want no work", res, f.embedCalls())
	}
	requireOneRowEach(t, f.rows(t, nomic.ModelID), objID(0), objID(1), objID(2), objID(3), objID(4))
	if c := f.coverage(t, nomic.ModelID); c != 1 {
		t.Errorf("coverage %.2f, want 1.00", c)
	}
	if st := f.mgr.Snapshot(); st.State != upgrade.StateIdle {
		t.Errorf("status after a clean run = %+v, want idle", st)
	}
}

// A provider error on one object is recorded and skipped: the rest are
// embedded, the job does not fail, the status keeps the failure count, and
// a re-run retries only the missing object.
func TestRun_ProviderErrorIsRecordedAndSkipped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.object(t, "obj-01", texts[0])
	f.object(t, "obj-02-long", longText)
	f.object(t, "obj-03", texts[2])
	f.object(t, "obj-04", texts[3])

	job := &storage.Job{ID: "job-fail", Type: migrate.JobType, Payload: `{"model_id":"` + nomic.ModelID + `"}`}
	out, err := f.runner(nil).Handle(ctx, job)
	if err != nil {
		t.Fatalf("one object's provider error failed the job: %v", err)
	}
	var res migrate.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result %q: %v", out, err)
	}
	if res.Embedded != 3 || res.Failed != 1 || len(res.FailedObjects) != 1 || res.FailedObjects[0] != "obj-02-long" {
		t.Fatalf("result = %+v, want 3 embedded and obj-02-long failed", res)
	}
	requireOneRowEach(t, f.rows(t, nomic.ModelID), "obj-01", "obj-03", "obj-04")
	f.requireFailureRecorded(t, "job-fail", "obj-02-long")

	calls := f.embedCalls()
	res2, err := f.runner(nil).Run(ctx, "job-retry", migrate.Request{ModelID: nomic.ModelID})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Total != 1 || res2.Failed != 1 || f.embedCalls() != calls+1 {
		t.Errorf("re-run = %+v with %d new calls; want only the missing object retried", res2, f.embedCalls()-calls)
	}
}

// requireFailureRecorded checks the status and the completed event carry
// one failure of 4, naming failedID.
func (f *fixture) requireFailureRecorded(t *testing.T, jobID, failedID string) {
	t.Helper()
	st := f.mgr.Snapshot()
	if st.State != upgrade.StateFailed || st.Failed != 1 || st.Done != 4 || st.Total != 4 ||
		st.Target != nomic.ModelID || !strings.Contains(st.LastError, failedID) {
		t.Errorf("status = %+v, want failed with 1 of 4 failed naming %s", st, failedID)
	}
	p, ok := f.bus.last(string(events.TopicDpkmsUpgradeEmbeddingsMigrationCompleted))
	if !ok || p.Failed != 1 || p.Embedded != 3 || p.JobID != jobID {
		t.Errorf("completed event = %+v (seen %v)", p, ok)
	}
}

// --rate-limit spaces provider calls at least 1/N seconds apart.
func TestRun_RateLimitSpacesProviderCalls(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 4; i++ {
		f.object(t, objID(i), texts[i])
	}
	clock := newFakeClock()
	res, err := f.runner(clock).Run(context.Background(), "job-rate", migrate.Request{ModelID: nomic.ModelID, RateLimit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedded != 4 {
		t.Fatalf("result %+v", res)
	}
	n, total := clock.slept()
	// 4 calls at 2/s: the first goes at once, each later one waits 500ms.
	if n != 3 || total < 1500*time.Millisecond {
		t.Errorf("slept %d times for %s over 4 calls at 2/s; want 3 waits totalling >= 1.5s", n, total)
	}
}

// The status surface shows the run while it is in flight, and the bus
// carries started, progressed and completed.
func TestRun_StatusVisibleWhileRunning(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 4; i++ {
		f.object(t, objID(i), texts[i])
	}
	var mid upgrade.Status
	var disk upgrade.Status
	clock := newFakeClock()
	clock.onSleep = func(_ context.Context, n int) error {
		if n == 2 { // before the third provider call
			mid = f.mgr.Snapshot()
			disk, _, _ = upgrade.ReadShadow(f.shadow)
		}
		return nil
	}
	if _, err := f.runner(clock).Run(context.Background(), "job-status", migrate.Request{ModelID: nomic.ModelID, RateLimit: 100}); err != nil {
		t.Fatal(err)
	}
	if mid.State != upgrade.StateInProgress || mid.Bucket != upgrade.BucketEmbeddingsMigrate ||
		mid.Target != nomic.ModelID || mid.Total != 4 || mid.Done != 2 {
		t.Errorf("status mid-run = %+v, want in_progress embeddings_migrate 2/4 for %s", mid, nomic.ModelID)
	}
	if disk.Target != nomic.ModelID || disk.Done != 2 {
		t.Errorf("shadow file mid-run = %+v", disk)
	}
	if st := f.mgr.Snapshot(); st.State != upgrade.StateIdle {
		t.Errorf("status after = %+v, want idle", st)
	}
	got := strings.Join(f.bus.topics(), " ")
	for _, want := range []string{
		string(events.TopicDpkmsUpgradeEmbeddingsMigrationStarted),
		string(events.TopicDpkmsUpgradeEmbeddingsMigrationProgress),
		string(events.TopicDpkmsUpgradeEmbeddingsMigrationCompleted),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("events %q lack %s", got, want)
		}
	}
}

// A dpkms restart mid-migration resumes where the rows stop: every object
// ends with one row, and no object is embedded twice.
func TestHandle_RestartResumesWithoutDuplicates(t *testing.T) {
	f := newFixture(t)
	for i := range texts {
		f.object(t, objID(i), texts[i])
	}
	q := jobs.NewQueue(f.drv.Jobs())
	payload, err := migrate.Request{ModelID: nomic.ModelID, RateLimit: 1000, Batch: 2}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	job, err := q.EnqueueTask(context.Background(), migrate.JobType, payload, 3)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.JobsConfig{PollInterval: 20 * time.Millisecond, StaleTimeout: 30 * time.Minute, MaxRetries: 3, MaxHops: 5}

	// First process: stops (SIGTERM) while waiting for the fourth call.
	poolCtx, stopPool := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopPool()
	stopped := false
	first := newFakeClock()
	first.onSleep = func(ctx context.Context, n int) error {
		if n == 3 {
			stopped = true
			stopPool()
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}
		return nil
	}
	pool := jobs.NewWorkerPool(q, builtins.Registry(), f.drv, 1, nil, cfg)
	pool.Handle(migrate.JobType, f.runner(first).Handle)
	if err := pool.Start(poolCtx); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("the first process never reached its fourth provider call")
	}
	got, _ := q.Get(context.Background(), job.ID)
	if got.Status != storage.JobRunning {
		t.Fatalf("job after shutdown: %s, want running (left for crash recovery)", got.Status)
	}
	partial := f.rows(t, nomic.ModelID)
	if len(partial) != 3 || f.embedCalls() != 3 {
		t.Fatalf("before restart: rows %v, %d calls; want 3", partial, f.embedCalls())
	}

	// Second process: crash recovery, then the same job resumes.
	if _, err := q.RecoverStale(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	f.mgr = upgrade.NewManager(f.shadow)
	pool = jobs.NewWorkerPool(q, builtins.Registry(), f.drv, 1, nil, cfg)
	pool.Handle(migrate.JobType, f.runner(newFakeClock()).Handle)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			if j, _ := q.Get(ctx, job.ID); j != nil && j.Status == storage.JobCompleted {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	_ = pool.Start(ctx)

	got, _ = q.Get(context.Background(), job.ID)
	if got.Status != storage.JobCompleted {
		t.Fatalf("job after restart: %+v, want completed", got)
	}
	ids := make([]string, len(texts))
	for i := range texts {
		ids[i] = objID(i)
	}
	requireOneRowEach(t, f.rows(t, nomic.ModelID), ids...)
	if n := f.embedCalls(); n != len(texts) {
		t.Errorf("%d provider calls across the restart, want %d (one per object)", n, len(texts))
	}
	if st := f.mgr.Snapshot(); st.State != upgrade.StateIdle {
		t.Errorf("status after resume = %+v, want idle", st)
	}
}

// Configuration errors fail the job for good: retrying cannot fix them.
func TestHandle_PermanentErrors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	old := registry.Model{ModelID: "ollama-old@2020-01-01", Provider: "ollama", Dimension: 768, ConfigJSON: nomic.ConfigJSON}
	if err := f.reg.Register(ctx, old, false); err != nil {
		t.Fatal(err)
	}
	if err := f.reg.Deprecate(ctx, old.ModelID, past); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"bad json":      `{"model_id":`,
		"unknown field": `{"model_id":"` + nomic.ModelID + `","budget_usd":5}`,
		"invalid id":    `{"model_id":"bad id"}`,
		"unregistered":  `{"model_id":"ollama-missing@1"}`,
		"deprecated":    `{"model_id":"` + old.ModelID + `"}`,
		"negative rate": `{"model_id":"` + nomic.ModelID + `","rate_limit":-1}`,
		"batch too big": `{"model_id":"` + nomic.ModelID + `","batch":5000}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.runner(nil).Handle(ctx, &storage.Job{ID: "j", Type: migrate.JobType, Payload: payload})
			if !pipeline.IsPermanent(err) {
				t.Errorf("Handle(%s) = %v, want a permanent error", payload, err)
			}
		})
	}
	if f.embedCalls() != 0 {
		t.Errorf("%d provider calls for rejected jobs", f.embedCalls())
	}
}

func TestParseRate(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
	}{{"5", 5}, {"5/s", 5}, {"0.5/s", 0.5}, {"0", 0}, {" 2 ", 2}} {
		in, want := c.in, c.want
		got, err := migrate.ParseRate(in)
		if err != nil || got != want {
			t.Errorf("ParseRate(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "fast", "-1", "5/m", "NaN"} {
		if _, err := migrate.ParseRate(in); err == nil {
			t.Errorf("ParseRate(%q) accepted", in)
		}
	}
}

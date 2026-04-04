package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"hop.top/uri"
)

// defaultTestJobsCfg returns a JobsConfig with fast poll for tests.
func defaultTestJobsCfg() config.JobsConfig {
	return config.JobsConfig{
		PollInterval: 50 * time.Millisecond,
		StaleTimeout: 30 * time.Minute,
		MaxRetries:   3,
		MaxHops:      5,
	}
}

func TestProcessJob(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-1")
	job.Payload = "short text content"
	job.Pipeline = "text.short"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())

	// Run pool in background, cancel after processing.
	go func() {
		for {
			got, _ := q.Get(ctx, "job-1")
			if got != nil && got.Status == storage.JobCompleted {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, _ := q.Get(context.Background(), "job-1")
	if got.Status != storage.JobCompleted {
		t.Fatalf("status: got %q, want completed", got.Status)
	}
	if got.ResultID == "" {
		t.Fatal("result_id should be set")
	}

	// Verify object was created.
	obj, err := driver.Objects().Get(context.Background(), got.ResultID)
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	if obj.Pipeline != "text.short" {
		t.Errorf("pipeline: got %q", obj.Pipeline)
	}
}

func TestProcessJobCreatesEdges(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())

	// Create a custom pipeline that adds mentions.
	pipes := pipeline.NewRegistry()
	pipes.Register("test.mentions", &pipeline.Pipeline{
		PipelineName: "test.mentions",
		Steps:        []pipeline.PipelineStep{&mentionStep{}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-1")
	job.Pipeline = "test.mentions"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())

	go func() {
		for {
			got, _ := q.Get(ctx, "job-1")
			if got != nil && got.Status == storage.JobCompleted {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, _ := q.Get(context.Background(), "job-1")
	if got.ResultID == "" {
		t.Fatal("result_id should be set")
	}

	edges, err := driver.Edges().ListFrom(context.Background(), "object", got.ResultID)
	if err != nil {
		t.Fatalf("list edges: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("edges: got %d, want 1", len(edges))
	}
}

func TestProcessJobFailure(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := pipeline.NewRegistry() // empty registry — pipeline won't be found

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-1")
	job.Pipeline = "nonexistent"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())

	go func() {
		for {
			got, _ := q.Get(ctx, "job-1")
			if got != nil && got.Status == storage.JobFailed {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, _ := q.Get(context.Background(), "job-1")
	if got.Status != storage.JobFailed {
		t.Errorf("status: got %q, want failed", got.Status)
	}
}

func TestWorkerPoolShutdown(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := builtins.Registry()

	ctx, cancel := context.WithCancel(context.Background())
	pool := NewWorkerPool(q, pipes, driver, 2, nil, defaultTestJobsCfg())

	done := make(chan struct{})
	go func() {
		pool.Start(ctx)
		close(done)
	}()

	// Cancel immediately.
	cancel()

	select {
	case <-done:
		// Clean exit.
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not shut down in time")
	}
}

func TestStaleRecovery(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	// Simulate a stale job: enqueue and acquire (making it "running").
	q.Enqueue(ctx, makeJob("job-1"))
	q.AcquireNext(ctx)

	// Recover with 0 timeout — all running jobs are stale.
	n, err := q.RecoverStale(ctx, 0)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 1 {
		t.Errorf("recovered: got %d", n)
	}

	got, _ := q.Get(ctx, "job-1")
	if got.Status != storage.JobPending {
		t.Errorf("status: got %q", got.Status)
	}
}

func TestWorkerPoolUsesConfigPollInterval(t *testing.T) {
	jobCfg := config.JobsConfig{
		PollInterval: 100 * time.Millisecond,
		StaleTimeout: 30 * time.Minute,
		MaxRetries:   3,
		MaxHops:      5,
	}
	pool := NewWorkerPool(nil, nil, nil, 1, nil, jobCfg)
	assert.Equal(t, 100*time.Millisecond, pool.pollInterval)
	assert.Equal(t, 5, pool.maxHops)
}

// mentionStep is a test step that adds a mention to the draft.
type mentionStep struct {
	pipeline.BaseContract
}

func (s *mentionStep) Name() string { return "test-mention" }
func (s *mentionStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Mentions = []uri.URI{{Scheme: "ctxt", Space: "entity", ID: "test/entity"}}
	return draft, nil
}

func TestWorkerPoolEmitsObjectCreated(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	bus := events.NewLocalBus()

	var mu sync.Mutex
	var received []events.Event
	bus.Subscribe("object.created", func(_ context.Context, e events.Event) error {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-oc-1")
	job.Payload = "object created event test content"
	job.Pipeline = "text.short"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, bus, defaultTestJobsCfg())

	go func() {
		for {
			got, _ := q.Get(ctx, "job-oc-1")
			if got != nil && got.Status == storage.JobCompleted {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	// Allow async handlers to run.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := len(received)
	var objectID string
	if count > 0 {
		var payload map[string]string
		if err := json.Unmarshal(received[0].Data, &payload); err == nil {
			objectID = payload["id"]
		}
	}
	mu.Unlock()

	if count != 1 {
		t.Errorf("object.created events: got %d, want 1", count)
	}
	if objectID == "" {
		t.Error("object.created event missing id in payload")
	}
	if count > 0 && received[0].Source != "worker.pool" {
		t.Errorf("source: got %q, want %q", received[0].Source, "worker.pool")
	}
}

func TestWorkerPoolNoObjectCreatedOnFailure(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := pipeline.NewRegistry() // empty — pipeline not found → job fails
	bus := events.NewLocalBus()

	var mu sync.Mutex
	var received []events.Event
	bus.Subscribe("object.created", func(_ context.Context, e events.Event) error {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-oc-fail")
	job.Pipeline = "nonexistent"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, bus, defaultTestJobsCfg())

	go func() {
		for {
			got, _ := q.Get(ctx, "job-oc-fail")
			if got != nil && got.Status == storage.JobFailed {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count != 0 {
		t.Errorf("object.created events on failure: got %d, want 0", count)
	}
}

// TestEdgeWriteFailureFails verifies that a failed edge write causes the job to
// be marked failed rather than completed, and that no object.created event is
// emitted. This is the regression test for T-0206.
func TestEdgeWriteFailureFails(t *testing.T) {
	base := storageutil.NewTestDriver(t)
	driver := &failingEdgeDriver{StorageDriver: base}
	q := NewQueue(driver.Jobs())

	pipes := pipeline.NewRegistry()
	pipes.Register("test.mentions", &pipeline.Pipeline{
		PipelineName: "test.mentions",
		Steps:        []pipeline.PipelineStep{&mentionStep{}},
	})

	bus := events.NewLocalBus()
	var mu sync.Mutex
	var objectCreatedCount int
	bus.Subscribe("object.created", func(_ context.Context, _ events.Event) error {
		mu.Lock()
		objectCreatedCount++
		mu.Unlock()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-edge-fail")
	job.Pipeline = "test.mentions"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, bus, defaultTestJobsCfg())

	go func() {
		for {
			got, _ := q.Get(ctx, "job-edge-fail")
			if got != nil && got.Status == storage.JobFailed {
				cancel()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	got, _ := q.Get(context.Background(), "job-edge-fail")
	if got.Status != storage.JobFailed {
		t.Errorf("status: got %q, want failed", got.Status)
	}
	if got.Error == "" {
		t.Error("job.Error should be set when edge write fails")
	}

	mu.Lock()
	count := objectCreatedCount
	mu.Unlock()
	if count != 0 {
		t.Errorf("object.created events on edge failure: got %d, want 0", count)
	}
}

// TestReinforceAfterRaceRetry verifies that the worker retries Reinforce when
// a UNIQUE constraint race produces a transient "no rows" error.
func TestReinforceAfterRaceRetry(t *testing.T) {
	base := storageutil.NewTestDriver(t)
	driver := newRaceObjectDriver(base, 1) // fail first Reinforce, succeed on second
	q := NewQueue(driver.Jobs())
	pipes := builtins.Registry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	job := makeJob("job-race-retry")
	job.Payload = "race retry test content"
	job.Pipeline = "text.short"
	q.Enqueue(ctx, job)

	pool := NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg())

	done := make(chan struct{})
	go func() {
		for {
			got, _ := q.Get(context.Background(), "job-race-retry")
			if got != nil && (got.Status == storage.JobCompleted || got.Status == storage.JobFailed) {
				close(done)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	go pool.Start(ctx)
	<-done
	cancel()

	got, _ := q.Get(context.Background(), "job-race-retry")
	if got.Status == storage.JobFailed {
		t.Logf("job error: %s", got.Error)
	}
	assert.Equal(t, storage.JobCompleted, got.Status, "job should complete after retry")
	assert.NotEmpty(t, got.ResultID)
}

// raceObjectDriver wraps a StorageDriver, returning a shared raceObjectStore
// that simulates the UNIQUE constraint race condition.
type raceObjectDriver struct {
	storage.StorageDriver
	store *raceObjectStore
}

func newRaceObjectDriver(base storage.StorageDriver, failCount int) *raceObjectDriver {
	return &raceObjectDriver{
		StorageDriver: base,
		store: &raceObjectStore{
			ObjectStore: base.Objects(),
			failCount:   failCount,
		},
	}
}

func (d *raceObjectDriver) Objects() storage.ObjectStore {
	return d.store
}

// raceObjectStore intercepts Create to return UNIQUE constraint error and
// Reinforce to fail with "no rows" for the first N calls.
type raceObjectStore struct {
	storage.ObjectStore
	mu           sync.Mutex
	failCount    int
	createCalled bool
	reinforced   int
}

func (s *raceObjectStore) Create(ctx context.Context, obj *storage.KnowledgeObject) error {
	s.mu.Lock()
	firstCall := !s.createCalled
	s.createCalled = true
	s.mu.Unlock()

	// First call: let the real store create the object so Reinforce can find it.
	if firstCall {
		if err := s.ObjectStore.Create(ctx, obj); err != nil {
			return err
		}
	}
	// Always return UNIQUE constraint to trigger the retry path.
	return errors.New("UNIQUE constraint failed: objects.content_hash")
}



func (s *raceObjectStore) Reinforce(ctx context.Context, hash string, mergeData *storage.KnowledgeObject) (string, error) {
	s.mu.Lock()
	call := s.reinforced
	s.reinforced++
	s.mu.Unlock()

	if call < s.failCount {
		return "", fmt.Errorf("reinforce: lookup: %w", sql.ErrNoRows)
	}
	return s.ObjectStore.Reinforce(ctx, hash, mergeData)
}

func (s *raceObjectStore) GetByContentHash(_ context.Context, _ string) (*storage.KnowledgeObject, error) {
	// Return not-found so the worker takes the Create path.
	return nil, nil
}

// failingEdgeDriver wraps a real StorageDriver and returns an error on every
// edge Create call, simulating a persistent storage failure.
type failingEdgeDriver struct {
	storage.StorageDriver
}

func (d *failingEdgeDriver) Edges() storage.EdgeStore {
	return &failingEdgeStore{EdgeStore: d.StorageDriver.Edges()}
}

type failingEdgeStore struct {
	storage.EdgeStore
}

func (s *failingEdgeStore) Create(_ context.Context, _ *storage.Edge) error {
	return errors.New("simulated edge write failure")
}

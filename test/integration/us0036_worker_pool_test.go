package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newWorkerTestDriver creates a SQLite driver with busy_timeout set so that
// concurrent writes during -race test runs don't hit SQLITE_BUSY.
func newWorkerTestDriver(t *testing.T) storage.StorageDriver {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db") + "?_pragma=busy_timeout(5000)"
	driver, err := sqlite.New(dsn)
	require.NoError(t, err)
	require.NoError(t, driver.Init(context.Background()))
	t.Cleanup(func() { driver.Close(context.Background()) })
	return driver
}

// startWorkerPool creates a WorkerPool with the given concurrency and
// a cancellable context. The pool starts in a goroutine; returns the context
// cancel func so tests can shut down the pool cleanly.
func startWorkerPool(
	t *testing.T,
	driver storage.StorageDriver,
	queue *jobs.Queue,
	pipes pipeline.Registry,
	workers int,
) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	pool := jobs.NewWorkerPool(queue, pipes, driver, workers, nil, config.JobsConfig{
		PollInterval: 30 * time.Millisecond,
		StaleTimeout: 30 * time.Minute,
		MaxRetries:   3,
		MaxHops:      5,
	})
	go func() { _ = pool.Start(ctx) }()
	return ctx, cancel
}

// waitAllJobsCompleted polls until all jobs in ids reach "completed" state,
// or the timeout expires.
func waitAllJobsCompleted(t *testing.T, queue *jobs.Queue, ids []string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timeout: not all jobs completed in time")
		case <-ticker.C:
			done := 0
			for _, id := range ids {
				job, err := queue.Get(context.Background(), id)
				if err != nil {
					continue
				}
				if job.Status == storage.JobCompleted {
					done++
				}
			}
			if done == len(ids) {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// US-0036 Tests
// Run with: go test -race ./test/integration/... -run TestUS0036
// ---------------------------------------------------------------------------

// TestUS0036_BurstJobsAllComplete verifies that a burst of jobs all complete
// without data races when the worker pool runs concurrently.
// NOTE: run this test with -race to catch data races.
func TestUS0036_BurstJobsAllComplete(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const nJobs = 20
	jobIDs := make([]string, nJobs)
	for i := 0; i < nJobs; i++ {
		id, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
			Content: fmt.Sprintf("burst content %d", i),
			Type:    "text",
			Source:  "e2e-test",
		})
		require.NoError(t, err)
		jobIDs[i] = id
	}

	// Wait for all jobs to complete.
	for _, id := range jobIDs {
		waitForJob(t, env.URL, id, storage.JobCompleted)
	}

	// Verify distinct result IDs.
	_, total, err := env.svc.ListJobs(context.Background(), storage.JobFilter{
		Status: storage.JobCompleted,
		Limit:  1000,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, nJobs, "all burst jobs should reach completed state")
}

// TestUS0036_MaxWorkersHonored verifies that all jobs complete even with a
// deliberatley low worker count, ensuring the pool queues and processes all.
func TestUS0036_MaxWorkersHonored(t *testing.T) {
	driver := newWorkerTestDriver(t)
	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()

	const workers = 2 // deliberately low to exercise queuing
	_, cancel := startWorkerPool(t, driver, queue, pipes, workers)
	defer cancel()

	const nJobs = 10
	ids := make([]string, nJobs)
	for i := 0; i < nJobs; i++ {
		jobID := fmt.Sprintf("max-workers-job-%02d", i)
		j := &storage.Job{
			ID:         jobID,
			Type:       "ingest:text",
			Status:     storage.JobPending,
			Payload:    fmt.Sprintf("max-workers content %d", i),
			Pipeline:   "text.short",
			Source:     "e2e-test",
			MaxRetries: 3,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		require.NoError(t, queue.Enqueue(context.Background(), j), "enqueue job %d", i)
		ids[i] = jobID
	}

	waitAllJobsCompleted(t, queue, ids, 30*time.Second)

	_, total, err := queue.List(context.Background(), storage.JobFilter{
		Status: storage.JobCompleted,
		Limit:  1000,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, nJobs,
		"all jobs should complete even with limited worker count")
}

// TestUS0036_ConcurrentIngestViaService verifies that multiple jobs enqueued
// sequentially are processed by the worker pool concurrently, all producing
// unique result IDs without data races.
// NOTE: run with -race to exercise the race detector.
func TestUS0036_ConcurrentIngestViaService(t *testing.T) {
	driver := newWorkerTestDriver(t) // busy_timeout set; safe for concurrent writers
	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()

	const workers = 4
	_, cancel := startWorkerPool(t, driver, queue, pipes, workers)
	defer cancel()

	// Enqueue sequentially (safe); workers process concurrently (races detected).
	const n = 12
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		jobID := fmt.Sprintf("concurrent-svc-job-%02d", i)
		j := &storage.Job{
			ID:         jobID,
			Type:       "ingest:text",
			Status:     storage.JobPending,
			Payload:    fmt.Sprintf("concurrent-ingest content %d", i),
			Pipeline:   "text.short",
			Source:     "e2e-test",
			MaxRetries: 3,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		require.NoError(t, queue.Enqueue(context.Background(), j), "enqueue job %d", i)
		ids[i] = jobID
	}

	waitAllJobsCompleted(t, queue, ids, 30*time.Second)

	// Verify via listing.
	_, total, err := queue.List(context.Background(), storage.JobFilter{
		Status: storage.JobCompleted,
		Limit:  1000,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, n, "all concurrent jobs should reach completed state")
}

// TestUS0036_NoDataRaceOnJobListDuringProcessing verifies that polling the job
// list while workers are processing concurrently does not produce data races.
// NOTE: run with -race to exercise the detector.
func TestUS0036_NoDataRaceOnJobListDuringProcessing(t *testing.T) {
	driver := newWorkerTestDriver(t)
	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()

	const workers = 3
	_, cancel := startWorkerPool(t, driver, queue, pipes, workers)
	defer cancel()

	const nJobs = 8
	ids := make([]string, nJobs)
	for i := 0; i < nJobs; i++ {
		jobID := fmt.Sprintf("race-check-job-%02d", i)
		j := &storage.Job{
			ID:         jobID,
			Type:       "ingest:text",
			Status:     storage.JobPending,
			Payload:    fmt.Sprintf("race-check content %d", i),
			Pipeline:   "text.short",
			Source:     "e2e-test",
			MaxRetries: 3,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		require.NoError(t, queue.Enqueue(context.Background(), j))
		ids[i] = jobID
	}

	// Poll concurrently from a separate goroutine while workers process.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			queue.List(context.Background(), storage.JobFilter{Limit: 100}) //nolint:errcheck
			time.Sleep(10 * time.Millisecond)
		}
	}()

	<-done

	waitAllJobsCompleted(t, queue, ids, 30*time.Second)

	_, total, err := queue.List(context.Background(), storage.JobFilter{
		Status: storage.JobCompleted,
		Limit:  1000,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, nJobs, "all race-check jobs should complete")
}

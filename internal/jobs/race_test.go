package jobs

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// newConcurrentTestDriver creates a SQLite driver with busy_timeout set in the
// DSN so that all connections in the pool inherit the setting. This prevents
// SQLITE_BUSY errors under concurrent write load in tests.
func newConcurrentTestDriver(t *testing.T) storage.StorageDriver {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db") + "?_pragma=busy_timeout(5000)"
	driver, err := sqlite.New(dsn)
	require.NoError(t, err)
	require.NoError(t, driver.Init(context.Background()))
	t.Cleanup(func() { driver.Close(context.Background()) })
	return driver
}

// allJobsCompleted polls until every job in ids reaches "completed" status,
// or the context expires. Returns true if all completed.
func allJobsCompleted(ctx context.Context, q *Queue, ids []string) bool {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			done := 0
			for _, id := range ids {
				job, err := q.Get(ctx, id)
				if err != nil {
					continue
				}
				if job.Status == storage.JobCompleted {
					done++
				}
			}
			if done == len(ids) {
				return true
			}
		}
	}
}

// TestConcurrentMultipleWorkersDequeue verifies that multiple workers process
// jobs without duplicates. 10 jobs are enqueued and 4 workers run; each job
// must be processed exactly once.
func TestConcurrentMultipleWorkersDequeue(t *testing.T) {
	driver := newConcurrentTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	const numJobs = 10
	ids := make([]string, numJobs)
	for i := 0; i < numJobs; i++ {
		id := fmt.Sprintf("race-mw-%d", i)
		ids[i] = id
		job := makeJob(id)
		// Stagger creation times so AcquireNext has a deterministic order.
		job.CreatedAt = time.Now().Add(time.Duration(-numJobs+i) * time.Second).Truncate(time.Second)
		require.NoError(t, q.Enqueue(ctx, job))
	}

	pool := NewWorkerPool(q, builtins.Registry(), driver, 4)
	pool.pollInterval = 50 * time.Millisecond

	poolCtx, poolCancel := context.WithTimeout(ctx, 10*time.Second)
	defer poolCancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pool.Start(poolCtx)
	}()

	// Wait for all jobs to complete.
	ok := allJobsCompleted(poolCtx, q, ids)
	require.True(t, ok, "not all jobs completed within 10s")

	// Cancel and wait for pool to exit.
	poolCancel()
	wg.Wait()

	// Verify each job processed exactly once.
	resultIDs := make(map[string]bool)
	for _, id := range ids {
		job, err := q.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, storage.JobCompleted, job.Status, "job %s should be completed", id)
		assert.NotEmpty(t, job.ResultID, "job %s should have a result ID", id)
		assert.False(t, resultIDs[job.ResultID], "duplicate result ID %s", job.ResultID)
		resultIDs[job.ResultID] = true
	}
}

// TestConcurrentEnqueueDequeue verifies that enqueueing jobs concurrently
// with worker processing does not cause races or lost jobs.
func TestConcurrentEnqueueDequeue(t *testing.T) {
	driver := newConcurrentTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	pool := NewWorkerPool(q, builtins.Registry(), driver, 2)
	pool.pollInterval = 50 * time.Millisecond

	poolCtx, poolCancel := context.WithTimeout(ctx, 15*time.Second)
	defer poolCancel()

	var poolWg sync.WaitGroup
	poolWg.Add(1)
	go func() {
		defer poolWg.Done()
		pool.Start(poolCtx)
	}()

	const numJobs = 20

	// Pre-compute all IDs so there is no data race on the ids slice.
	ids := make([]string, numJobs)
	for i := 0; i < numJobs; i++ {
		ids[i] = fmt.Sprintf("race-ed-%d", i)
	}

	// Enqueue jobs from a separate goroutine, with small delays to interleave
	// with worker processing. Retry on transient SQLite "database is locked"
	// errors that arise from concurrent writes.
	enqueueDone := make(chan struct{})
	go func() {
		defer close(enqueueDone)
		for i := 0; i < numJobs; i++ {
			job := makeJob(ids[i])
			job.CreatedAt = time.Now().Truncate(time.Second)
			for attempt := 0; attempt < 50; attempt++ {
				err := q.Enqueue(poolCtx, job)
				if err == nil {
					break
				}
				if poolCtx.Err() != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Wait for enqueuing to finish before polling for completion.
	select {
	case <-enqueueDone:
	case <-poolCtx.Done():
		t.Fatal("context expired before enqueue finished")
	}

	// Wait for all jobs to complete.
	ok := allJobsCompleted(poolCtx, q, ids)
	require.True(t, ok, "not all jobs completed within 15s")

	poolCancel()
	poolWg.Wait()

	// Verify all completed.
	for _, id := range ids {
		job, err := q.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, storage.JobCompleted, job.Status, "job %s should be completed", id)
		assert.NotEmpty(t, job.ResultID, "job %s should have a result ID", id)
	}
}

// TestShutdownWithInFlight verifies that cancelling the context while jobs
// are in-flight causes the pool to shut down cleanly (no deadlocks, no panics)
// within a reasonable time.
func TestShutdownWithInFlight(t *testing.T) {
	driver := newConcurrentTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	const numJobs = 5
	ids := make([]string, numJobs)
	for i := 0; i < numJobs; i++ {
		id := fmt.Sprintf("race-sd-%d", i)
		ids[i] = id
		job := makeJob(id)
		job.CreatedAt = time.Now().Add(time.Duration(-numJobs+i) * time.Second).Truncate(time.Second)
		require.NoError(t, q.Enqueue(ctx, job))
	}

	pool := NewWorkerPool(q, builtins.Registry(), driver, 2)
	pool.pollInterval = 50 * time.Millisecond

	poolCtx, poolCancel := context.WithCancel(ctx)

	// Wait until at least one job is completed before cancelling.
	go func() {
		for {
			time.Sleep(50 * time.Millisecond)
			for _, id := range ids {
				job, err := q.Get(context.Background(), id)
				if err == nil && job.Status == storage.JobCompleted {
					poolCancel()
					return
				}
			}
		}
	}()

	// Start must return within 5 seconds of context cancellation (no deadlock).
	done := make(chan error, 1)
	go func() {
		done <- pool.Start(poolCtx)
	}()

	select {
	case err := <-done:
		// Pool exited cleanly; err may be nil or context.Canceled.
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("pool did not shut down within 5s after context cancellation (possible deadlock)")
	}

	// Verify at least one job completed.
	var completedCount int
	for _, id := range ids {
		job, err := q.Get(ctx, id)
		if err == nil && job.Status == storage.JobCompleted {
			completedCount++
		}
	}
	assert.GreaterOrEqual(t, completedCount, 1, "at least one job should have completed before shutdown")
}

// TestStaleRecoveryUnderLoad verifies that a job stuck in "running" state
// (stale) is recovered and eventually completed when staleTimeout is set to 0.
func TestStaleRecoveryUnderLoad(t *testing.T) {
	driver := newConcurrentTestDriver(t)
	q := NewQueue(driver.Jobs())
	ctx := context.Background()

	// Create and acquire the job so it's in "running" state.
	job := makeJob("race-stale-1")
	require.NoError(t, q.Enqueue(ctx, job))

	acquired, err := q.AcquireNext(ctx)
	require.NoError(t, err)
	require.NotNil(t, acquired)
	require.Equal(t, "race-stale-1", acquired.ID)
	require.Equal(t, storage.JobRunning, acquired.Status)

	pool := NewWorkerPool(q, builtins.Registry(), driver, 2)
	pool.pollInterval = 50 * time.Millisecond
	// staleTimeout of 0 means all running jobs are treated as stale immediately.
	pool.staleTimeout = 0

	poolCtx, poolCancel := context.WithTimeout(ctx, 10*time.Second)
	defer poolCancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pool.Start(poolCtx)
	}()

	// The stale recovery loop runs every minute by default, which is too slow
	// for tests. Manually trigger recovery in a tight loop until the job is
	// recovered and eventually completed.
	ok := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// Manually trigger stale recovery.
		q.RecoverStale(ctx, 0)

		j, err := q.Get(ctx, "race-stale-1")
		if err == nil && j.Status == storage.JobCompleted {
			ok = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	poolCancel()
	wg.Wait()

	require.True(t, ok, "stale job should have been recovered and completed")

	// Final verification.
	final, err := q.Get(ctx, "race-stale-1")
	require.NoError(t, err)
	assert.Equal(t, storage.JobCompleted, final.Status)
	assert.NotEmpty(t, final.ResultID)
}

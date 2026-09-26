package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// Two dpkms processes on one database are two worker pools, each with its
// own driver handle (connection pool) and its own in-flight map. The
// scenarios below take two such handles and run the same checks on SQLite
// here and on Postgres under the integration tag.

// sharedSQLite opens two drivers on one SQLite file.
func sharedSQLite(t *testing.T) (a, b storage.StorageDriver) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "shared.db") + "?_pragma=busy_timeout(5000)"
	open := func() storage.StorageDriver {
		d, err := sqlite.New(dsn)
		require.NoError(t, err)
		require.NoError(t, d.Init(context.Background()))
		t.Cleanup(func() { d.Close(context.Background()) })
		return d
	}
	return open(), open()
}

// lapseLease makes a live lease look expired: the holder's lease is
// dropped and recovery (through other, another handle) requeues the job.
// A renewal can land between the two calls, so it retries.
func lapseLease(t *testing.T, holder, other storage.JobStore, id, claim string) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		require.NoError(t, holder.ReleaseLease(ctx, id, claim))
		n, err := other.RecoverStale(ctx, 0)
		require.NoError(t, err)
		if n == 1 {
			return
		}
	}
	t.Fatalf("job %s: lease never lapsed", id)
}

// processPool is one process's worker pool on driver.
func processPool(driver storage.StorageDriver, workers int) (*WorkerPool, *Queue) {
	q := NewQueue(driver.Jobs())
	return NewWorkerPool(q, builtins.Registry(), driver, workers, nil, defaultTestJobsCfg()), q
}

// blockingTask counts runs and blocks each one until release is closed.
type blockingTask struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func newBlockingTask() *blockingTask {
	return &blockingTask{started: make(chan struct{}, 8), release: make(chan struct{})}
}

func (b *blockingTask) handle(ctx context.Context, _ *storage.Job) (string, error) {
	b.calls.Add(1)
	b.started <- struct{}{}
	select {
	case <-b.release:
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// A second process starting while the first runs a task job runs crash
// recovery, then acquires. It must not start a second run of that job.
func runStartupRecoveryDoesNotStealLiveTask(t *testing.T, a, b storage.StorageDriver) {
	t.Helper()
	task := newBlockingTask()
	poolA, qA := processPool(a, 1)
	poolA.Handle(testTaskType, task.handle)
	job, err := qA.EnqueueTask(context.Background(), testTaskType, "x", 2)
	require.NoError(t, err)
	stopA := startPool(poolA)
	defer stopA()
	<-task.started

	// Process B starts: crash recovery, then its workers poll.
	poolB, qB := processPool(b, 2)
	poolB.Handle(testTaskType, task.handle)
	_, err = qB.RecoverStale(context.Background(), 0)
	require.NoError(t, err)
	stopB := startPool(poolB)
	defer stopB()

	time.Sleep(500 * time.Millisecond) // many poll intervals for B
	close(task.release)

	waitStatus(t, qA, job.ID, storage.JobCompleted)
	if n := task.calls.Load(); n != 1 {
		t.Errorf("task ran %d times across two processes, want 1", n)
	}
}

func TestClaim_StartupRecoveryDoesNotStealLiveTask_SQLite(t *testing.T) {
	a, b := sharedSQLite(t)
	runStartupRecoveryDoesNotStealLiveTask(t, a, b)
}

// The lease contract every JobStore keeps: only the current claim renews
// or releases; recovery skips a live lease, requeues an expired or
// released one, and requeueing (or a retry) voids the old claim.
func runLeaseStoreContract(t *testing.T, store storage.JobStore) {
	t.Helper()
	ctx := context.Background()
	q := NewQueue(store)
	job, err := q.EnqueueTask(ctx, testTaskType, "x", 2)
	require.NoError(t, err)

	got, err := store.AcquireNext(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, job.ID, got.ID)
	require.NotEmpty(t, got.Claim, "AcquireNext must return its claim")
	claim := got.Claim

	held, err := store.ExtendLease(ctx, job.ID, "someone-else", time.Minute)
	require.NoError(t, err)
	require.False(t, held, "a foreign claim renewed the lease")
	held, err = store.ExtendLease(ctx, job.ID, "", time.Minute)
	require.NoError(t, err)
	require.False(t, held, "an empty claim renewed the lease")
	held, err = store.ExtendLease(ctx, job.ID, claim, time.Minute)
	require.NoError(t, err)
	require.True(t, held)

	n, err := store.RecoverStale(ctx, 0)
	require.NoError(t, err)
	require.Zero(t, n, "crash recovery requeued a job under a live lease")

	// A foreign release is a no-op; the holder's release makes the job
	// recoverable again.
	require.NoError(t, store.ReleaseLease(ctx, job.ID, "someone-else"))
	n, err = store.RecoverStale(ctx, 0)
	require.NoError(t, err)
	require.Zero(t, n, "a foreign release dropped the lease")
	require.NoError(t, store.ReleaseLease(ctx, job.ID, claim))
	n, err = store.RecoverStale(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, 1, n, "a released job was not requeued")

	// The requeue voided the old claim; the next acquisition mints a new one.
	held, err = store.ExtendLease(ctx, job.ID, claim, time.Minute)
	require.NoError(t, err)
	require.False(t, held, "the old claim renewed a requeued job")
	again, err := store.AcquireNext(ctx)
	require.NoError(t, err)
	require.NotNil(t, again)
	require.NotEqual(t, claim, again.Claim)

	// An expired lease is recovered whatever the stale timeout.
	held, err = store.ExtendLease(ctx, job.ID, again.Claim, 50*time.Millisecond)
	require.NoError(t, err)
	require.True(t, held)
	time.Sleep(1100 * time.Millisecond)
	n, err = store.RecoverStale(ctx, 3600)
	require.NoError(t, err)
	require.Equal(t, 1, n, "an expired lease was not recovered")

	// A retry voids the claim too.
	third, err := store.AcquireNext(ctx)
	require.NoError(t, err)
	require.NotNil(t, third)
	require.NoError(t, store.Retry(ctx, job.ID))
	held, err = store.ExtendLease(ctx, job.ID, third.Claim, time.Minute)
	require.NoError(t, err)
	require.False(t, held, "a retried job kept its claim")
}

func TestClaim_LeaseStoreContract_SQLite(t *testing.T) {
	a, _ := sharedSQLite(t)
	runLeaseStoreContract(t, a.Jobs())
}

// Many task jobs, two processes polling: each job runs exactly once.
func runConcurrentAcquireRunsEachOnce(t *testing.T, a, b storage.StorageDriver) {
	t.Helper()
	const jobs = 20
	var calls sync.Map // job ID -> *atomic.Int32
	handle := func(_ context.Context, job *storage.Job) (string, error) {
		c, _ := calls.LoadOrStore(job.ID, new(atomic.Int32))
		c.(*atomic.Int32).Add(1)
		time.Sleep(20 * time.Millisecond)
		return "ok", nil
	}
	poolA, qA := processPool(a, 3)
	poolB, _ := processPool(b, 3)
	poolA.Handle(testTaskType, handle)
	poolB.Handle(testTaskType, handle)
	ids := make([]string, 0, jobs)
	for i := 0; i < jobs; i++ {
		job, err := qA.EnqueueTask(context.Background(), testTaskType, "x", 2)
		require.NoError(t, err)
		ids = append(ids, job.ID)
	}
	stopA, stopB := startPool(poolA), startPool(poolB)
	defer stopA()
	defer stopB()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.True(t, allJobsCompleted(ctx, qA, ids), "not every task job completed")
	for _, id := range ids {
		c, ok := calls.Load(id)
		if !ok || c.(*atomic.Int32).Load() != 1 {
			n := int32(0)
			if ok {
				n = c.(*atomic.Int32).Load()
			}
			t.Errorf("job %s ran %d times, want 1", id, n)
		}
	}
}

func TestClaim_ConcurrentAcquireRunsEachOnce_SQLite(t *testing.T) {
	a, b := sharedSQLite(t)
	runConcurrentAcquireRunsEachOnce(t, a, b)
}

// A process that dies mid-task leaves its lease behind. Another process's
// crash recovery leaves the job alone while the lease is live, and its
// stale recovery requeues and runs it once the lease runs out.
func runDeadHolderLeaseExpires(t *testing.T, a, b storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	dead := NewQueue(a.Jobs())
	job, err := dead.EnqueueTask(ctx, testTaskType, "x", 2)
	require.NoError(t, err)
	got, err := dead.AcquireNext(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	held, err := dead.ExtendLease(ctx, got.ID, got.Claim, 1500*time.Millisecond)
	require.NoError(t, err)
	require.True(t, held)
	// The process dies here: no renewal, no release.

	var calls atomic.Int32
	poolB, qB := processPool(b, 1)
	poolB.recoverEvery = 100 * time.Millisecond
	poolB.Handle(testTaskType, func(context.Context, *storage.Job) (string, error) {
		calls.Add(1)
		return "resumed", nil
	})
	n, err := qB.RecoverStale(ctx, 0)
	require.NoError(t, err)
	require.Zero(t, n, "crash recovery requeued a job under a live lease")
	stop := startPool(poolB)
	defer stop()

	done := waitStatus(t, qB, job.ID, storage.JobCompleted)
	require.Equal(t, "resumed", done.ResultID)
	require.Equal(t, int32(1), calls.Load())
}

func TestClaim_DeadHolderLeaseExpires_SQLite(t *testing.T) {
	a, b := sharedSQLite(t)
	runDeadHolderLeaseExpires(t, a, b)
}

// A holder whose lease ran out and whose job another process claimed
// stops with ErrLeaseLost and leaves settling to the new claimant.
func runLostLeaseStopsFormerHolder(t *testing.T, a, b storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	claims := make(chan string, 1)
	causes := make(chan error, 1)
	poolA, qA := processPool(a, 1)
	poolA.leaseTTL = 900 * time.Millisecond
	poolA.Handle(testTaskType, func(ctx context.Context, job *storage.Job) (string, error) {
		claims <- job.Claim
		<-ctx.Done()
		causes <- context.Cause(ctx)
		return "", ctx.Err()
	})
	job, err := qA.EnqueueTask(ctx, testTaskType, "x", 2)
	require.NoError(t, err)
	stopA := startPool(poolA)
	defer stopA()
	claim := <-claims

	// A's lease lapses (as if renewals failed), recovery requeues the job
	// and process B claims and runs it.
	lapseLease(t, a.Jobs(), b.Jobs(), job.ID, claim)
	poolB, qB := processPool(b, 1)
	release := make(chan struct{})
	poolB.Handle(testTaskType, func(ctx context.Context, _ *storage.Job) (string, error) {
		<-release
		return "taken over", nil
	})
	stopB := startPool(poolB)
	defer stopB()
	defer func() {
		select {
		case <-release:
		default:
			close(release) // unblock B's handler so stopB returns on failure
		}
	}()

	select {
	case c := <-causes:
		if !errors.Is(c, ErrLeaseLost) {
			t.Errorf("former holder's cause = %v, want ErrLeaseLost", c)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("former holder never stopped")
	}
	close(release)
	done := waitStatus(t, qB, job.ID, storage.JobCompleted)
	require.Equal(t, "taken over", done.ResultID)
}

func TestClaim_LostLeaseStopsFormerHolder_SQLite(t *testing.T) {
	a, b := sharedSQLite(t)
	runLostLeaseStopsFormerHolder(t, a, b)
}

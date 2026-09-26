package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const testTaskType = "test:task"

// taskPool returns a one-worker pool whose queue validates pipeline names,
// so a task job reaching the handler proves EnqueueTask skipped that check.
func taskPool(t *testing.T) (*WorkerPool, *Queue, storage.StorageDriver) {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	q.SetPipelineValidator(func(name string) error {
		_, err := pipes.Get(name)
		return err
	})
	return NewWorkerPool(q, pipes, driver, 1, nil, defaultTestJobsCfg()), q, driver
}

// startPool runs pool until the returned stop is called; stop waits for
// Start to return.
func startPool(pool *WorkerPool) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = pool.Start(ctx)
		close(done)
	}()
	return func() {
		cancel()
		<-done
	}
}

func waitStatus(t *testing.T, q *Queue, id string, want storage.JobStatus) *storage.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, err := q.Get(context.Background(), id)
		if err == nil && got.Status == want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, _ := q.Get(context.Background(), id)
	t.Fatalf("job %s: status %+v, want %s", id, got, want)
	return nil
}

func enqueueTask(t *testing.T, q *Queue, payload string) *storage.Job {
	t.Helper()
	job, err := q.EnqueueTask(context.Background(), testTaskType, payload, 2)
	if err != nil {
		t.Fatalf("EnqueueTask: %v", err)
	}
	return job
}

func TestEnqueueTask_SkipsPipelineValidation(t *testing.T) {
	_, q, _ := taskPool(t)
	job := enqueueTask(t, q, `{"k":"v"}`)
	got, err := q.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != testTaskType || got.Status != storage.JobPending || got.Pipeline != "" || got.Payload != `{"k":"v"}` || got.MaxRetries != 2 {
		t.Fatalf("stored job = %+v", got)
	}
	if _, err := q.EnqueueTask(context.Background(), "", "x", 0); err == nil {
		t.Error("EnqueueTask accepted an empty job type")
	}
}

func TestTask_HandlerCompletesJob(t *testing.T) {
	pool, q, _ := taskPool(t)
	var gotPayload atomic.Value
	pool.Handle(testTaskType, func(_ context.Context, job *storage.Job) (string, error) {
		gotPayload.Store(job.Payload)
		return "summary", nil
	})
	job := enqueueTask(t, q, "payload-1")
	stop := startPool(pool)
	defer stop()

	got := waitStatus(t, q, job.ID, storage.JobCompleted)
	if got.ResultID != "summary" {
		t.Errorf("result = %q, want summary", got.ResultID)
	}
	if gotPayload.Load() != "payload-1" {
		t.Errorf("handler saw payload %v", gotPayload.Load())
	}
}

func TestTask_PermanentErrorFailsWithoutRetry(t *testing.T) {
	pool, q, _ := taskPool(t)
	var calls atomic.Int32
	pool.Handle(testTaskType, func(context.Context, *storage.Job) (string, error) {
		calls.Add(1)
		return "", pipeline.Permanent(errors.New("bad payload"))
	})
	job := enqueueTask(t, q, "x")
	stop := startPool(pool)
	defer stop()

	got := waitStatus(t, q, job.ID, storage.JobFailed)
	if got.RetryCount != 0 || calls.Load() != 1 {
		t.Errorf("retries %d, calls %d; a permanent error must not retry", got.RetryCount, calls.Load())
	}
}

func TestTask_TransientErrorRetries(t *testing.T) {
	pool, q, _ := taskPool(t)
	var calls atomic.Int32
	pool.Handle(testTaskType, func(context.Context, *storage.Job) (string, error) {
		if calls.Add(1) == 1 {
			return "", errors.New("provider briefly down")
		}
		return "ok", nil
	})
	job := enqueueTask(t, q, "x")
	stop := startPool(pool)
	defer stop()

	got := waitStatus(t, q, job.ID, storage.JobCompleted)
	if got.RetryCount != 1 || calls.Load() != 2 {
		t.Errorf("retries %d, calls %d; want one retry then success", got.RetryCount, calls.Load())
	}
}

// An operator cancel (`dpkms job cancel`) reaches the running handler as a
// context cancelled with ErrJobCancelled, and the job stays cancelled.
func TestTask_OperatorCancelStopsHandler(t *testing.T) {
	pool, q, driver := taskPool(t)
	started := make(chan struct{})
	cause := make(chan error, 1)
	pool.Handle(testTaskType, func(ctx context.Context, _ *storage.Job) (string, error) {
		close(started)
		<-ctx.Done()
		cause <- context.Cause(ctx)
		return "", ctx.Err()
	})
	job := enqueueTask(t, q, "x")
	stop := startPool(pool)
	defer stop()

	<-started
	if err := driver.Jobs().Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-cause:
		if !errors.Is(c, ErrJobCancelled) {
			t.Errorf("cause = %v, want ErrJobCancelled", c)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("handler never saw the cancel")
	}
	time.Sleep(100 * time.Millisecond)
	got, _ := q.Get(context.Background(), job.ID)
	if got.Status != jobCancelled {
		t.Errorf("status %s after cancel, want cancelled", got.Status)
	}
}

// Shutdown interrupts the handler and leaves the job running, so the next
// start's crash recovery (RecoverStale(0)) requeues it and it resumes.
func TestTask_ShutdownLeavesJobForRecovery(t *testing.T) {
	pool, q, driver := taskPool(t)
	started := make(chan struct{}, 2)
	var calls atomic.Int32
	pool.Handle(testTaskType, func(ctx context.Context, _ *storage.Job) (string, error) {
		if calls.Add(1) == 1 {
			started <- struct{}{}
			<-ctx.Done()
			return "", ctx.Err()
		}
		return "resumed", nil
	})
	job := enqueueTask(t, q, "x")
	stop := startPool(pool)
	<-started
	stop()

	got, _ := q.Get(context.Background(), job.ID)
	if got.Status != storage.JobRunning || got.RetryCount != 0 {
		t.Fatalf("after shutdown: status %s retries %d, want running with no retry spent", got.Status, got.RetryCount)
	}

	if _, err := q.RecoverStale(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	next := NewWorkerPool(q, builtins.Registry(), driver, 1, nil, defaultTestJobsCfg())
	next.handlers = pool.handlers
	stop = startPool(next)
	defer stop()
	if got := waitStatus(t, q, job.ID, storage.JobCompleted); got.ResultID != "resumed" {
		t.Errorf("result %q after restart", got.ResultID)
	}
}

// Stale recovery can requeue a long task this pool is still running once
// its lease lapsed; the pool re-acquires it. That must not start a second,
// concurrent run: the running handler takes over the new claim, keeps the
// lease, and settles the job.
func TestTask_ReacquiredInFlightJobRunsOnce(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	q := NewQueue(driver.Jobs())
	pool := NewWorkerPool(q, builtins.Registry(), driver, 2, nil, defaultTestJobsCfg())
	pool.leaseTTL = 600 * time.Millisecond
	release := make(chan struct{})
	claims := make(chan string, 2)
	var calls atomic.Int32
	var cause atomic.Value
	pool.Handle(testTaskType, func(ctx context.Context, job *storage.Job) (string, error) {
		calls.Add(1)
		claims <- job.Claim
		select {
		case <-release:
			return "done", nil
		case <-ctx.Done():
			cause.Store(context.Cause(ctx))
			return "", ctx.Err()
		}
	})
	job := enqueueTask(t, q, "x")
	stop := startPool(pool)
	defer stop()

	claim := <-claims
	lapseLease(t, driver.Jobs(), driver.Jobs(), job.ID, claim)
	waitStatus(t, q, job.ID, storage.JobRunning) // the second worker re-acquired it
	time.Sleep(time.Second)                      // several lease renewals under the new claim
	if n, err := q.RecoverStale(context.Background(), 0); err != nil || n != 0 {
		t.Fatalf("RecoverStale = %d, %v; the running handler must hold the new claim's lease", n, err)
	}
	close(release)

	got := waitStatus(t, q, job.ID, storage.JobCompleted)
	if n := calls.Load(); n != 1 {
		t.Errorf("handler ran %d times, want 1", n)
	}
	if c := cause.Load(); c != nil {
		t.Errorf("running handler stopped: %v", c)
	}
	if got.ResultID != "done" {
		t.Errorf("result %q, want the running handler's", got.ResultID)
	}
}

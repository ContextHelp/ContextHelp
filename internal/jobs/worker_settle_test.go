package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

var errSettle = errors.New("settle: database is locked")

// settleFailJobs rejects every Complete, Fail and RecoverStale, as a store
// under lock contention or a dropped connection does.
type settleFailJobs struct {
	storage.JobStore
}

func (settleFailJobs) Complete(context.Context, string, string) error { return errSettle }
func (settleFailJobs) Fail(context.Context, string, string) error     { return errSettle }
func (settleFailJobs) RecoverStale(context.Context, int64) (int, error) {
	return 0, errSettle
}

// acquiredJob enqueues and acquires one job, leaving it running.
func acquiredJob(t *testing.T, q *Queue, id, pipe, payload string) *storage.Job {
	t.Helper()
	ctx := context.Background()
	job := makeJob(id)
	job.Pipeline = pipe
	job.Payload = payload
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	got, err := q.AcquireNext(ctx)
	if err != nil || got == nil {
		t.Fatalf("acquire: %v (job %v)", err, got)
	}
	return got
}

// A job whose completion the store rejects stays running until stale
// recovery re-runs it; the rejection must be logged, not dropped.
func TestProcess_CompleteErrorIsLogged(t *testing.T) {
	logs := captureSlog(t)
	driver := storageutil.NewTestDriver(t)
	job := acquiredJob(t, NewQueue(driver.Jobs()), "job-settle-complete", "text.short", "settle complete body")

	pool := NewWorkerPool(NewQueue(settleFailJobs{driver.Jobs()}), builtins.Registry(), driver, 1, nil, defaultTestJobsCfg())
	pool.process(context.Background(), job)

	out := logs.String()
	if !strings.Contains(out, "jobs: mark completed") || !strings.Contains(out, "job="+job.ID) || !strings.Contains(out, errSettle.Error()) {
		t.Errorf("rejected Complete not logged with job and cause; logs:\n%s", out)
	}
}

func TestProcess_FailErrorIsLogged(t *testing.T) {
	logs := captureSlog(t)
	driver := storageutil.NewTestDriver(t)
	pipes := pipeline.NewRegistry()
	pipes.Register("test.permanent", &pipeline.Pipeline{
		PipelineName: "test.permanent",
		Steps:        []pipeline.PipelineStep{&permanentStep{}},
	})
	job := acquiredJob(t, NewQueue(driver.Jobs()), "job-settle-fail", "test.permanent", "settle fail body")

	pool := NewWorkerPool(NewQueue(settleFailJobs{driver.Jobs()}), pipes, driver, 1, nil, defaultTestJobsCfg())
	pool.process(context.Background(), job)

	out := logs.String()
	if !strings.Contains(out, "jobs: mark failed") || !strings.Contains(out, "job="+job.ID) || !strings.Contains(out, errSettle.Error()) {
		t.Errorf("rejected Fail not logged with job and cause; logs:\n%s", out)
	}
}

func TestRecoverStaleLoop_ErrorIsLogged(t *testing.T) {
	logs := captureSlog(t)
	driver := storageutil.NewTestDriver(t)
	pool := NewWorkerPool(NewQueue(settleFailJobs{driver.Jobs()}), pipeline.NewRegistry(), driver, 1, nil, defaultTestJobsCfg())
	pool.recoverEvery = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = pool.recoverStaleLoop(ctx)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(logs.String(), "jobs: recover stale") {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	if out := logs.String(); !strings.Contains(out, "jobs: recover stale") || !strings.Contains(out, errSettle.Error()) {
		t.Errorf("RecoverStale failure not logged with its cause; logs:\n%s", out)
	}
}

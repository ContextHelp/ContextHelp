package rederive

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func TestRun_SuccessStampsTheCurrentSignature(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	f.add("o2", "alpha", alphaOld)
	f.add("cur", "alpha@v1", alphaNew) // already current: untouched
	if f.stale() != 2 {
		t.Fatalf("stale = %d before the run, want 2", f.stale())
	}
	res, err := f.run()
	if err != nil {
		t.Fatal(err)
	}
	if res.Processed != 2 || res.Total != 2 || res.SkippedTotal() != 0 {
		t.Fatalf("result = %+v", res)
	}
	for _, id := range []string{"o1", "o2"} {
		if got := f.sigOf(id); got != alphaNew {
			t.Errorf("%s signature = %q, want the current one", id, got)
		}
	}
	if f.stale() != 0 {
		t.Errorf("stale = %d after the run, want 0", f.stale())
	}
	// Groups run in order of stored pipeline stamp: "alpha" before "alpha@v1".
	if got := f.svc.called(); !slices.Equal(got, []string{"o2", "o1"}) {
		t.Errorf("re-derived %v", got)
	}
}

// A failed stamp must not count as success: the object stays stale.
func TestRun_NoStampWhenTheReanalysisFails(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	f.svc.result = func(string, service.ReanalyzeOptions) error { return errors.New("boom") }
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if got := f.sigOf("o1"); got != alphaOld {
		t.Fatalf("signature = %q, want the old one kept", got)
	}
}

func TestRun_PauseIsHonouredBetweenObjects(t *testing.T) {
	f := newFixture(t)
	f.job.Pause = 2 * time.Second
	for _, id := range []string{"o1", "o2", "o3"} {
		f.add(id, "alpha@v1", alphaOld)
	}
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{2 * time.Second, 2 * time.Second}
	if !slices.Equal(f.sleep, want) {
		t.Fatalf("waited %v, want %v (between the three, not before the first or after the last)", f.sleep, want)
	}
}

func TestRun_NoPauseWhenZero(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	f.add("o2", "alpha@v1", alphaOld)
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if len(f.sleep) != 0 {
		t.Fatalf("waited %v with no pause configured", f.sleep)
	}
}

func TestRun_CancellationStopsBetweenObjects(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"o1", "o2", "o3"} {
		f.add(id, "alpha@v1", alphaOld)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.onCall = func(string) { cancel() } // shutdown arrives during the first object
	prog := &fakeProgress{}
	f.job.Progress = prog
	res, err := f.job.Run(ctx, "job-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context's", err)
	}
	if got := f.svc.called(); len(got) != 1 {
		t.Fatalf("re-derived %v after the cancel, want only the first", got)
	}
	// A shutdown is not a failure: the status goes back to idle.
	if !res.Interrupted || prog.failed != nil || prog.done == nil {
		t.Errorf("interrupted=%v status failed=%v done=%v: want interrupted and idle", res.Interrupted, prog.failed, prog.done)
	}
	if len(f.skips(alphaNew)) != 0 {
		t.Errorf("a cancel recorded skips: %v", f.skips(alphaNew))
	}
	// The next start resumes with what is left.
	f.svc.onCall = nil
	res, err = f.run()
	if err != nil || res.Processed != 2 {
		t.Fatalf("resume: %+v, %v", res, err)
	}
}

func TestSchedule_OnlyWhenNeededAndNeverTwice(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	q := jobs.NewQueue(f.drv.Jobs())

	if j, err := f.job.Schedule(ctx, q); err != nil || j != nil {
		t.Fatalf("nothing stale: job=%v err=%v", j, err)
	}
	f.add("o1", "alpha@v1", alphaOld)
	f.job.Enabled = false
	if j, err := f.job.Schedule(ctx, q); err != nil || j != nil {
		t.Fatalf("disabled: job=%v err=%v", j, err)
	}
	f.job.Enabled = true
	first, err := f.job.Schedule(ctx, q)
	if err != nil || first == nil {
		t.Fatalf("stale rows: job=%v err=%v", first, err)
	}
	second, err := f.job.Schedule(ctx, q)
	if err != nil || second == nil || second.ID != first.ID {
		t.Fatalf("second schedule while pending: %v, %v (want the first job %s)", second, err, first.ID)
	}
	pending, _, err := q.List(ctx, storage.JobFilter{Type: JobType, Status: storage.JobPending, Limit: 10})
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending re-derive jobs = %d, %v; want 1", len(pending), err)
	}
}

// The status is single-slot and shared with the re-projection: a busy
// slot is waited for, not failed on, and nothing runs meanwhile.
func TestRun_WaitsForTheStatusSlot(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	mgr := upgrade.NewManager("")
	if err := mgr.Start(upgrade.BucketReindexAuto, 10); err != nil {
		t.Fatal(err)
	}
	f.job.Progress = mgr
	waits := 0
	f.job.Wait = func(context.Context, time.Duration) error {
		waits++
		if got := f.svc.called(); len(got) != 0 {
			t.Errorf("re-derived %v while the slot was held", got)
		}
		if waits == 2 {
			return mgr.Complete() // the other run ends
		}
		return nil
	}
	res, err := f.run()
	if err != nil || res.Processed != 1 {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if waits != 2 {
		t.Errorf("waited %d times, want 2", waits)
	}
	if st := mgr.Snapshot(); st.State != upgrade.StateIdle {
		t.Errorf("state after the run = %s", st.State)
	}
	if last := mgr.LastRun(); last.Processed != 1 {
		t.Errorf("LastRun = %+v", last)
	}
}

func TestRun_ReportsProgressAndTheDeferredInTheStatus(t *testing.T) {
	f := newFixture(t)
	prog := &fakeProgress{}
	f.job.Progress = prog
	f.add("o1", "alpha@v1", alphaOld)
	f.add("o2", "alpha@v1", alphaOld)
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if prog.total != 2 || len(prog.started) != 1 || !strings.HasPrefix(prog.started[0], "rederive@") {
		t.Errorf("status started %v with total %d", prog.started, prog.total)
	}
	if prog.done == nil || prog.done.Processed != 2 {
		t.Errorf("run summary = %+v", prog.done)
	}
	if last := prog.ticks[len(prog.ticks)-1]; last != [2]int{2, 0} {
		t.Errorf("last tick = %v", last)
	}
}

// --- outcomes ---

// A panic in a run ends the job as failed, without a retry, and never
// takes the process down.
func TestHandle_APanicFailsTheJobWithoutRetry(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	f.svc.onCall = func(string) { panic("boom") }
	msg, err := f.job.Handle(context.Background(), &storage.Job{ID: "j"})
	if err == nil || !pipeline.IsPermanent(err) || msg != "" {
		t.Fatalf("Handle = %q, %v; want a permanent error", msg, err)
	}
}

// A panic after the run began must free the upgrade status, so health is
// not "upgrading" until restart and a re-projection is not kept waiting.
func TestHandle_APanicFreesTheStatus(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	mgr := upgrade.NewManager("")
	f.job.Progress = mgr
	f.svc.onCall = func(string) { panic("boom") }
	if _, err := f.job.Handle(context.Background(), &storage.Job{ID: "j"}); err == nil || !pipeline.IsPermanent(err) {
		t.Fatalf("Handle err = %v", err)
	}
	if st := mgr.Snapshot(); st.State == upgrade.StateInProgress {
		t.Fatalf("status still %s %s after a panic", st.State, st.Bucket)
	}
	if err := mgr.StartTarget(upgrade.BucketReindexAuto, "projection@v1", 1); err != nil {
		t.Fatalf("a re-projection cannot start after the panic: %v", err)
	}
}

// A panic before the run holds the status must not fail another run's.
func TestHandle_APanicBeforeTheStatusLeavesAnotherRunsAlone(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	mgr := upgrade.NewManager("")
	if err := mgr.StartTarget(upgrade.BucketReindexAuto, "projection@v1", 1); err != nil {
		t.Fatal(err)
	}
	f.job.Progress = mgr
	f.svc.panicLocality = true
	if _, err := f.job.Handle(context.Background(), &storage.Job{ID: "j"}); err == nil {
		t.Fatal("want an error")
	}
	if st := mgr.Snapshot(); st.State != upgrade.StateInProgress || st.Bucket != upgrade.BucketReindexAuto {
		t.Fatalf("another run's status = %+v", st)
	}
}

// Start-up must survive a decision that panics.
func TestSchedule_APanicDoesNotStopStartUp(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	f.svc.panicLocality = true
	j, err := f.job.Schedule(context.Background(), jobs.NewQueue(f.drv.Jobs()))
	if err == nil || j != nil {
		t.Fatalf("Schedule = %v, %v; want an error and no panic", j, err)
	}
}

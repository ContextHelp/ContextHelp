package rederive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// The survey is one grouped query and one decision per group, whatever
// the number of stale rows.
func TestSurvey_OneDecisionPerGroup(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 60; i++ {
		f.add(fmt.Sprintf("a%02d", i), "alpha@v1", alphaOld)
	}
	for i := 0; i < 40; i++ {
		f.add(fmt.Sprintf("b%02d", i), "alpha", alphaOld)
	}
	q := jobs.NewQueue(f.drv.Jobs())
	if j, err := f.job.Schedule(context.Background(), q); err != nil || j == nil {
		t.Fatalf("job=%v err=%v", j, err)
	}
	// Two groups (two stored stamps), each judged against the stored and
	// no further plan: 2 locality calls, not 100.
	if f.svc.localityCalls != 2 {
		t.Fatalf("locality calls = %d, want 2", f.svc.localityCalls)
	}
}

// Groups whose pipeline is not registered, or whose stored signature has
// no recorded entries, are deferred with that reason: no job, no record,
// shown on the status. They become eligible by themselves.
func TestSchedule_UnjudgeableGroupsAreDeferredAndRecoverByThemselves(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	q := jobs.NewQueue(f.drv.Jobs())
	prog := &fakeProgress{}
	f.job.Progress = prog
	f.add("ghost", "ghost@v1", alphaOld)
	f.add("noentries", "alpha@v1", "alpha#unrecorded")
	f.svc.perrs["ghost"] = fmt.Errorf("no pipeline registered")

	for start := 1; start <= 2; start++ {
		if j, err := f.job.Schedule(ctx, q); err != nil || j != nil {
			t.Fatalf("start %d: job=%v err=%v", start, j, err)
		}
	}
	if len(prog.deferred) != 2 || !strings.Contains(prog.deferred[0].Reason+prog.deferred[1].Reason, "no recorded entries") {
		t.Fatalf("deferred = %+v", prog.deferred)
	}
	if len(f.skips(alphaNew))+len(f.skips(alphaOld))+len(f.skips("alpha#unrecorded")) != 0 {
		t.Fatal("a deferred group must write no record")
	}

	// The entries are recorded: the group runs.
	if err := f.store.UpsertDerivationSignature(ctx, "alpha#unrecorded", "alpha", []string{"a@1", "b@0"}); err != nil {
		t.Fatal(err)
	}
	if j, err := f.job.Schedule(ctx, q); err != nil || j == nil {
		t.Fatalf("after recording: job=%v err=%v", j, err)
	}
	res, err := f.run()
	if err != nil || res.Processed != 1 || f.sigOf("noentries") != alphaNew {
		t.Fatalf("run = %+v, %v", res, err)
	}

	// The pipeline is registered: the other runs.
	delete(f.svc.perrs, "ghost")
	f.svc.plans["ghost"] = localPlan("a", "b")
	if err := f.reg.Register("ghost", &pipeline.Pipeline{PipelineName: "ghost", Signature: "ghost#2", Entries: []string{"a@1", "b@2"},
		BaselineSignature: alphaOld, BaselineEntries: alphaOldEntries}); err != nil {
		t.Fatal(err)
	}
	res, err = f.run()
	if err != nil || res.Processed != 1 || f.sigOf("ghost") != "ghost#2" {
		t.Fatalf("run = %+v, %v, sig %q", res, err, f.sigOf("ghost"))
	}
}

// With the job disabled nothing is scheduled or written, and one line says
// how many objects are stale. A job left pending, or requeued by crash
// recovery, does nothing either.
func TestDisabled_NothingScheduledAndAPendingJobDoesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	q := jobs.NewQueue(f.drv.Jobs())
	f.add("o1", "alpha@v1", alphaOld)
	f.add("o2", "alpha@v1", alphaOld)
	pending, err := f.job.Schedule(ctx, q)
	if err != nil || pending == nil {
		t.Fatalf("enabled: job=%v err=%v", pending, err)
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	f.job.Enabled = false

	if j, err := f.job.Schedule(ctx, q); err != nil || j != nil {
		t.Fatalf("disabled: job=%v err=%v", j, err)
	}
	if out := buf.String(); strings.Count(out, "rederive: disabled by upgrade.rederive.enabled; stale objects") != 1 || !strings.Contains(out, "stale=2") {
		t.Fatalf("log = %q", out)
	}
	msg, err := f.job.Handle(ctx, pending)
	if err != nil || msg == "" {
		t.Fatalf("Handle = %q, %v", msg, err)
	}
	if len(f.svc.called()) != 0 || f.sigOf("o1") != alphaOld || f.stale() != 2 {
		t.Fatalf("a disabled job touched the rows: calls %v", f.svc.called())
	}
}

// A pipeline that only lost steps (skip_steps) applies nothing: the rows
// are stamped current, nothing runs.
func TestRun_RemovedStepsOnlyStampsWithoutRunning(t *testing.T) {
	f := newFixture(t)
	f.reg.Upsert("alpha", &pipeline.Pipeline{PipelineName: "alpha", Signature: alphaNew, Entries: []string{"a@1"},
		BaselineSignature: alphaOld, BaselineEntries: alphaOldEntries})
	f.add("o1", "alpha@v1", alphaOld)
	f.add("o2", "alpha@v1", alphaOld)
	prog := &fakeProgress{}
	f.job.Progress = prog
	res, err := f.run()
	if err != nil || res.Stamped != 2 || res.Processed != 0 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if len(f.svc.called()) != 0 {
		t.Fatalf("re-derived %v: nothing should run", f.svc.called())
	}
	if f.sigOf("o1") != alphaNew || f.sigOf("o2") != alphaNew || f.stale() != 0 {
		t.Fatal("rows not stamped current")
	}
	if prog.done == nil || prog.done.Processed != 2 {
		t.Fatalf("summary = %+v", prog.done)
	}
}

// An added step that neither mode runs (pruned, left out) would be
// stamped without having run: the group is deferred instead.
func TestRun_AStepNoModeRunsDefersTheGroup(t *testing.T) {
	f := newFixture(t)
	f.reg.Upsert("alpha", &pipeline.Pipeline{PipelineName: "alpha", Signature: alphaNew,
		Entries: []string{"a@1", "b@1", "c@1"}, BaselineSignature: alphaOld, BaselineEntries: alphaOldEntries})
	f.add("o1", "alpha@v1", alphaOld)
	res, err := f.run()
	if err != nil || res.Processed != 0 || len(res.Deferred) != 1 || !strings.Contains(res.Deferred[0].Reason, "step c") {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if len(f.svc.called()) != 0 || f.sigOf("o1") != alphaOld {
		t.Fatal("a deferred group must stay as stored")
	}
}

// The objects are stamped with the signature of the pipeline the plan ran
// (the highest registered @vN), not of the bare registration.
func TestRun_StampsTheSignatureOfThePipelineThePlanRan(t *testing.T) {
	f := newFixture(t)
	const v2 = "alpha#00000000000000v2"
	v2Entries := []string{"a@1", "b@3"}
	if err := f.reg.Register("alpha@v2", &pipeline.Pipeline{PipelineName: "alpha@v2", Signature: v2, Entries: v2Entries,
		BaselineSignature: alphaOld, BaselineEntries: alphaOldEntries}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpsertDerivationSignature(context.Background(), v2, "alpha", v2Entries); err != nil {
		t.Fatal(err)
	}
	plan := localPlan("a", "b")
	plan.Pipeline = "alpha@v2"
	f.svc.plans["alpha"] = plan
	f.add("o1", "alpha@v1", alphaOld)
	res, err := f.run()
	if err != nil || res.Processed != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if got := f.sigOf("o1"); got != v2 {
		t.Fatalf("stamped %q, want the signature of alpha@v2 (%q), not the bare registration's %q", got, v2, alphaNew)
	}
}

// Rows that arrive during a run must not push done past total.
func TestRun_DoneNeverExceedsTotal(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	f.add("o2", "alpha@v1", alphaOld)
	added := false
	f.svc.onCall = func(string) {
		if !added {
			added = true
			f.add("o3", "alpha@v1", alphaOld)
		}
	}
	prog := &fakeProgress{}
	f.job.Progress = prog
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	for _, tk := range prog.ticks {
		if tk[0] > prog.total || tk[1] > tk[0] {
			t.Fatalf("tick %v with total %d", tk, prog.total)
		}
	}
}

func TestRun_DeferredIsReportedWhenNothingRuns(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	plan := localPlan("a", "b")
	plan.Locality = pipeline.LocalityHosted
	f.svc.plans["alpha"] = plan
	prog := &fakeProgress{}
	f.job.Progress = prog
	q := jobs.NewQueue(f.drv.Jobs())
	if j, err := f.job.Schedule(context.Background(), q); err != nil || j != nil {
		t.Fatalf("Schedule: job=%v err=%v: an all-deferred store schedules nothing", j, err)
	}
	if len(prog.deferred) != 1 || prog.deferred[0].Pipeline != "alpha" {
		t.Fatalf("deferred after Schedule = %+v", prog.deferred)
	}
	prog.deferred = nil
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if len(prog.started) != 0 {
		t.Fatalf("a run that does nothing started the status: %v", prog.started)
	}
	if len(prog.deferred) != 1 {
		t.Fatalf("deferred after the run = %+v", prog.deferred)
	}
}

func TestRun_SkipCountsReachTheStatus(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	f.add("o2", "alpha@v1", alphaOld)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		if id == "o1" {
			return fmt.Errorf("x: %w", upgrade.ErrCannotReanalyze)
		}
		return nil
	}
	prog := &fakeProgress{}
	f.job.Progress = prog
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if prog.skipped[storage.SkipRefused] != 1 {
		t.Fatalf("skipped = %v", prog.skipped)
	}
}

// Stamped objects appear in the handler's message and in the event, not
// only in the status.
func TestHandle_MessageAndEventCarryStamped(t *testing.T) {
	f := newFixture(t)
	f.reg.Upsert("alpha", &pipeline.Pipeline{PipelineName: "alpha", Signature: alphaNew, Entries: []string{"a@1"},
		BaselineSignature: alphaOld, BaselineEntries: alphaOldEntries})
	f.add("o1", "alpha@v1", alphaOld)
	f.add("o2", "alpha@v1", alphaOld)
	b := &recordingBus{}
	f.job.Bus = b
	msg, err := f.job.Handle(context.Background(), &storage.Job{ID: "j"})
	if err != nil || !strings.Contains(msg, "stamped 2") {
		t.Fatalf("Handle = %q, %v", msg, err)
	}
	if len(b.events) != 1 || b.events[0].Stamped != 2 {
		t.Fatalf("events = %+v", b.events)
	}
}

// deleting removes object id the way a user's delete does, during a run.
func (f *fixture) deleting(id string, then error) func(string, service.ReanalyzeOptions) error {
	return func(got string, _ service.ReanalyzeOptions) error {
		if got != id {
			return nil
		}
		if err := f.drv.Objects().Delete(context.Background(), id); err != nil {
			f.t.Fatal(err)
		}
		return then
	}
}

// An object deleted during the run is dropped: no try, no strike, no
// skip, no error, no failed status; the run goes on.
func TestRun_AnObjectDeletedMidRunIsDropped(t *testing.T) {
	cases := map[string]error{
		"the service reports it gone":             fmt.Errorf("reanalyze: load %q: %w: %w", "o1", upgrade.ErrObjectGone, storage.ErrNotFound),
		"an unclassified failure, then the try":   errors.New("database is locked"),
		"a transient failure, then the try":       fetchErr("unexpected status 503"),
		"a refusal, then the skip write":          fmt.Errorf("x: %w", upgrade.ErrCannotReanalyze),
		"a provider failure, then the try":        llmFailure("o1"),
		"an unusable reply, then the strike":      unusableErr("o1"),
		"success, then the stamp finds it absent": nil,
	}
	for name, then := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			addN(f, 3)
			prog := &fakeProgress{}
			f.job.Progress = prog
			f.svc.result = f.deleting("o1", then)
			res, err := f.run()
			if err != nil || res.Interrupted || prog.failed != nil {
				t.Fatalf("run = %+v, %v, status failed %v", res, err, prog.failed)
			}
			if res.Gone != 1 || res.Processed != 2 {
				t.Fatalf("result = %+v: want 1 gone and the others processed", res)
			}
			if len(f.skips(alphaNew)) != 0 || res.Transient != 0 || res.SkippedTotal() != 0 {
				t.Fatalf("a deleted object left records: skips %v, %+v", f.skips(alphaNew), res)
			}
		})
	}
}

// Rows of a target that is no longer produced are dead and go at start; the
// current target's stay.
func TestSchedule_PrunesRowsOfDeadTargets(t *testing.T) {
	f := newFixture(t)
	f.add("o1", "alpha@v1", alphaOld)
	ctx := context.Background()
	for _, target := range []string{alphaNew, "alpha#dead"} {
		if err := f.store.RecordRederiveTry(ctx, "o1", target); err != nil {
			t.Fatal(err)
		}
		if err := f.store.RecordRederiveSkip(ctx, storage.RederiveSkip{ObjectID: "o1", TargetSig: target, Reason: storage.SkipRefused}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.job.Schedule(ctx, jobs.NewQueue(f.drv.Jobs())); err != nil {
		t.Fatal(err)
	}
	if len(skipRows(t, f.drv, "alpha#dead")) != 0 || len(skipRows(t, f.drv, alphaNew)) != 1 {
		t.Fatalf("dead %v, current %v", skipRows(t, f.drv, "alpha#dead"), skipRows(t, f.drv, alphaNew))
	}
	// Disabled, nothing is written.
	f.job.Enabled = false
	if err := f.store.RecordRederiveSkip(ctx, storage.RederiveSkip{ObjectID: "o1", TargetSig: "alpha#dead2", Reason: storage.SkipRefused}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.job.Schedule(ctx, jobs.NewQueue(f.drv.Jobs())); err != nil {
		t.Fatal(err)
	}
	if len(skipRows(t, f.drv, "alpha#dead2")) != 1 {
		t.Fatal("a disabled job pruned")
	}
}

// A not-found that is not the object lookup (a blob, an edge) does not say
// the object is gone: it is classified like any other error, a try.
func TestRun_ANotFoundThatIsNotTheObjectIsNotGone(t *testing.T) {
	f := newFixture(t)
	addN(f, 2)
	f.svc.result = func(id string, _ service.ReanalyzeOptions) error {
		return fmt.Errorf("reanalyze: read blob of %q: %w", id, storage.ErrNotFound)
	}
	res, err := f.run()
	if err != nil || res.Gone != 0 || res.Transient != 2 {
		t.Fatalf("run = %+v, %v: a blob not found must be a try, not a gone object", res, err)
	}
}

// A pipeline that cannot be built at this start (its provider is not up
// yet) is known by its baseline only; the strikes and skips it earned for
// its last real signature must survive the start, or every provider flap
// would reset them. A target no definition produces is pruned.
func TestSchedule_KeepsRowsOfAPipelineUnavailableAtThisStart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.add("o1", "alpha@v1", alphaOld)
	// beta was built at an earlier start (its signature and entries were
	// recorded); now the registry holds only its baseline.
	if err := f.store.UpsertDerivationSignature(ctx, "beta#current", "beta", []string{"x@2"}); err != nil {
		t.Fatal(err)
	}
	pipeline.MarkBaseline(f.reg, "beta", "beta#baseline", []string{"x@1"})
	for _, target := range []string{"beta#current", "nobody#produces"} {
		if err := f.store.RecordRederiveSkip(ctx, storage.RederiveSkip{ObjectID: "o1", TargetSig: target, Reason: storage.SkipRefused}); err != nil {
			t.Fatal(err)
		}
		if err := f.store.RecordRederiveTry(ctx, "o1", target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.job.Schedule(ctx, jobs.NewQueue(f.drv.Jobs())); err != nil {
		t.Fatal(err)
	}
	if len(skipRows(t, f.drv, "beta#current")) != 1 {
		t.Error("the skip of an unavailable pipeline's signature was pruned")
	}
	if len(skipRows(t, f.drv, "nobody#produces")) != 0 {
		t.Error("a target no definition produces was kept")
	}
	var tries int
	if err := sqliteDB(t, f.drv).QueryRow(`SELECT COUNT(*) FROM rederive_attempts WHERE target_sig = 'beta#current'`).Scan(&tries); err != nil || tries != 1 {
		t.Errorf("attempt rows of the unavailable pipeline = %d, %v", tries, err)
	}
}

// A pipeline that cannot be built at this start may be known by a versioned
// name; its rows are kept by the base name derivation_signatures records.
func TestSchedule_KeepsRowsOfAnUnbuiltPipelineKnownByAVersionedName(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.add("o1", "alpha@v1", alphaOld)
	if err := f.store.UpsertDerivationSignature(ctx, "gamma#current", "gamma", []string{"x@2"}); err != nil {
		t.Fatal(err)
	}
	pipeline.MarkBaseline(f.reg, "gamma@v2", "gamma#baseline", []string{"x@1"})
	if err := f.store.RecordRederiveSkip(ctx, storage.RederiveSkip{ObjectID: "o1", TargetSig: "gamma#current", Reason: storage.SkipRefused}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.job.Schedule(ctx, jobs.NewQueue(f.drv.Jobs())); err != nil {
		t.Fatal(err)
	}
	if len(skipRows(t, f.drv, "gamma#current")) != 1 {
		t.Fatal("the skip of a pipeline known by a versioned name was pruned")
	}
}

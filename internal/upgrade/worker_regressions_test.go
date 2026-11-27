package upgrade

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
)

// One object failing is recorded with its id and counted, and the run
// goes on to the others and completes.
func TestWorkerRegressions_FailedObjectIsRecordedAndRunContinues(t *testing.T) {
	db := fixtureDB(t)
	if _, err := db.Exec("INSERT INTO objects (id, pipeline, graph_json, created_at) VALUES ('obj-f', 'text.short@v0', '{}', '2026-05-06T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	var mu sync.Mutex
	var completed []events.UpgradeReingestCompletedPayload
	var failedRuns int
	b.Subscribe(string(events.TopicDpkmsUpgradeReingestCompleted), func(_ context.Context, e bus.Event) error {
		mu.Lock()
		defer mu.Unlock()
		completed = append(completed, e.Payload.(events.UpgradeReingestCompletedPayload))
		return nil
	})
	b.Subscribe(string(events.TopicDpkmsUpgradeReingestFailed), func(context.Context, bus.Event) error {
		mu.Lock()
		defer mu.Unlock()
		failedRuns++
		return nil
	})

	mgr := NewManager("")
	fake := &fakeReanalyzer{errs: []error{nil, errors.New("step exploded fetching https://h.example/x?sig=SECRET"), nil}}
	if err := NewWorker(db, fake, mgr, b).Run(context.Background(), sel, WorkerOpts{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := fake.CallCount(); got != 3 {
		t.Fatalf("calls = %d, want 3: the run stopped at the failing object", got)
	}
	if got := mgr.Snapshot().State; got != StateIdle {
		t.Errorf("state = %q, want idle (completed)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if failedRuns != 0 {
		t.Errorf("%d run-failed events, want 0", failedRuns)
	}
	if len(completed) != 1 {
		t.Fatalf("%d completed events, want 1", len(completed))
	}
	c := completed[0]
	if c.Done != 3 || c.Failed != 1 {
		t.Errorf("done/failed = %d/%d, want 3/1", c.Done, c.Failed)
	}
	if len(c.Failures) != 1 || c.Failures[0].ObjectID != fake.calls[1] || c.Failures[0].Reason != "step exploded fetching https://h.example/x" {
		t.Errorf("failures = %+v, want object %q with reason", c.Failures, fake.calls[1])
	}
}

// A cancel that surfaces as an object's error still ends the run.
func TestWorkerRegressions_CancelDuringFailingObjectEndsRun(t *testing.T) {
	db := fixtureDB(t)
	sel, _ := ParseSelector("pipeline=text.short@v0")
	ctx, cancel := context.WithCancel(context.Background())
	fake := &cancelingReanalyzer{cancel: cancel}
	mgr := NewManager("")
	err := NewWorker(db, fake, mgr, nil).Run(ctx, sel, WorkerOpts{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := mgr.Snapshot().State; got != StateFailed {
		t.Errorf("state = %q, want failed", got)
	}
	if fake.calls != 1 {
		t.Errorf("calls = %d, want 1", fake.calls)
	}
}

type cancelingReanalyzer struct {
	cancel func()
	calls  int
}

func (c *cancelingReanalyzer) ReanalyzeObject(ctx context.Context, id string) (string, float64, error) {
	c.calls++
	c.cancel()
	time.Sleep(time.Millisecond)
	return "", 0, errors.New("failed while cancelled")
}

// A failure reason is bounded and carries no URL query string (fetch
// errors embed signed URLs).
func TestWorkerRegressions_FailureReasonIsBoundedAndHasNoURLQuery(t *testing.T) {
	long := strings.Repeat("x", 2000)
	err := fmt.Errorf("fetch https://example.com/a/b?X-Amz-Signature=SECRET&token=abc#frag failed: %s", long)
	got := failureReason(err)
	if strings.Contains(got, "SECRET") || strings.Contains(got, "token=") || strings.Contains(got, "frag") {
		t.Errorf("query leaked: %q", got[:120])
	}
	if !strings.Contains(got, "https://example.com/a/b") {
		t.Errorf("url path lost: %q", got[:120])
	}
	if n := len([]rune(got)); n > maxFailureReason+3 {
		t.Errorf("reason has %d runes, want <= %d", n, maxFailureReason+3)
	}
}

// The run's processed, failed and skipped counts are readable after it
// completed, and the completed event carries skipped.
func TestWorkerRegressions_SummaryAfterCompletion(t *testing.T) {
	db := fixtureDB(t)
	if _, err := db.Exec("INSERT INTO objects (id, pipeline, graph_json, created_at) VALUES ('obj-f', 'text.short@v0', '{}', '2026-05-06T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	sel, _ := ParseSelector("pipeline=text.short@v0")
	b := bus.New()
	var skipped int
	b.Subscribe(string(events.TopicDpkmsUpgradeReingestCompleted), func(_ context.Context, e bus.Event) error {
		skipped = e.Payload.(events.UpgradeReingestCompletedPayload).Skipped
		return nil
	})
	fake := &fakeReanalyzer{errs: []error{nil, errors.New("boom"), fmt.Errorf("x: %w", ErrCannotReanalyze)}}
	w := NewWorker(db, fake, NewManager(""), b)
	if err := w.Run(context.Background(), sel, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	if got, ran := w.LastRun(); !ran || got != (RunSummary{Processed: 1, Failed: 1, Skipped: 1}) {
		t.Errorf("LastRun = %+v (ran %v)", got, ran)
	}
	if skipped != 1 {
		t.Errorf("event skipped = %d, want 1", skipped)
	}
}

// URL userinfo and the query of any scheme are removed from a reason.
func TestWorkerRegressions_FailureReasonStripsUserinfoAndNonHTTPQuery(t *testing.T) {
	secret := strings.Join([]string{"tok", "en", "value"}, "") // built at runtime: no literal
	for _, in := range []string{
		"fetch https://user:" + secret + "@host.example/a/b failed",
		"dial s3://bucket/key?X-Sig=" + secret + " failed",
		"clone git+ssh://deploy:" + secret + "@git.example/r.git?ref=" + secret + " failed",
	} {
		got := failureReason(errors.New(in))
		if strings.Contains(got, secret) {
			t.Errorf("secret leaked from %q: %q", in, got)
		}
		if !strings.Contains(got, "://") || !strings.HasSuffix(got, " failed") {
			t.Errorf("reason mangled: %q", got)
		}
	}
}

// An over-budget run reports the counts it reached.
func TestWorkerRegressions_BudgetExceededKeepsCounts(t *testing.T) {
	db := fixtureDB(t)
	sel, _ := ParseSelector("pipeline=text.short@v0")
	fake := &fakeReanalyzer{costs: []float64{0.10, 0.10}}
	w := NewWorker(db, fake, NewManager(""), nil)
	if err := w.Run(context.Background(), sel, WorkerOpts{BudgetUSD: 0.05}); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}
	if got, ran := w.LastRun(); !ran || got != (RunSummary{Processed: 1}) {
		t.Errorf("LastRun = %+v (ran %v), want 1 processed", got, ran)
	}
}

// A Run that returns before a run started has no summary: not a previous
// run's, not zeros.
func TestWorkerRegressions_EarlyReturnHasNoSummary(t *testing.T) {
	db := fixtureDB(t)
	sel, _ := ParseSelector("pipeline=text.short@v0")
	mgr := NewManager("")
	w := NewWorker(db, &fakeReanalyzer{}, mgr, nil)
	if err := w.Run(context.Background(), sel, WorkerOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, ran := w.LastRun(); !ran {
		t.Fatal("the first run should have a summary")
	}
	if err := mgr.Start(BucketReindexAuto, 1); err != nil {
		t.Fatal(err)
	}
	defer mgr.Complete()
	if err := w.Run(context.Background(), sel, WorkerOpts{}); !errors.Is(err, ErrAlreadyInProgress) {
		t.Fatalf("err = %v, want ErrAlreadyInProgress", err)
	}
	if sum, ran := w.LastRun(); ran {
		t.Errorf("LastRun reports a run that did not happen: %+v", sum)
	}
}

// Worker — selective re-ingest worker for ADR-070
// reingest_selective upgrades.
//
// Lifecycle:
//
//	mgr.Start(BucketReingestSelective, total)
//	for id := range sel.IterateMatching(ctx, db):
//	    cost, err := svc.ReanalyzeObject(ctx, id)
//	    if err: mgr.Fail(err); return
//	    if running cost > BudgetUSD: mgr.Fail(ErrBudgetExceeded); return
//	    mgr.Tick(done)
//	    publish progress event every total/100 objects
//	mgr.Complete()
//
// All work is in-process and synchronous. Cancelling ctx aborts cleanly:
// the manager moves to "failed" with reason "context cancelled" so the
// `ctxt upgrade status` banner reflects the abort.

package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"hop.top/kit/go/runtime/bus"
)

// reanalyzer is the subset of *service.Service.ReanalyzeObject the worker
// depends on. Defined as an interface so worker_test.go can wire a fake
// without spinning up the full service stack (which would require a real
// SQLite driver, a queue, and a registry). Production callers pass
// service.Service directly — its ReanalyzeObject signature satisfies this
// shape per Go's structural typing.
type reanalyzer interface {
	ReanalyzeObject(ctx context.Context, id string) (string, float64, error)
}

// ErrBudgetExceeded signals that the cumulative LLM cost has surpassed
// WorkerOpts.BudgetUSD. The manager state goes to "failed" with this as
// the LastError.
var ErrBudgetExceeded = errors.New("upgrade run: LLM budget exceeded")

// ErrAlreadyInProgress signals that another upgrade run is already in
// flight on this Manager — the worker refuses to start a second one.
var ErrAlreadyInProgress = errors.New("upgrade run: another upgrade is already in progress")

// Worker drives a Selector's matching set through Service.ReanalyzeObject
// while keeping the in-memory upgrade state machine in sync.
//
// The worker is deliberately lean: it owns no goroutines (the Selector's
// iterator owns one) and no timers beyond the rate-limit sleep. Cancellation
// is via the ctx passed to Run.
type Worker struct {
	db  *sql.DB
	svc reanalyzer
	mgr *Manager
	bus events.Bus
}

// NewWorker constructs a Worker. db must be the same *sql.DB the storage
// driver uses (so the Selector queries match the live corpus). svc is the
// in-process service.Service.
func NewWorker(db *sql.DB, svc reanalyzer, mgr *Manager, busArg events.Bus) *Worker {
	return &Worker{
		db:  db,
		svc: svc,
		mgr: mgr,
		bus: busArg,
	}
}

// WorkerOpts configures a single Worker.Run invocation.
type WorkerOpts struct {
	// DryRun: skip ReanalyzeObject calls; just walk the selector and
	// report counts. Still emits a `dpkms.upgrade.plan.computed` event.
	DryRun bool
	// RateLimit: max ReanalyzeObject calls per second. 0 = unbounded.
	// Implemented as a sleep between iterations rather than a leaky
	// bucket — good enough for operator-scale runs (worst case: <1000
	// objects/sec).
	RateLimit int
	// BudgetUSD: cumulative LLM cost ceiling. The worker tracks the sum
	// of llmCostUSD returned from ReanalyzeObject; when it exceeds this
	// value the run aborts with ErrBudgetExceeded. 0 = unbounded.
	BudgetUSD float64
}

// Run executes the selector's matching set. Blocks until done, ctx is
// cancelled, or the budget is exceeded. The Manager's state reflects the
// outcome:
//   - completed: state → idle (shadow file removed).
//   - cancelled: state → failed with reason "context cancelled".
//   - budget exceeded: state → failed with ErrBudgetExceeded as LastError.
//   - any other error: state → failed with the underlying error.
//
// Returns a non-nil error in every failure case so the caller sees the
// concrete reason.
func (w *Worker) Run(ctx context.Context, sel *Selector, opts WorkerOpts) error {
	if sel == nil {
		return errors.New("upgrade run: nil selector")
	}
	if w.mgr == nil {
		return errors.New("upgrade run: nil manager")
	}

	total, err := sel.CountMatching(ctx, w.db)
	if err != nil {
		return fmt.Errorf("upgrade run: count: %w", err)
	}

	// DryRun short-circuits before touching the manager — a plan never
	// occupies the upgrade-state slot, so plan-while-running is allowed.
	if opts.DryRun {
		w.publish(ctx, events.TopicDpkmsUpgradePlanComputed, events.UpgradePlanComputedPayload{
			Bucket:    string(BucketReingestSelective),
			PlanItems: map[string]int{sel.Raw(): total},
			Total:     total,
		})
		return nil
	}

	if err := w.mgr.Start(BucketReingestSelective, total); err != nil {
		// Distinguish "already running" from other start failures so
		// callers can surface the right CLI message.
		if errors.Is(err, ErrBusy) {
			return ErrAlreadyInProgress
		}
		return fmt.Errorf("upgrade run: start: %w", err)
	}

	w.publish(ctx, events.TopicDpkmsUpgradeReingestStarted, events.UpgradeReingestStartedPayload{
		Selector: sel.Raw(),
		Total:    total,
	})

	startedAt := time.Now()
	progressEvery := total / 100
	if progressEvery < 1 {
		progressEvery = 1
	}

	run := &reingestRun{w: w, sel: sel, total: total}
	ids, iterErrs, err := sel.IterateMatching(ctx, w.db)
	if err != nil {
		run.fail(ctx, fmt.Errorf("iterate: %w", err), err.Error())
		return fmt.Errorf("upgrade run: iterate: %w", err)
	}

	var sleepInterval time.Duration
	if opts.RateLimit > 0 {
		sleepInterval = time.Second / time.Duration(opts.RateLimit)
	}

	for {
		select {
		case <-ctx.Done():
			run.fail(ctx, fmt.Errorf("context cancelled: %w", ctx.Err()), ctx.Err().Error())
			return ctx.Err()
		case id, ok := <-ids:
			if !ok {
				goto completed
			}
			if err := run.step(ctx, id, opts, progressEvery); err != nil {
				return err
			}

			if sleepInterval > 0 {
				timer := time.NewTimer(sleepInterval)
				select {
				case <-ctx.Done():
					timer.Stop()
				case <-timer.C:
				}
			}
		}
	}

completed:
	// The id channel closing does not by itself mean iteration finished: a
	// driver error truncates the stream the same way a clean end does. Fail
	// the run rather than reporting a partial reingest as complete.
	if iterErr := <-iterErrs; iterErr != nil {
		run.fail(ctx, iterErr, iterErr.Error())
		return fmt.Errorf("upgrade run: iterate: %w", iterErr)
	}
	if err := w.mgr.Complete(); err != nil {
		return fmt.Errorf("upgrade run: complete: %w", err)
	}
	w.publish(ctx, events.TopicDpkmsUpgradeReingestCompleted, events.UpgradeReingestCompletedPayload{
		Selector:   sel.Raw(),
		Done:       run.done,
		Total:      total,
		CostUSD:    run.costUSD,
		DurationMs: time.Since(startedAt).Milliseconds(),
	})
	return nil
}

// reingestRun is one Run's progress, carried into its events.
type reingestRun struct {
	w       *Worker
	sel     *Selector
	total   int
	done    int
	costUSD float64
}

// step reanalyzes one object, charges its cost against the budget and
// reports progress. A non-nil error ends the run; the manager and the
// failure event already record it.
func (r *reingestRun) step(ctx context.Context, id string, opts WorkerOpts, progressEvery int) error {
	_, cost, err := r.w.svc.ReanalyzeObject(ctx, id)
	if err != nil {
		r.fail(ctx, err, err.Error())
		return fmt.Errorf("upgrade run: reanalyze %s: %w", id, err)
	}
	r.costUSD += cost
	r.done++

	if opts.BudgetUSD > 0 && r.costUSD > opts.BudgetUSD {
		r.fail(ctx, ErrBudgetExceeded, ErrBudgetExceeded.Error())
		return ErrBudgetExceeded
	}

	if err := r.w.mgr.Tick(r.done); err != nil {
		// ErrNotRunning here means the manager was forcibly
		// reset out from under us — bail rather than silently
		// continuing without progress reporting.
		return fmt.Errorf("upgrade run: tick: %w", err)
	}

	if r.done%progressEvery == 0 || r.done == r.total {
		r.w.publish(ctx, events.TopicDpkmsUpgradeReingestProgress, events.UpgradeReingestProgressPayload{
			Selector:  r.sel.Raw(),
			Done:      r.done,
			Total:     r.total,
			CostUSD:   r.costUSD,
			BudgetUSD: opts.BudgetUSD,
		})
	}
	return nil
}

// fail moves the manager to failed with cause and publishes the failure
// event with reason and the progress so far.
func (r *reingestRun) fail(ctx context.Context, cause error, reason string) {
	// The run is already failing with cause; a manager that cannot record
	// it (not running, state write error) changes nothing the caller does.
	_ = r.w.mgr.Fail(cause)
	r.w.publish(ctx, events.TopicDpkmsUpgradeReingestFailed, events.UpgradeReingestFailedPayload{
		Selector: r.sel.Raw(),
		Done:     r.done,
		Total:    r.total,
		CostUSD:  r.costUSD,
		Reason:   reason,
	})
}

// publish wraps events.Bus.Publish with a CloudEvent envelope. nil bus is a
// no-op so tests / unconfigured callers don't crash on missing wiring.
func (w *Worker) publish(ctx context.Context, topic bus.Topic, payload any) {
	if w.bus == nil {
		return
	}
	ev, err := events.NewEvent("upgrade.worker", string(topic), payload)
	if err != nil {
		return
	}
	_ = w.bus.Publish(ctx, ev)
}

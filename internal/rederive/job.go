// Package rederive is the automatic re-derive job: when stored objects
// carry a derivation signature that is no longer current (a step version
// or a configured model changed), it re-runs their pipeline in the
// background, without the operator doing anything and without sending
// content anywhere it was not already going.
//
// Only pipelines whose work stays on this machine run; the others are
// deferred, reported, and left untouched. Each object ends in one of
// four ways: re-derived and stamped with the current signature; stamped
// without running anything, when the pipeline only lost steps; skipped
// with a durable record keyed by that signature (never retried until the
// signature changes); or left stale for the next start (an interrupted
// run, a failure that says nothing certain about the object, which only
// counts a try). A run is resumable because the outcomes live in the
// store: what is stamped is no longer stale, what is skipped is not
// listed, and the tries order what is left.
package rederive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// JobType is the task job type of a re-derive run.
const JobType = "derivation:rederive"

const (
	// failureStreak is how many objects in a row must fail on a provider
	// or a fetch that may succeed later before the run takes it for an
	// outage and stops. The attempts counted stand; the next start goes on.
	failureStreak = 5
	// maxStrikes is how many strikes an object may have for one target
	// before the next strike becomes a durable skip.
	maxStrikes = 3
	// maxDetail caps the error text kept with a skip.
	maxDetail = 200
	// maxRetries bounds automatic retries of a run that errors.
	maxRetries = 1
	// defaultBatch is the stale-row page size.
	defaultBatch = 200
	// defaultBusyPoll is how often a run retries claiming the upgrade
	// status while another run holds it.
	defaultBusyPoll = 5 * time.Second
)

const eventSource = "dpkms.upgrade.rederive"

// ErrUnclassifiedOutage is why a run stopped early: failureStreak objects in
// a row failed for a reason the job does not know (a storage error, an
// unwrapped step error). The last error is wrapped.
var ErrUnclassifiedOutage = errors.New("rederive: objects failing for an unknown reason; stopped, the next start resumes")

// ErrProviderOutage is why a run stopped early: failureStreak objects in
// a row failed on a provider.
var ErrProviderOutage = errors.New("rederive: provider failing; stopped, the next start resumes")

// ErrFetchOutage is why a run stopped early: failureStreak objects in a
// row failed on a fetch that may succeed later (a rate limit, a timeout,
// a refused connection, a server error).
var ErrFetchOutage = errors.New("rederive: fetches failing; stopped, the next start resumes")

// Progress is the upgrade-status surface (*upgrade.Manager).
type Progress interface {
	StartTarget(bucket upgrade.Bucket, target string, total int) error
	TickFailed(done, failed int) error
	SetDeferred(d []upgrade.Deferred) error
	SetSkipped(c map[string]int) error
	SetStopped(st []upgrade.Stopped) error
	CompleteRun(sum upgrade.RunSummary) error
	Fail(err error) error
}

var _ Progress = (*upgrade.Manager)(nil)

// Reanalyzer is the part of *service.Service the job drives.
type Reanalyzer interface {
	ReanalysisLocality(ctx context.Context, name string, opts service.ReanalyzeOptions) (service.ReanalysisPlan, error)
	ReanalyzeObjectWith(ctx context.Context, id string, opts service.ReanalyzeOptions) (string, float64, error)
}

// Store is what the job reads and writes besides the service.
type Store interface {
	storage.RederiveStore
	storage.DerivationStore
}

// Job runs re-derives. Zero-value optional fields: no status (Progress),
// no events (Bus), no pause (Pause), the default batch and busy poll.
type Job struct {
	Store    Store
	Svc      Reanalyzer
	Registry pipeline.Registry
	Progress Progress
	Bus      bus.Bus
	// Pause is the wait between two objects (upgrade.rederive.pause).
	Pause time.Duration
	// Wait sleeps for d or until ctx ends; nil is a timer. Tests inject
	// it so nothing sleeps.
	Wait func(ctx context.Context, d time.Duration) error
	// Enabled is upgrade.rederive.enabled: off, nothing is scheduled,
	// and a job already pending or requeued does nothing.
	Enabled  bool
	Batch    int
	BusyPoll time.Duration
	// Scope returns ctx as the system principal the service reads and
	// writes every row as; nil leaves ctx as it is.
	Scope func(ctx context.Context) context.Context
}

// Result summarises one run.
type Result struct {
	Target string `json:"target"`
	// Total is the objects the run set out to handle: the stale objects
	// of the groups it may run or stamp, the deferred ones not counted.
	Total     int `json:"total"`
	Processed int `json:"processed"`
	// Stamped objects needed no change (the pipeline only lost steps):
	// their signature was set to the current one without running anything.
	Stamped int `json:"stamped"`
	// Skipped counts the objects left as stored, by reason, each with a
	// durable record; Failed is the part of them that failed (fetch,
	// provider or other error) rather than being refused.
	Skipped map[string]int `json:"skipped,omitempty"`
	Failed  int            `json:"failed"`
	// Transient counts the failures that said nothing certain about the
	// object (a provider that is down, a rate limit, a timeout): each is
	// one more try, the object stays stale and is tried again at the next
	// start. Whether it also earns a strike towards a skip is decided when
	// the run ends (see settleStrikes).
	Transient int `json:"transient"`
	// Gone counts the objects deleted since they were listed: nothing is
	// recorded for them and the run goes on.
	Gone int `json:"gone"`
	// Deferred are the pipelines left alone because their work would not
	// stay on this machine; their objects get no record.
	Deferred []upgrade.Deferred `json:"deferred,omitempty"`
	// Stopped are the groups stopped on the way after five tries in a row:
	// the run went on with the next group; their remaining objects wait for
	// a later start.
	Stopped []upgrade.Stopped `json:"stopped,omitempty"`
	// Interrupted is set when the run stopped early: the objects after
	// the last one handled, and those of an outage, are for the next start.
	Interrupted bool `json:"interrupted,omitempty"`
}

// SkippedTotal is the number of skipped objects.
func (r Result) SkippedTotal() int {
	n := 0
	for _, c := range r.Skipped {
		n += c
	}
	return n
}

// Done is the number of objects the run handled.
func (r Result) Done() int { return r.Processed + r.Stamped + r.SkippedTotal() + r.Transient + r.Gone }

// failureReasons are the skip reasons that mean an error, not a refusal.
var failureReasons = []string{storage.SkipFetchFailed, storage.SkipProviderFailed, storage.SkipUnusableReply, storage.SkipFailed}

// Target is the upgrade-status target of a run: the signatures objects
// are brought to, as a short hash, so a different configuration shows as
// a different target.
func Target(current []string) string {
	sum := sha256.Sum256([]byte(strings.Join(current, "\n")))
	return "rederive@" + hex.EncodeToString(sum[:])[:12]
}

// RecordSignatures upserts the entries of every signature the registry
// can produce, each pipeline's current one and its baseline, so a later
// start can tell which step or model changed. It reports how many it
// recorded.
func RecordSignatures(ctx context.Context, store storage.RederiveStore, reg pipeline.Registry) (int, error) {
	n := 0
	for _, rec := range pipeline.SignatureRecords(reg) {
		if rec.Entries == nil {
			continue
		}
		if err := store.UpsertDerivationSignature(ctx, rec.Sig, rec.Pipeline, rec.Entries); err != nil {
			return n, fmt.Errorf("record signature entries: %w", err)
		}
		n++
	}
	return n, nil
}

// Handle is the TaskHandler for JobType. A provider outage ends the job
// without error: a retry now would meet the same outage, and the next
// start resumes.
func (j *Job) Handle(ctx context.Context, job *storage.Job) (msg string, err error) {
	// A panic must not take the daemon down, nor leave the job leased to
	// be claimed and to panic again at the next start: it ends the job as
	// failed, without a retry.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("rederive: run panicked; job failed", "job", job.ID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			msg, err = "", pipeline.Permanent(fmt.Errorf("rederive: run panicked: %v", r))
		}
	}()
	res, err := j.Run(ctx, job.ID)
	if err != nil {
		return "", err
	}
	msg = fmt.Sprintf("re-derived %d and stamped %d of %d objects; skipped %d, failed %d, retried %d, gone %d, deferred %d pipelines",
		res.Processed, res.Stamped, res.Total, res.SkippedTotal(), res.Failed, res.Transient, res.Gone, len(res.Deferred))
	if len(res.Stopped) > 0 {
		msg += fmt.Sprintf("; %d groups stopped after consecutive failures", len(res.Stopped))
	}
	return msg, nil
}

// Run re-derives the stale objects of every group whose work stays local.
// Five tries in a row stop a group, not the run: the run goes on with the
// next group and ends normally, with the stopped groups in the result. It
// returns an error only when the run itself breaks or is shut down; the
// result then holds what was reached.
func (j *Job) Run(ctx context.Context, jobID string) (res Result, err error) {
	if !j.Enabled {
		slog.Info("rederive: disabled by upgrade.rederive.enabled; nothing done", "job", jobID)
		return Result{}, nil
	}
	if j.Scope != nil {
		ctx = j.Scope(ctx)
	}
	x := &run{Job: j, jobID: jobID, start: time.Now(), failures: map[string]failure{}, proved: map[string]bool{}}
	// A panic frees the upgrade status this run holds, if it holds it:
	// left in progress it would show the daemon upgrading until restart and
	// make a re-projection wait for ever. A status held by another run is
	// not this run's to fail.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("rederive: run panicked", "job", jobID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			cause := fmt.Errorf("rederive: run panicked: %v", r)
			if x.owns && x.Progress != nil {
				_ = x.Progress.Fail(cause)
			}
			res, err = x.res, pipeline.Permanent(cause)
		}
	}()
	x.res.Target = Target(pipeline.CurrentSignatures(j.Registry))
	groups, err := j.survey(ctx)
	if err != nil {
		return x.res, err
	}
	x.groups = groups
	x.res.Deferred = deferredOf(groups)
	if x.Progress != nil {
		// Shown even when nothing runs: a run that defers everything
		// starts no status, and an empty list clears the last one.
		_ = x.Progress.SetDeferred(x.res.Deferred)
	}
	for _, d := range x.res.Deferred {
		slog.Warn("rederive: pipeline deferred; its objects stay as stored",
			"pipeline", d.Pipeline, "objects", d.Objects, "reason", d.Reason)
	}
	for _, g := range groups {
		if g.kind != kindDefer {
			x.res.Total += g.Objects
		}
	}
	if x.res.Total == 0 {
		x.finish(ctx)
		return x.res, nil
	}
	if err := x.begin(ctx); err != nil {
		return x.res, err
	}
	if err := x.process(ctx); err != nil {
		return x.stop(ctx, err)
	}
	x.finish(ctx)
	return x.res, nil
}

// run is one run's state.
type run struct {
	*Job
	jobID  string
	start  time.Time
	res    Result
	groups []*group
	// failures holds the objects that failed in this run, so that the try
	// they just got does not bring them back later in it, and so that the
	// run's end can turn them into strikes.
	failures map[string]failure
	// proved records what this run showed to work: "any" (an object was
	// re-derived), "fetched" (a fetch was accepted), "host:<host>" (one
	// addressed to that host was), "provider:<kind>" (an object was
	// re-derived and its reanalysis made a call of that provider kind).
	proved map[string]bool
	// owns: this run claimed the upgrade status and has not released it.
	owns bool
	// streak counts the failures that may succeed later, in a row.
	streak  int
	started bool // an object was re-derived: the next one waits Pause first
}

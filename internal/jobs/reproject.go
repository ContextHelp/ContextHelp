package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// Re-projection (ADR-070 reindex_auto for a projection change).
//
// When indexsig.ProjectionVersion changes, the FTS signature stops
// matching and every stored projected_fts_body is stale: it was derived by
// the old projection. dpkms startup verifies the signature and, on a
// mismatch, schedules one task job of ReprojectJobType. The job re-projects
// every stale object from its stored fields (body, FTS index entry,
// fts_indexed), then stamps the signature.
//
// Progress is the per-row projection_version stamp, not a cursor: each
// object is re-projected and stamped in one transaction, and every run
// lists only rows whose stamp differs from the current version. A run cut
// short (crash, shutdown, cancel) leaves its finished rows stamped and the
// signature unstamped, so the next start schedules the job again and it
// resumes where the stamps stop. Ingest stamps the rows it writes, so
// concurrent writes neither need re-projecting nor get reverted.
//
// Embeddings are not refreshed: the embedding text is also derived from
// the projection, but re-embedding is an embedding-model concern.

// ReprojectJobType is the task job type of a re-projection.
const ReprojectJobType = "projection:reproject"

// Re-projection defaults.
const (
	// DefaultReprojectBatch is the stale-ID page size.
	DefaultReprojectBatch = 200
	// defaultReprojectBusyPoll is how often a run retries claiming the
	// upgrade status while another run holds it.
	defaultReprojectBusyPoll = 5 * time.Second
	// reprojectMaxRetries bounds automatic retries of a run that errors.
	reprojectMaxRetries = 3
	// maxFailedReprojections caps the failed IDs carried in results and
	// events.
	maxFailedReprojections = 20
)

// reprojectEventSource is the CloudEvents source of re-projection events.
const reprojectEventSource = "dpkms.upgrade.reproject"

// ReprojectProgress is the ADR-070 upgrade-status surface (*upgrade.Manager).
type ReprojectProgress interface {
	StartTarget(bucket upgrade.Bucket, target string, total int) error
	TickFailed(done, failed int) error
	Complete() error
	Fail(err error) error
}

var _ ReprojectProgress = (*upgrade.Manager)(nil)

// ReprojectTarget is the upgrade-status target of a re-projection run:
// the projection version objects are brought to, in ADR-070's name@vN form.
func ReprojectTarget() string { return "projection@" + indexsig.ProjectionVersion }

// ReprojectResult summarises one run.
type ReprojectResult struct {
	ProjectionVersion string `json:"projection_version"`
	Total             int    `json:"total"`
	Reprojected       int    `json:"reprojected"`
	// Skipped objects were deleted, or re-projected by a concurrent
	// write, after they were listed.
	Skipped       int      `json:"skipped"`
	Failed        int      `json:"failed"`
	FailedObjects []string `json:"failed_objects,omitempty"`
}

// Done is the number of objects the run handled.
func (r ReprojectResult) Done() int { return r.Reprojected + r.Skipped + r.Failed }

// Reprojector runs re-projections. Zero-value optional fields: no status
// (Progress), no events (Bus), DefaultReprojectBatch (Batch), a 5s busy
// poll (BusyPoll).
type Reprojector struct {
	Store storage.ProjectionStore
	// Stamp stores the current FTS signature; called once every object
	// is re-projected.
	Stamp    func(ctx context.Context) error
	Progress ReprojectProgress
	Bus      events.Bus
	Batch    int
	BusyPoll time.Duration
}

// NewReprojector builds the Reprojector for a SQLite or Postgres driver.
func NewReprojector(driver storage.StorageDriver, progress ReprojectProgress, b events.Bus) (*Reprojector, error) {
	store, ok := driver.Objects().(storage.ProjectionStore)
	if !ok {
		return nil, fmt.Errorf("reproject: %T object store cannot re-project", driver.Objects())
	}
	r := &Reprojector{Store: store, Progress: progress, Bus: b}
	switch d := driver.(type) {
	case *sqlite.Driver:
		r.Stamp = func(ctx context.Context) error { return indexsig.StampFTS(ctx, d.DB(), indexsig.DialectSQLite) }
	case *postgres.Driver:
		r.Stamp = func(ctx context.Context) error { return indexsig.StampFTS(ctx, d.DB(), indexsig.DialectPostgres) }
	default:
		return nil, fmt.Errorf("reproject: %T has no FTS signature", driver)
	}
	return r, nil
}

// ScheduleReprojection enqueues the re-projection job when verify reports
// a mismatch against a stamped signature (a first boot has nothing to
// re-project). A job already pending or running (a resumed run) is
// returned instead of a second one. A nil job means nothing to do.
func ScheduleReprojection(ctx context.Context, q *Queue, verify *indexsig.VerifyResult, b events.Bus) (*storage.Job, error) {
	if verify == nil || verify.Match || verify.FirstBoot {
		return nil, nil
	}
	for _, status := range []storage.JobStatus{storage.JobRunning, storage.JobPending} {
		jobs, _, err := q.List(ctx, storage.JobFilter{Type: ReprojectJobType, Status: status, Limit: 1})
		if err != nil {
			return nil, fmt.Errorf("schedule reprojection: list jobs: %w", err)
		}
		if len(jobs) > 0 {
			return jobs[0], nil
		}
	}
	job, err := q.EnqueueTask(ctx, ReprojectJobType, "{}", reprojectMaxRetries)
	if err != nil {
		return nil, fmt.Errorf("schedule reprojection: %w", err)
	}
	publishReprojection(ctx, b, events.TopicDpkmsUpgradeReprojectionScheduled, events.ReprojectionPayload{
		ProjectionVersion: indexsig.ProjectionVersion,
		JobID:             job.ID,
	})
	return job, nil
}

// Handle is the TaskHandler for ReprojectJobType.
func (r *Reprojector) Handle(ctx context.Context, job *storage.Job) (string, error) {
	res, err := r.Run(ctx, job.ID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("reprojected %d of %d objects to %s (%d failed)",
		res.Reprojected, res.Total, ReprojectTarget(), res.Failed), nil
}

// Run re-projects every stale object and stamps the FTS signature when
// none is left. It returns an error only when the run stops early; objects
// that fail are counted, left stale, and leave the signature unstamped and
// the status failed, so the next start retries them.
func (r *Reprojector) Run(ctx context.Context, jobID string) (ReprojectResult, error) {
	x := &reprojectRun{Reprojector: r, jobID: jobID, start: time.Now()}
	x.res.ProjectionVersion = indexsig.ProjectionVersion
	if err := x.begin(ctx); err != nil {
		return x.res, err
	}
	x.publish(ctx, events.TopicDpkmsUpgradeReprojectionStarted, "")

	batch := r.Batch
	if batch <= 0 {
		batch = DefaultReprojectBatch
	}
	progressEvery := max(1, x.res.Total/100)
	cursor := ""
	for {
		ids, err := r.Store.ListStaleProjections(ctx, cursor, batch)
		if err != nil {
			if ctx.Err() != nil {
				return x.stop(ctx, interruptCause(ctx))
			}
			return x.stop(ctx, fmt.Errorf("list stale objects: %w", err))
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return x.stop(ctx, interruptCause(ctx))
			}
			cursor = id
			changed, err := r.Store.Reproject(ctx, id)
			if ctx.Err() != nil {
				// Cut off, not failed: the row is still stale (its
				// transaction rolled back) and the resumed run takes it.
				return x.stop(ctx, interruptCause(ctx))
			}
			x.count(id, changed, err)
			if x.res.Done()%progressEvery == 0 {
				x.publish(ctx, events.TopicDpkmsUpgradeReprojectionProgressed, "")
			}
		}
		if err := x.tick(); err != nil {
			return x.res, err
		}
	}
	return x.finish(ctx)
}

// reprojectRun is one run's state.
type reprojectRun struct {
	*Reprojector
	jobID   string
	start   time.Time
	res     ReprojectResult
	failure string // first per-object failure, for the status message
}

// begin counts the stale objects and claims the upgrade status, waiting
// while another upgrade run holds it.
func (x *reprojectRun) begin(ctx context.Context) error {
	poll := x.BusyPoll
	if poll <= 0 {
		poll = defaultReprojectBusyPoll
	}
	for {
		total, err := x.Store.CountStaleProjections(ctx)
		if err != nil {
			return fmt.Errorf("count stale objects: %w", err)
		}
		x.res.Total = total
		if x.Progress == nil {
			return nil
		}
		err = x.Progress.StartTarget(upgrade.BucketReindexAuto, ReprojectTarget(), total)
		if err == nil {
			return nil
		}
		if !errors.Is(err, upgrade.ErrBusy) {
			return fmt.Errorf("upgrade status: %w", err)
		}
		slog.Info("reproject: waiting for another upgrade run to finish", "err", err)
		select {
		case <-ctx.Done():
			return interruptCause(ctx)
		case <-time.After(poll):
		}
	}
}

func (x *reprojectRun) count(id string, changed bool, err error) {
	switch {
	case err != nil:
		x.res.Failed++
		if len(x.res.FailedObjects) < maxFailedReprojections {
			x.res.FailedObjects = append(x.res.FailedObjects, id)
		}
		if x.failure == "" {
			x.failure = err.Error()
		}
		slog.Warn("reproject: object not re-projected; continuing", "object", id, "err", err)
	case changed:
		x.res.Reprojected++
	default:
		x.res.Skipped++
	}
}

func (x *reprojectRun) tick() error {
	if x.Progress == nil {
		return nil
	}
	if err := x.Progress.TickFailed(x.res.Done(), x.res.Failed); err != nil {
		return fmt.Errorf("upgrade status: %w", err)
	}
	return nil
}

// finish stamps the signature when no object is left stale.
func (x *reprojectRun) finish(ctx context.Context) (ReprojectResult, error) {
	left, err := x.Store.CountStaleProjections(ctx)
	if err != nil {
		return x.stop(ctx, fmt.Errorf("count stale objects: %w", err))
	}
	if left > 0 || x.res.Failed > 0 {
		reason := fmt.Sprintf("%d objects still carry an older projection than %s; the next dpkms start retries them (first failure: %s)",
			left, ReprojectTarget(), x.failure)
		if x.Progress != nil {
			_ = x.Progress.Fail(errors.New(reason))
		}
		x.publish(ctx, events.TopicDpkmsUpgradeReprojectionFailed, reason)
		return x.res, nil
	}
	if err := x.Stamp(ctx); err != nil {
		return x.stop(ctx, fmt.Errorf("stamp fts signature: %w", err))
	}
	x.publish(ctx, events.TopicDpkmsUpgradeReprojectionCompleted, "")
	if x.Progress != nil {
		if err := x.Progress.Complete(); err != nil {
			return x.res, fmt.Errorf("upgrade status: %w", err)
		}
	}
	return x.res, nil
}

// stop ends a run early: status failed, failed event, cause returned.
func (x *reprojectRun) stop(ctx context.Context, cause error) (ReprojectResult, error) {
	if x.Progress != nil {
		_ = x.Progress.Fail(fmt.Errorf("re-projection to %s stopped after %d of %d objects: %w",
			ReprojectTarget(), x.res.Done(), x.res.Total, cause))
	}
	x.publish(context.WithoutCancel(ctx), events.TopicDpkmsUpgradeReprojectionFailed, cause.Error())
	return x.res, cause
}

func (x *reprojectRun) publish(ctx context.Context, topic bus.Topic, reason string) {
	p := events.ReprojectionPayload{
		ProjectionVersion: x.res.ProjectionVersion,
		JobID:             x.jobID,
		Done:              x.res.Done(),
		Total:             x.res.Total,
		Reprojected:       x.res.Reprojected,
		Skipped:           x.res.Skipped,
		Failed:            x.res.Failed,
		FailedObjects:     x.res.FailedObjects,
		Reason:            reason,
	}
	if topic == events.TopicDpkmsUpgradeReprojectionCompleted || topic == events.TopicDpkmsUpgradeReprojectionFailed {
		p.DurationMs = time.Since(x.start).Milliseconds()
	}
	publishReprojection(ctx, x.Bus, topic, p)
}

func publishReprojection(ctx context.Context, b events.Bus, topic bus.Topic, p events.ReprojectionPayload) {
	if b == nil {
		return
	}
	ev, err := events.NewEvent(reprojectEventSource, string(topic), p)
	if err != nil {
		return
	}
	if err := b.Publish(ctx, ev); err != nil {
		slog.Warn("reproject: publish event", "topic", topic, "err", err)
	}
}

// interruptCause is the error a run stopped by its context returns: the
// context's cause when there is one (shutdown, cancel, lost lease).
func interruptCause(ctx context.Context) error {
	if c := context.Cause(ctx); c != nil && !errors.Is(c, ctx.Err()) {
		return fmt.Errorf("%w: %w", ctx.Err(), c)
	}
	return ctx.Err()
}

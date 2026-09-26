// Package migrate runs the embedding migration job (ADR-071 "Migration
// job", amendment 2026-09-26): it fills one registered model's rows for
// every object that has none, through that model's provider only.
//
// `ctxt embeddings migrate --to <model_id>` enqueues a task job of type
// JobType; dpkms runs it in its worker pool through Runner.Handle, so the
// migration outlives the CLI.
//
// Progress is never stored: every page comes from
// EmbeddingStore.ListMissing, so a crash, a shutdown or a re-run resumes
// where the rows stop, and an object that already has a row for the model
// is never embedded again. A provider error for one object is recorded
// (log, bus event, status failure count) and skipped; its missing row is
// the durable record, and the next run retries it.
package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
	"hop.top/kit/go/runtime/bus"
)

// JobType is the task job type of an embedding migration.
const JobType = "embeddings:migrate"

// Batch bounds: the ListMissing page size.
const (
	DefaultBatch = 100
	MaxBatch     = 1000
)

// maxFailedObjects caps the failed object IDs carried in results and events.
const maxFailedObjects = 20

// eventSource is the CloudEvents source of migration events.
const eventSource = "dpkms.embeddings.migrate"

// Request is a migration job's payload.
type Request struct {
	ModelID string `json:"model_id"`
	// RateLimit caps provider calls per second; 0 means no cap.
	RateLimit float64 `json:"rate_limit,omitempty"`
	// Batch is the ListMissing page size; 0 means DefaultBatch.
	Batch int `json:"batch,omitempty"`
}

// Validate checks the request's own fields (not the registry).
func (r Request) Validate() error {
	if err := storage.ValidateEmbeddingModelID(r.ModelID); err != nil {
		return err
	}
	if r.RateLimit < 0 {
		return fmt.Errorf("rate limit %g must not be negative", r.RateLimit)
	}
	if r.Batch < 0 || r.Batch > MaxBatch {
		return fmt.Errorf("batch %d must be between 1 and %d", r.Batch, MaxBatch)
	}
	return nil
}

func (r Request) batch() int {
	if r.Batch == 0 {
		return DefaultBatch
	}
	return r.Batch
}

// Encode returns the job payload for r.
func (r Request) Encode() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Decode parses and validates a job payload.
func Decode(payload string) (Request, error) {
	var r Request
	dec := json.NewDecoder(strings.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Request{}, fmt.Errorf("embeddings migrate payload: %w", err)
	}
	if err := r.Validate(); err != nil {
		return Request{}, fmt.Errorf("embeddings migrate payload: %w", err)
	}
	return r, nil
}

// ParseRate parses a --rate-limit value: calls per second as "N" or "N/s".
// "0" means no cap.
func ParseRate(s string) (float64, error) {
	v := strings.TrimSpace(s)
	v = strings.TrimSuffix(v, "/s")
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 || n != n { // n != n: NaN
		return 0, fmt.Errorf("rate limit %q: want calls per second, like 5 or 5/s", s)
	}
	return n, nil
}

// Models reads the registry.
type Models interface {
	Get(ctx context.Context, modelID string) (*registry.Model, error)
}

var _ Models = (*registry.Store)(nil)

// Progress is the ADR-070 upgrade-status surface (*upgrade.Manager).
type Progress interface {
	StartTarget(bucket upgrade.Bucket, target string, total int) error
	TickFailed(done, failed int) error
	Complete() error
	Fail(err error) error
}

var _ Progress = (*upgrade.Manager)(nil)

// Target returns the registry entry a migration can fill: registered, with
// a measured dimension, and not effectively deprecated at now. Errors wrap
// registry.ErrModelNotFound or ErrNotMigratable.
func Target(ctx context.Context, models Models, modelID string, now time.Time) (*registry.Model, error) {
	m, err := models.Get(ctx, modelID)
	if err != nil {
		return nil, err
	}
	if m.Dimension <= 0 {
		return nil, fmt.Errorf("%s has no measured dimension: %w", modelID, ErrNotMigratable)
	}
	if m.DeprecatedAt != nil && !m.DeprecatedAt.After(now) {
		return nil, fmt.Errorf("%s is deprecated since %s: %w", modelID, m.DeprecatedAt.UTC().Format(time.RFC3339), ErrNotMigratable)
	}
	return m, nil
}

// ErrNotMigratable marks a registered model a migration cannot fill.
var ErrNotMigratable = errors.New("model cannot be migrated to")

// CountMissing counts the objects with no row for modelID.
func CountMissing(ctx context.Context, store storage.EmbeddingStore, modelID string) (int, error) {
	const page = 10000
	total, after := 0, ""
	for {
		ids, err := store.ListMissing(ctx, modelID, after, page)
		if err != nil {
			return 0, err
		}
		total += len(ids)
		if len(ids) < page {
			return total, nil
		}
		after = ids[len(ids)-1]
	}
}

// Result summarises one run.
type Result struct {
	ModelID  string `json:"model_id"`
	Total    int    `json:"total"`
	Embedded int    `json:"embedded"`
	Failed   int    `json:"failed"`
	// Skipped objects have no embeddable text, or were deleted mid-run.
	Skipped       int      `json:"skipped"`
	FailedObjects []string `json:"failed_objects,omitempty"`
}

// Done is the number of objects the run handled.
func (r Result) Done() int { return r.Embedded + r.Failed + r.Skipped }

// Runner runs migrations. Zero-value optional fields: no status (Progress),
// no events (Bus), wall clock (Clock). Runs are serialised per Runner.
type Runner struct {
	Models   Models
	Objects  storage.ObjectStore
	Store    storage.EmbeddingStore
	Resolver embeddings.ProviderResolver
	Progress Progress
	Bus      events.Bus
	Clock    Clock

	sem  chan struct{}
	once sync.Once
}

// Handle is the jobs.TaskHandler for JobType.
func (r *Runner) Handle(ctx context.Context, job *storage.Job) (string, error) {
	req, err := Decode(job.Payload)
	if err != nil {
		return "", pipeline.Permanent(err)
	}
	res, err := r.Run(ctx, job.ID, req)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Run embeds every object missing a row for req.ModelID. It returns an
// error only when the run stops early; per-object failures are counted in
// the Result. Configuration errors (unknown or unmigratable model, a
// provider the registry entry cannot resolve) are pipeline.Permanent.
func (r *Runner) Run(ctx context.Context, jobID string, req Request) (Result, error) {
	if err := req.Validate(); err != nil {
		return Result{}, pipeline.Permanent(err)
	}
	if err := r.acquire(ctx); err != nil {
		return Result{}, err
	}
	defer r.release()

	clock := r.clock()
	m, err := Target(ctx, r.Models, req.ModelID, clock.Now())
	if err != nil {
		if errors.Is(err, registry.ErrModelNotFound) || errors.Is(err, ErrNotMigratable) {
			return Result{}, pipeline.Permanent(err)
		}
		return Result{}, err
	}
	if err := r.Store.EnsureIndex(ctx, embeddings.SpecFor(*m)); err != nil {
		return Result{}, fmt.Errorf("index %s: %w", m.ModelID, err)
	}
	// Resolve once up front: a registry entry that cannot produce a
	// provider fails every object the same way.
	if _, err := r.Resolver.ForModel(ctx, *m); err != nil {
		return Result{}, pipeline.Permanent(fmt.Errorf("provider for %s: %w", m.ModelID, err))
	}

	total, err := CountMissing(ctx, r.Store, m.ModelID)
	if err != nil {
		return Result{}, fmt.Errorf("count missing %s: %w", m.ModelID, err)
	}

	run := &run{
		Runner: r, jobID: jobID, req: req, model: *m, clock: clock,
		res:   Result{ModelID: m.ModelID, Total: total},
		calls: &calls{limit: newLimiter(req.RateLimit, clock)},
	}
	return run.execute(ctx)
}

func (r *Runner) acquire(ctx context.Context) error {
	r.once.Do(func() { r.sem = make(chan struct{}, 1) })
	select {
	case r.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) release() { <-r.sem }

func (r *Runner) clock() Clock {
	if r.Clock == nil {
		return wallClock{}
	}
	return r.Clock
}

// run is one migration's state.
type run struct {
	*Runner
	jobID string
	req   Request
	model registry.Model
	clock Clock
	res   Result
	calls *calls
	start time.Time
	// failure is the first per-object failure, for the status message.
	failure string
}

func (x *run) execute(ctx context.Context) (Result, error) {
	x.start = x.clock.Now()
	if x.Progress != nil {
		if err := x.Progress.StartTarget(upgrade.BucketEmbeddingsMigrate, x.model.ModelID, x.res.Total); err != nil {
			return x.res, fmt.Errorf("upgrade status: %w", err)
		}
	}
	x.publish(ctx, events.TopicDpkmsUpgradeEmbeddingsMigrationStarted, "")

	// The step embeds only the target: its model source yields just that
	// model, and its resolver meters and records every provider call.
	step := steps.NewEmbeddingGenerator(only(x.model), &meteredResolver{inner: x.Resolver, calls: x.calls})
	progressEvery := max(1, x.res.Total/100)

	cursor := ""
	for {
		ids, err := x.Store.ListMissing(ctx, x.model.ModelID, cursor, x.req.batch())
		if err != nil {
			return x.stop(ctx, fmt.Errorf("list missing: %w", err))
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return x.stop(ctx, interruption(ctx))
			}
			cursor = id
			outcome, oerr := x.embedOne(ctx, step, id)
			if ctx.Err() != nil {
				// The object was cut off, not failed: it is still
				// missing and the resumed run embeds it.
				return x.stop(ctx, interruption(ctx))
			}
			x.count(id, outcome, oerr)
			if x.Progress != nil {
				if err := x.Progress.TickFailed(x.res.Done(), x.res.Failed); err != nil {
					return x.res, fmt.Errorf("upgrade status: %w", err)
				}
			}
			if x.res.Done()%progressEvery == 0 {
				x.publish(ctx, events.TopicDpkmsUpgradeEmbeddingsMigrationProgress, "")
			}
		}
	}

	x.publish(ctx, events.TopicDpkmsUpgradeEmbeddingsMigrationCompleted, "")
	if x.Progress != nil {
		if x.res.Failed > 0 {
			// Sticky: the status stays failed until the next run, so the
			// operator sees that some objects still lack rows.
			_ = x.Progress.Fail(fmt.Errorf("%d of %d objects not embedded under %s; re-run `ctxt embeddings migrate --to %s` to retry them (first: %s)",
				x.res.Failed, x.res.Done(), x.model.ModelID, x.model.ModelID, x.firstFailure()))
		} else if err := x.Progress.Complete(); err != nil {
			return x.res, fmt.Errorf("upgrade status: %w", err)
		}
	}
	return x.res, nil
}

type outcome int

const (
	embedded outcome = iota
	failed
	skipped
)

// embedOne embeds object id under the target and writes its row.
func (x *run) embedOne(ctx context.Context, step *steps.EmbeddingGenerator, id string) (outcome, error) {
	obj, err := x.Objects.Get(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, storage.ErrNotFound) {
			return skipped, nil // deleted since ListMissing
		}
		return failed, fmt.Errorf("load object: %w", err)
	}
	x.calls.reset()
	draft, err := step.Run(ctx, obj)
	if err != nil {
		return failed, fmt.Errorf("embedding step: %w", err)
	}
	vectors := make([]storage.ObjectVector, 0, 1)
	for _, v := range draft.Vectors {
		if v.ModelID == x.model.ModelID {
			vectors = append(vectors, v)
		}
	}
	if len(vectors) == 0 {
		if err := x.calls.err(); err != nil {
			return failed, err
		}
		return skipped, nil // no embeddable text
	}
	if err := x.Store.Put(ctx, id, vectors); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, storage.ErrNotFound) {
			return skipped, nil
		}
		return failed, fmt.Errorf("store vector: %w", err)
	}
	return embedded, nil
}

func (x *run) count(id string, o outcome, err error) {
	switch o {
	case embedded:
		x.res.Embedded++
	case skipped:
		x.res.Skipped++
	case failed:
		x.res.Failed++
		if len(x.res.FailedObjects) < maxFailedObjects {
			x.res.FailedObjects = append(x.res.FailedObjects, id)
		}
		if x.failure == "" {
			x.failure = fmt.Sprintf("%s: %v", id, err)
		}
		slog.Warn("embeddings migrate: object not embedded; continuing",
			"model_id", x.model.ModelID, "object", id, "err", err)
	}
}

func (x *run) firstFailure() string { return x.failure }

// stop ends a run early: status failed, failed event, the cause returned.
func (x *run) stop(ctx context.Context, cause error) (Result, error) {
	if x.Progress != nil {
		_ = x.Progress.Fail(fmt.Errorf("embeddings migrate to %s stopped after %d of %d objects: %w",
			x.model.ModelID, x.res.Done(), x.res.Total, cause))
	}
	x.publish(context.WithoutCancel(ctx), events.TopicDpkmsUpgradeEmbeddingsMigrationFailed, cause.Error())
	return x.res, cause
}

// interruption is the error a run stopped by its context returns: the
// context's cause when there is one (operator cancel, shutdown).
func interruption(ctx context.Context) error {
	if c := context.Cause(ctx); c != nil && !errors.Is(c, ctx.Err()) {
		return fmt.Errorf("%w: %w", ctx.Err(), c)
	}
	return ctx.Err()
}

func (x *run) publish(ctx context.Context, topic bus.Topic, reason string) {
	if x.Bus == nil {
		return
	}
	payload := events.EmbeddingsMigrationPayload{
		ModelID:       x.model.ModelID,
		JobID:         x.jobID,
		Done:          x.res.Done(),
		Total:         x.res.Total,
		Embedded:      x.res.Embedded,
		Failed:        x.res.Failed,
		Skipped:       x.res.Skipped,
		RateLimit:     x.req.RateLimit,
		FailedObjects: x.res.FailedObjects,
		Reason:        reason,
	}
	if topic == events.TopicDpkmsUpgradeEmbeddingsMigrationCompleted || topic == events.TopicDpkmsUpgradeEmbeddingsMigrationFailed {
		payload.DurationMs = x.clock.Now().Sub(x.start).Milliseconds()
	}
	ev, err := events.NewEvent(eventSource, string(topic), payload)
	if err != nil {
		return
	}
	if err := x.Bus.Publish(ctx, ev); err != nil {
		slog.Warn("embeddings migrate: publish event", "topic", topic, "err", err)
	}
}

// only is a ModelSource of just the target. The step's ingest-time
// "no default model" warning concerns drafts being stored; the migration
// discards the draft, so the target is presented as the default to keep
// that warning quiet.
type only registry.Model

func (o only) Default(context.Context) (*registry.Model, error) {
	m := registry.Model(o)
	m.IsDefault = true
	return &m, nil
}

func (o only) Populating(ctx context.Context, _ time.Time) ([]registry.Model, error) {
	m, _ := o.Default(ctx)
	return []registry.Model{*m}, nil
}

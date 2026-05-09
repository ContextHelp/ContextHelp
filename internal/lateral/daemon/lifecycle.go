package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/domain"
	"hop.top/kit/go/runtime/job"
	"hop.top/kit/go/runtime/job/mock"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/jobs"
)

// CapturedEventTopic is the bus topic the daemon subscribes to for
// new captured objects. Matches the substrate's discover.go
// subscriptions.
const CapturedEventTopic = "ctxt.ingest.object.captured"

// PersistedEventTopic is the parallel "object persisted" topic. The
// substrate's Discover handles both; the daemon's lifecycle layer
// dispatches the same way.
const PersistedEventTopic = "ctxt.ingest.object.persisted"

// Lifecycle owns the running pieces of the daemon: the bus
// subscription, the job engine + poller, and the cold-cycle
// reaper handler. Construct via NewLifecycle; drive via Run.
type Lifecycle struct {
	registry *lateral.Registry
	pub      domain.EventPublisher
	busSrc   bus.Bus
	jobSvc   job.Service
	workerID string
	pollIntv time.Duration

	cancels []bus.Unsubscribe

	// scanCount tracks ScanFunc invocations for tests/observability.
	scanCount atomic.Int64
}

// LifecycleOptions wires the lifecycle layer.
//
// Bus is required: lifecycle subscribes to capture events and
// publishes reaper completion events through it.
// Registry is required: dispatch routes captured events through it.
// Publisher (kitdomain.EventPublisher) is the same bus wrapped via
// the T-0318 adapter; passed separately because domain.EventPublisher
// is the kit-side type the cold-cycle handler consumes.
//
// JobService is optional. Nil falls back to job/mock.New() (in-process,
// in-memory). Production deployments wire a sqlite-backed job.Service
// once kit ships one (currently a follow-on).
//
// WorkerID identifies this daemon in the job claim ledger. Empty
// falls back to "lateral-daemon-default".
//
// PollInterval is the cold-cycle poller cadence. Zero falls back to
// 5s (kit's job/poller default).
type LifecycleOptions struct {
	Bus          bus.Bus
	Registry     *lateral.Registry
	Publisher    domain.EventPublisher
	JobService   job.Service
	WorkerID     string
	PollInterval time.Duration
}

// NewLifecycle wires the lifecycle layer. Validates required deps;
// returns an error rather than panicking so the daemon's start
// command can render a clean message.
func NewLifecycle(opts LifecycleOptions) (*Lifecycle, error) {
	if opts.Bus == nil {
		return nil, errors.New("daemon.NewLifecycle: Bus is nil")
	}
	if opts.Registry == nil {
		return nil, errors.New("daemon.NewLifecycle: Registry is nil")
	}
	if opts.Publisher == nil {
		return nil, errors.New("daemon.NewLifecycle: Publisher is nil")
	}
	svc := opts.JobService
	if svc == nil {
		svc = mock.New()
	}
	worker := opts.WorkerID
	if worker == "" {
		worker = "lateral-daemon-default"
	}
	pollIntv := opts.PollInterval
	if pollIntv == 0 {
		pollIntv = 5 * time.Second
	}
	return &Lifecycle{
		registry: opts.Registry,
		pub:      opts.Publisher,
		busSrc:   opts.Bus,
		jobSvc:   svc,
		workerID: worker,
		pollIntv: pollIntv,
	}, nil
}

// Start subscribes to capture events. Idempotent: calling twice
// re-subscribes (test convenience); production callers invoke once.
func (l *Lifecycle) Start(_ context.Context) error {
	for _, c := range l.cancels {
		c()
	}
	l.cancels = []bus.Unsubscribe{
		l.busSrc.Subscribe(CapturedEventTopic, l.handleCaptureEvent),
		l.busSrc.Subscribe(PersistedEventTopic, l.handleCaptureEvent),
	}
	return nil
}

// Stop tears down subscriptions. Safe to call multiple times.
func (l *Lifecycle) Stop() {
	for _, c := range l.cancels {
		c()
	}
	l.cancels = nil
}

// Run executes the cold-cycle poller until ctx is cancelled. Blocks.
// SIGINT/SIGTERM handling lives in the cmd surface (cmd/ctxt/cmd/
// lateral.go); this method is signal-agnostic.
//
// Returns ctx.Err() when ctx is cancelled (typical clean shutdown).
func (l *Lifecycle) Run(ctx context.Context) error {
	scan := l.makeScanFunc()
	handler := jobs.ColdCycleHandler(l.jobSvc, l.pub, scan)

	hmap := jobs.HandlerMap(jobs.Handlers{
		ColdCycleExpiry: handler,
	})

	poller := &job.Poller{
		Service:  l.jobSvc,
		Interval: l.pollIntv,
		Queue:    jobs.QueueDeferred,
		WorkerID: l.workerID,
		Handlers: hmap,
	}
	return poller.Run(ctx)
}

// EnqueueColdCycle inserts a single cold-cycle job into the queue.
// Used by the lateral status command to trigger an on-demand cycle
// and by tests that want to drive the poller deterministically.
func (l *Lifecycle) EnqueueColdCycle(ctx context.Context) (string, error) {
	return jobs.EnqueueDeferred(ctx, l.jobSvc, jobs.Deferred{
		CandidateID: "scheduled",
		Cause:       jobs.CauseColdCycleExpiry,
	})
}

// ScanCount returns the number of ScanFunc invocations since boot.
// Test-facing; production observability lives on the
// ctxt.lateral.reaper_cycle.completed bus topic.
func (l *Lifecycle) ScanCount() int64 { return l.scanCount.Load() }

// makeScanFunc returns the closure the cold-cycle handler invokes
// each cycle. The substrate's lifecycle/promote/reject machinery
// owns the actual sweep (see internal/lateral/lifecycle/ttl.go for
// the TTL boundary check); this closure just observes the boundary
// once per cycle for the rolled-up CycleStats. The substrate-side
// reaper integration is a follow-on slice — T-0321 ships the wiring
// shape; the closure is intentionally minimal so the substrate's
// own reaper closure can plug in directly when it lands.
func (l *Lifecycle) makeScanFunc() jobs.ScanFunc {
	return func(_ context.Context) (jobs.CycleStats, error) {
		start := time.Now()
		l.scanCount.Add(1)
		// Placeholder: real sweep enumerates lateral_candidate rows
		// where ttl < now and transitions them via the lifecycle
		// state machine (Probationary → Expired). Wiring the storage
		// adapter into Lifecycle is part of the resolver
		// integration slice, not this track.
		stats := jobs.CycleStats{
			Expired:    0,
			DurationMS: int(time.Since(start) / time.Millisecond),
		}
		return stats, nil
	}
}

// handleCaptureEvent is the bus subscriber for capture events. It
// constructs a lateral.CapturedEvent, dispatches through the registry,
// and runs each matching strategy's Probe. The cumulative candidate
// count surfaces in a ctxt.lateral.scan.completed event per the schema
// at schemas/lateral_events.json (object_id + candidates_emitted).
//
// Probe outputs themselves are NOT routed downstream by this slice.
// Score / cap-gate / identity-resolve / materialize are separate stages
// that a follow-on track wires (see docs/superpowers/specs/...
// lateral-capture-discovery-design.md). For now the daemon's job is
// dispatch + observability; downstream consumers subscribe to
// scan.completed for the count and scan.failed for per-strategy errors.
//
// Errors from individual strategies are NOT returned to the bus —
// the bus would veto every subsequent handler. They emit as
// ctxt.lateral.scan.failed events instead so observers see the
// failure without breaking other subscribers.
func (l *Lifecycle) handleCaptureEvent(ctx context.Context, e bus.Event) error {
	ev, ok := captureEventFromBusPayload(e)
	if !ok {
		return nil // malformed payload — skip, not bus error
	}
	chosen := l.registry.Dispatch(ctx, ev)
	candidates := 0
	for _, s := range chosen {
		out, err := s.Probe(ctx, ev, lateral.ActiveContext{})
		if err != nil {
			// Emit a scan-failed event for observability; intentionally
			// not returning the error so other subscribers still run.
			_ = l.pub.Publish(ctx, "ctxt.lateral.scan.failed", "lateral.daemon",
				map[string]any{
					"strategy_id": s.ID(),
					"object_id":   ev.ObjectID,
					"source_url":  ev.SourceURL,
					"error":       err.Error(),
					"severity":    "error",
				})
			continue
		}
		candidates += len(out)
	}
	// scan.completed is emitted whether or not any strategy claimed —
	// observers see the dispatch happened, with candidates_emitted=0
	// when no strategy matched (or all matched but produced empty).
	_ = l.pub.Publish(ctx, "ctxt.lateral.scan.completed", "lateral.daemon",
		map[string]any{
			"object_id":          ev.ObjectID,
			"candidates_emitted": candidates,
			"severity":           "info",
		})
	return nil
}

// captureEventFromBusPayload coerces the bus event payload into a
// lateral.CapturedEvent. Capture pipeline payloads are typed as
// map[string]any with stable keys (object_id / namespace / source_url
// / capture_pipeline / persisted_at). The capture pipeline owns this
// shape; lateral consumes it as-is. No formal JSON schema is published
// for the capture-side payload today — the field set tracks
// lateral.CapturedEvent directly.
//
// Returns ok=false on shape mismatch so the caller can drop the event
// without surfacing it as a bus error.
func captureEventFromBusPayload(e bus.Event) (lateral.CapturedEvent, bool) {
	m, ok := e.Payload.(map[string]any)
	if !ok {
		return lateral.CapturedEvent{}, false
	}
	out := lateral.CapturedEvent{}
	if v, ok := m["object_id"].(string); ok {
		out.ObjectID = v
	}
	if v, ok := m["namespace"].(string); ok {
		out.Namespace = v
	}
	if v, ok := m["source_url"].(string); ok {
		out.SourceURL = v
	}
	if v, ok := m["capture_pipeline"].(string); ok {
		out.CapturePipeline = v
	}
	if v, ok := m["persisted_at"].(int64); ok {
		out.PersistedAt = v
	}
	return out, out.SourceURL != "" || out.ObjectID != ""
}

// EnqueueDirect dispatches an event synchronously through the
// registry. Test-facing; production paths route via the bus
// subscription.
func (l *Lifecycle) EnqueueDirect(ctx context.Context, ev lateral.CapturedEvent) error {
	chosen := l.registry.Dispatch(ctx, ev)
	if len(chosen) == 0 {
		return fmt.Errorf("lifecycle.EnqueueDirect: no strategy matched %s", ev.SourceURL)
	}
	for _, s := range chosen {
		if _, err := s.Probe(ctx, ev, lateral.ActiveContext{}); err != nil {
			return err
		}
	}
	return nil
}

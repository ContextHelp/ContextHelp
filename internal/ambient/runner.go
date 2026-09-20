package ambient

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"hop.top/kit/go/runtime/bus"
)

// Enqueuer is the contract the Runner uses to deliver RawEvents to dpkms. A
// real implementation POSTs to /api/v1/analyze with session_id, ambient_source,
// and fingerprint as optional fields (per ADR-066 §Decision). Tests and
// in-memory backends implement Enqueue directly.
//
// Enqueue MUST be safe to call concurrently. It returns nil on accepted-by-
// dpkms (2xx HTTP status), or an error indicating the failure category. The
// Runner emits ctxt.ambient.enqueue.{succeeded,failed} based on the result.
type Enqueuer interface {
	Enqueue(ctx context.Context, ev RawEvent) error
}

// Cutter is the contract the Runner uses to tag RawEvents with the active
// session id (per ADR-067). On every event, OnEvent is called so the cutter
// can update its app-mix and idle-gap state; ActiveID() returns the current
// session id (empty when no session is open).
//
// Implementations live under internal/ambient/session/. Tests pass a stub.
type Cutter interface {
	OnEvent(ev RawEvent)
	ActiveID() string
}

// Dedup is the contract the Runner uses for client-side fingerprint dedup at
// the enqueue boundary. A real implementation maintains a bounded LRU keyed
// on Fingerprint with a configurable TTL window. Tests pass a stub.
//
// IsDuplicate is called for every event with a non-empty Fingerprint. Empty
// fingerprints bypass the dedup check (Source-side opt-out).
type Dedup interface {
	// IsDuplicate reports whether fingerprint has been seen recently. Side
	// effect: records the fingerprint if not duplicate.
	IsDuplicate(fingerprint string) bool
}

// runnerBusPublisher wraps a kit/runtime/bus.Bus to satisfy the Source
// Publisher interface. Topic strings are passed through as kit Topic values.
type runnerBusPublisher struct {
	bus bus.Bus
}

func (p *runnerBusPublisher) Publish(ctx context.Context, topic, source string, payload any) error {
	if p == nil || p.bus == nil {
		return nil
	}
	return p.bus.Publish(ctx, bus.NewEvent(bus.Topic(topic), source, payload))
}

// Runner is the substrate's core: it owns the Registry, multiplexes Source
// event channels, applies dedup → session-tag → enqueue, and emits bus events
// at every transition per the ADR-066 §Decision taxonomy.
//
// One Runner instance per process (managed by ctxd's main loop or by the
// ctxt CLI's `capture --ambient` foreground mode). The Runner is the single
// owner of source lifecycle goroutines; CLI / MCP read-side queries inspect
// state through accessor methods that take the Runner's read lock.
//
// Lifecycle:
//
//	NewRunner   — construct with bus + cutter + dedup + enqueue dependencies
//	Register    — add a Source (delegates to internal Registry)
//	Start       — start every registered Source and the dispatch loop
//	Stop        — drain + stop every Source; close the dispatch loop
//
// Start blocks until ctx is cancelled or all sources have stopped. Production
// callers run Start in its own goroutine; tests use a context with a tight
// deadline.
type Runner struct {
	bus      bus.Bus
	registry *Registry
	cutter   Cutter
	dedup    Dedup
	enqueue  Enqueuer

	mu        sync.RWMutex
	state     LifecycleState
	starts    map[string]error // start error per source
	publisher *runnerBusPublisher
}

// RunnerOptions are dependencies passed to NewRunner. All fields except Bus
// are optional; the Runner provides safe no-op defaults so unit tests can
// construct a Runner with just a bus.
type RunnerOptions struct {
	Bus      bus.Bus // required; used for lifecycle + per-event topic emission
	Cutter   Cutter  // optional; defaults to a no-op (events get empty SessionID)
	Dedup    Dedup   // optional; defaults to no-op (every event passes through)
	Enqueuer Enqueuer
}

// NewRunner constructs a Runner with the supplied options. Returns an error
// when bus is nil; everything else has a safe default.
func NewRunner(opts RunnerOptions) (*Runner, error) {
	if opts.Bus == nil {
		return nil, errors.New("ambient: NewRunner requires a non-nil Bus")
	}
	r := &Runner{
		bus:       opts.Bus,
		registry:  NewRegistry(),
		cutter:    opts.Cutter,
		dedup:     opts.Dedup,
		enqueue:   opts.Enqueuer,
		state:     StateStopped,
		starts:    make(map[string]error),
		publisher: &runnerBusPublisher{bus: opts.Bus},
	}
	if r.cutter == nil {
		r.cutter = noopCutter{}
	}
	if r.dedup == nil {
		r.dedup = noopDedup{}
	}
	if r.enqueue == nil {
		r.enqueue = noopEnqueuer{}
	}
	return r, nil
}

// Register adds a Source under its declared Name(). Wrapper around the
// underlying Registry so callers don't need to reach in.
func (r *Runner) Register(s Source) error {
	return r.registry.Register(s)
}

// Sources returns the sorted list of registered source names. Read-side
// accessor used by status CLI and MCP health tool.
func (r *Runner) Sources() []string {
	return r.registry.Names()
}

// State returns the runner's current lifecycle state.
func (r *Runner) State() LifecycleState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// Start begins multiplex dispatch: starts every registered Source, fans the
// per-source event channels into a single dispatch loop, and routes each
// RawEvent through dedup → session-tag → enqueue with the documented bus
// topics emitted at every stage. Start blocks until ctx is cancelled.
//
// On clean shutdown (ctx cancellation), Start drains every source via
// Source.Drain then Source.Stop and returns the first non-nil error
// encountered (or nil if all clean).
func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.state != StateStopped {
		state := r.state
		r.mu.Unlock()
		return fmt.Errorf("ambient: runner already started (state=%s)", state)
	}
	r.state = StateStarting
	r.mu.Unlock()

	names := r.registry.Names()

	// Start each source, capturing per-source errors and merging event channels
	// into a single dispatch path. We use a fan-in goroutine per source so the
	// dispatch loop reads from one merged channel.
	merged := make(chan RawEvent, 64)
	var wg sync.WaitGroup

	for _, name := range names {
		src := r.registry.Get(name)
		if err := src.Start(ctx, r.publisher); err != nil {
			r.mu.Lock()
			r.starts[name] = err
			r.mu.Unlock()
			_ = r.bus.Publish(ctx, bus.NewEvent(
				bus.Topic(SourceLifecycleTopic("failed")),
				name,
				map[string]any{"error": err.Error()},
			))
			continue
		}
		_ = r.bus.Publish(ctx, bus.NewEvent(
			bus.Topic(SourceLifecycleTopic("started")),
			name,
			nil,
		))

		wg.Add(1)
		go func(n string, ch <-chan RawEvent) {
			defer wg.Done()
			for ev := range ch {
				select {
				case merged <- ev:
				case <-ctx.Done():
					return
				}
			}
		}(name, src.Events())
	}

	r.mu.Lock()
	r.state = StateReady
	r.mu.Unlock()

	// Dispatch loop. Reads from merged, applies dedup → session-tag → enqueue.
	dispatchDone := make(chan struct{})
	go func() {
		defer close(dispatchDone)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-merged:
				if !ok {
					return
				}
				r.dispatch(ctx, ev)
			}
		}
	}()

	// Wait for context cancellation or all source goroutines to finish.
	go func() {
		wg.Wait()
		close(merged)
	}()

	<-ctx.Done()

	// Shutdown: drain then stop every source. Use a fresh context so cleanup
	// has time to flush even if the parent ctx is already cancelled.
	r.mu.Lock()
	r.state = StateDraining
	r.mu.Unlock()

	shutdownCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var firstErr error
	for _, name := range names {
		src := r.registry.Get(name)
		if err := src.Drain(shutdownCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("source %s drain: %w", name, err)
		}
		if err := src.Stop(shutdownCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("source %s stop: %w", name, err)
		}
		_ = r.bus.Publish(shutdownCtx, bus.NewEvent(
			bus.Topic(SourceLifecycleTopic("stopped")),
			name,
			nil,
		))
	}

	<-dispatchDone

	r.mu.Lock()
	r.state = StateStopped
	r.mu.Unlock()
	return firstErr
}

// dispatch routes one RawEvent through the substrate pipeline. Each transition
// emits a kit/runtime/bus event so observers (status CLI, kit/policy CEL
// guards, audit) can subscribe at any granularity.
//
// Order is fixed:
//
//  1. captured     — every event arriving at dispatch
//  2. session-tag  — populate ev.SessionID from the cutter
//  3. dedup        — drop if fingerprint matches recent event
//  4. enqueue      — POST to dpkms; emit succeeded/failed per result
//
// Source-side redaction and kit/policy CEL filter are applied INSIDE Source
// implementations or via subscribers on captured/redacted topics; the Runner
// does not implement them. This keeps the substrate minimal — privacy and
// policy hooks compose via bus subscriptions, not hard-coded gates.
func (r *Runner) dispatch(ctx context.Context, ev RawEvent) {
	_ = r.bus.Publish(ctx, bus.NewEvent(
		bus.Topic(EventTopic("captured")),
		ev.Source,
		ev,
	))

	// Tag with active session id. Empty when no session is open.
	r.cutter.OnEvent(ev)
	ev.SessionID = r.cutter.ActiveID()
	if ev.SessionID != "" {
		_ = r.bus.Publish(ctx, bus.NewEvent(
			bus.Topic(SessionTopic("event_joined")),
			ev.Source,
			map[string]any{"session_id": ev.SessionID, "fingerprint": ev.Fingerprint},
		))
	}

	// Fingerprint dedup. Empty fingerprint is a Source-side opt-out (rare).
	if ev.Fingerprint != "" && r.dedup.IsDuplicate(ev.Fingerprint) {
		_ = r.bus.Publish(ctx, bus.NewEvent(
			bus.Topic(EventTopic("deduped")),
			ev.Source,
			map[string]any{"fingerprint": ev.Fingerprint},
		))
		return
	}

	// Enqueue.
	_ = r.bus.Publish(ctx, bus.NewEvent(
		bus.Topic(EnqueueTopic("attempted")),
		ev.Source,
		map[string]any{"fingerprint": ev.Fingerprint, "session_id": ev.SessionID},
	))
	if err := r.enqueue.Enqueue(ctx, ev); err != nil {
		_ = r.bus.Publish(ctx, bus.NewEvent(
			bus.Topic(EnqueueTopic("failed")),
			ev.Source,
			map[string]any{"fingerprint": ev.Fingerprint, "error": err.Error()},
		))
		return
	}
	_ = r.bus.Publish(ctx, bus.NewEvent(
		bus.Topic(EnqueueTopic("succeeded")),
		ev.Source,
		map[string]any{"fingerprint": ev.Fingerprint, "session_id": ev.SessionID},
	))
}

// noopCutter is the default Cutter when none is supplied. Useful in tests and
// in deployments where session cutting hasn't landed yet (Phase 2 ships before
// ADR-067's cutter implementation).
type noopCutter struct{}

func (noopCutter) OnEvent(RawEvent) {}
func (noopCutter) ActiveID() string { return "" }

// noopDedup admits every event. Useful in tests.
type noopDedup struct{}

func (noopDedup) IsDuplicate(string) bool { return false }

// noopEnqueuer succeeds for every event without doing anything. Useful in
// substrate tests where the dispatch path is exercised without a real dpkms.
type noopEnqueuer struct{}

func (noopEnqueuer) Enqueue(context.Context, RawEvent) error { return nil }

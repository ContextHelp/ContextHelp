package lateral

import (
	"context"
	"errors"
	"time"

	"hop.top/kit/go/runtime/bus"
)

// ErrNotYetReadable indicates the parent object is not yet readable from the
// canonical store. The discover pipeline retries on this error per the
// configured schedule.
var ErrNotYetReadable = errors.New("parent not yet readable")

// Store is the subset of the canonical store that lateral.discover reads.
type Store interface {
	Read(ctx context.Context, id string) (map[string]any, error)
}

// DiscoverConfig wires a Discover's collaborators.
type DiscoverConfig struct {
	Bus           bus.Bus
	Registry      *Registry
	Store         Store
	RetrySchedule []time.Duration
}

// Discover is the top-level lateral pipeline. It subscribes to capture events
// and dispatches strategies; the candidate scoring → resolution → cap gate →
// materialization phases land in later tasks. T03 ships only the subscription
// shell.
type Discover struct {
	cfg     DiscoverConfig
	cancels []bus.Unsubscribe
}

// NewDiscover constructs a Discover. Call Start to subscribe.
func NewDiscover(cfg DiscoverConfig) *Discover { return &Discover{cfg: cfg} }

// Start subscribes Discover to the capture-pipeline events. Subscriptions are
// torn down by Stop. Returns nil; subscription errors (if the underlying bus
// surfaces any) bubble up via the returned Unsubscribe handles.
func (d *Discover) Start(_ context.Context) error {
	d.cancels = append(
		d.cancels,
		d.cfg.Bus.Subscribe("ctxt.ingest.object.persisted", d.handlePersisted),
		d.cfg.Bus.Subscribe("ctxt.ingest.object.captured", d.handleCaptured),
	)
	return nil
}

// Stop tears down all subscriptions.
func (d *Discover) Stop() {
	for _, c := range d.cancels {
		c()
	}
	d.cancels = nil
}

// readParent returns the canonical record for the given object ID. On
// ErrNotYetReadable, retries per the configured schedule (default: 50ms,
// 200ms, 1s, 3s). Returns the last error if no attempt succeeds within the
// schedule.
func (d *Discover) readParent(ctx context.Context, id string) (map[string]any, error) {
	schedule := d.cfg.RetrySchedule
	if len(schedule) == 0 {
		schedule = []time.Duration{50 * time.Millisecond, 200 * time.Millisecond, 1 * time.Second, 3 * time.Second}
	}
	rec, err := d.cfg.Store.Read(ctx, id)
	if err == nil {
		return rec, nil
	}
	for _, wait := range schedule {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		rec, err = d.cfg.Store.Read(ctx, id)
		if err == nil {
			return rec, nil
		}
	}
	return nil, err
}

func (d *Discover) handlePersisted(_ context.Context, _ bus.Event) error {
	// Later task wires this: read parent from canonical store via readParent,
	// dispatch strategies, score, gate, materialize.
	return nil
}

func (d *Discover) handleCaptured(_ context.Context, _ bus.Event) error {
	// Later task wires this: same flow as handlePersisted, but parent read
	// goes through readParent's full retry schedule (the .captured event
	// arrives before persistence is confirmed).
	return nil
}

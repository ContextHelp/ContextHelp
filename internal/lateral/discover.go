package lateral

import (
	"context"

	"hop.top/kit/go/runtime/bus"
)

// DiscoverConfig wires a Discover's collaborators.
type DiscoverConfig struct {
	Bus      bus.Bus
	Registry *Registry
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

func (d *Discover) handlePersisted(_ context.Context, _ bus.Event) error {
	// T04 fills in: read parent from canonical store, dispatch strategies, etc.
	return nil
}

func (d *Discover) handleCaptured(_ context.Context, _ bus.Event) error {
	// T04 fills in: read-with-retry for pipelines that don't emit .persisted.
	return nil
}

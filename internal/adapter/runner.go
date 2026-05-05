package adapter

import (
	"context"
	"fmt"

	"hop.top/kit/go/runtime/bus"
)

// Runner orchestrates an Adapter's lifecycle and emits lifecycle
// events on the bus. The Runner is stateless w.r.t. registered
// adapters — the daemon owns the Registry and calls Start / Stop on
// each adapter through this Runner.
type Runner struct {
	bus bus.Bus
}

// NewRunner returns a Runner that publishes lifecycle events on b.
func NewRunner(b bus.Bus) *Runner {
	return &Runner{bus: b}
}

// Start drives the adapter from Stopped → Starting → Ready, emitting
// dpkms.adapter.lifecycle.started before invoking Adapter.Start and
// dpkms.adapter.lifecycle.readied after Start returns nil.
//
// On Adapter.Start error: the started event has already fired (so
// observers see the transition began), but readied is suppressed and
// the error is returned wrapped with adapter identity.
func (r *Runner) Start(ctx context.Context, a Adapter) error {
	if err := r.publish(ctx, LifecycleTopic("started"), a); err != nil {
		return fmt.Errorf("publish started: %w", err)
	}
	if err := a.Start(ctx, r.bus); err != nil {
		return fmt.Errorf("adapter %s/%s: start: %w", a.Protocol(), a.Backend(), err)
	}
	if err := r.publish(ctx, LifecycleTopic("readied"), a); err != nil {
		return fmt.Errorf("publish readied: %w", err)
	}
	return nil
}

// Stop drives the adapter from Ready → Draining → Stopped, calling
// Drain and Stop in order and emitting dpkms.adapter.lifecycle.drained
// after Drain and dpkms.adapter.lifecycle.stopped after Stop.
//
// Errors from Drain or Stop are joined and returned; events still fire
// so observers see the transition complete even on partial failure.
// The two methods are always invoked in order: a Drain failure does
// NOT skip Stop, since Stop must release resources regardless.
func (r *Runner) Stop(ctx context.Context, a Adapter) error {
	drainErr := a.Drain(ctx)
	if err := r.publish(ctx, LifecycleTopic("drained"), a); err != nil {
		return fmt.Errorf("publish drained: %w", err)
	}
	stopErr := a.Stop(ctx)
	if err := r.publish(ctx, LifecycleTopic("stopped"), a); err != nil {
		return fmt.Errorf("publish stopped: %w", err)
	}
	switch {
	case drainErr != nil && stopErr != nil:
		return fmt.Errorf("adapter %s/%s: drain: %v; stop: %w", a.Protocol(), a.Backend(), drainErr, stopErr)
	case drainErr != nil:
		return fmt.Errorf("adapter %s/%s: drain: %w", a.Protocol(), a.Backend(), drainErr)
	case stopErr != nil:
		return fmt.Errorf("adapter %s/%s: stop: %w", a.Protocol(), a.Backend(), stopErr)
	}
	return nil
}

// publish emits a kit/bus event tagged with the adapter's
// protocol+backend identity so subscribers can route to specific
// adapters without re-decoding the source string.
func (r *Runner) publish(ctx context.Context, topic string, a Adapter) error {
	return r.bus.Publish(ctx, bus.NewEvent(
		bus.Topic(topic),
		fmt.Sprintf("%s.%s", a.Protocol(), a.Backend()),
		map[string]any{
			"protocol": a.Protocol(),
			"backend":  a.Backend(),
		},
	))
}

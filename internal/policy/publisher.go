package policy

import (
	"context"

	"hop.top/kit/go/runtime/bus"
)

// busPublisher adapts kit/runtime/bus.Bus to
// kit/runtime/domain.EventPublisher so domain.Service[T] mutations fire
// kit.runtime.entity.* topics on the daemon bus. Sync subscribers
// (the policy engine wired in Init) veto by returning an error from
// their Handler, which the bus surfaces back through Publish.
//
// Adapter is intentionally minimal: same Bus that policy.Wire
// subscribes to, no goroutine fan-out, no transformation. Callers get
// the kit topic shape ("kit.runtime.entity.pre_persisted") that
// policy.Engine already understands.
type busPublisher struct {
	bus bus.Bus
}

func newBusPublisher(b bus.Bus) *busPublisher {
	if b == nil {
		return nil
	}
	return &busPublisher{bus: b}
}

// Publish satisfies domain.EventPublisher. Returning an error vetoes
// the in-flight domain.Service operation; the policy engine returns
// *policy.PolicyDeniedError, which wraps domain.ErrConflict so the
// HTTP layer's existing ErrConflict mapping picks it up.
func (p *busPublisher) Publish(ctx context.Context, topic, source string, payload any) error {
	if p == nil || p.bus == nil {
		return nil
	}
	ev := bus.NewEvent(bus.Topic(topic), source, payload)
	return p.bus.Publish(ctx, ev)
}

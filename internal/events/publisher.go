package events

import (
	"context"

	"hop.top/kit/go/runtime/bus"
)

const source = "ctxt"

// Publisher wraps bus.Bus for domain event publishing.
type Publisher struct {
	bus bus.Bus
}

// NewPublisher returns a Publisher wired to the given bus.
func NewPublisher(b bus.Bus) *Publisher {
	return &Publisher{bus: b}
}

// Publish sends an event on the bus with the ctxt source.
func (p *Publisher) Publish(ctx context.Context, topic bus.Topic, payload any) error {
	return p.bus.Publish(ctx, bus.NewEvent(topic, source, payload))
}

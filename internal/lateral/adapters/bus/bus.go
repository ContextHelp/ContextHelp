// Package bus adapts hop.top/kit/go/runtime/bus.Bus into the
// kitdomain.EventPublisher interface lateral strategies depend on.
//
// # Why an adapter
//
// Lateral's emit helpers (jit.EmitProposalFailure, github failure.go,
// etc.) consume kitdomain.EventPublisher:
//
//	Publish(ctx, topic string, source string, payload any) error
//
// kit/runtime/bus.Bus has a different shape:
//
//	Publish(ctx, e bus.Event) error
//
// where Event is constructed via bus.NewEvent(topic, source, payload).
// The shapes are intentionally distinct: domain.EventPublisher is a
// transport-style 3-tuple that any pub/sub backend can implement;
// bus.Event carries kit-specific metadata (timestamp, validation
// state) that doesn't belong in lateral's contract.
//
// This adapter is the canonical bridge — same shape kit's own tests
// use (cross_module_topics_test.go).
package bus

import (
	"context"

	kitbus "hop.top/kit/go/runtime/bus"
	kitdomain "hop.top/kit/go/runtime/domain"
)

// Publisher is the kit/runtime/bus subset the adapter calls. The real
// bus.Bus interface satisfies it; tests inject a stub.
type Publisher interface {
	Publish(ctx context.Context, e kitbus.Event) error
}

// Adapter implements kitdomain.EventPublisher by constructing a
// bus.Event for each call and forwarding to the wrapped Publisher.
type Adapter struct {
	publisher Publisher
}

// New wraps p. Panics on nil — the daemon constructs exactly one bus
// per process and hands it to every strategy that needs to publish.
func New(p Publisher) *Adapter {
	if p == nil {
		panic("bus.New: publisher is nil")
	}
	return &Adapter{publisher: p}
}

// kitdomain.EventPublisher contract.
var _ kitdomain.EventPublisher = (*Adapter)(nil)

// Publish builds a bus.Event from (topic, source, payload) and
// delegates to the wrapped Bus. Errors propagate verbatim — kit's
// validation / sink-failure semantics are visible to lateral callers
// (the github failure.go and jit pipeline.go publishers tolerate nil
// publishers but DO surface publish errors when configured).
func (a *Adapter) Publish(ctx context.Context, topic, source string, payload any) error {
	return a.publisher.Publish(ctx, kitbus.NewEvent(kitbus.Topic(topic), source, payload))
}

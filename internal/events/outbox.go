package events

import (
	"context"
	"time"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Origins an after event records (event_outbox.origin).
const (
	OriginAPI = "api"
	OriginJob = "job"
)

// OutboxTopics are the topics the event outbox records: the object
// events (ADR-075 §4). The SSE stream serves them from the outbox, so
// each carries an id and can be replayed; the bus carries the same
// events live and only wakes the stream up for these.
var OutboxTopics = map[bus.Topic]bool{
	TopicObjectIngested:  true,
	TopicObjectUpdated:   true,
	TopicObjectDeleted:   true,
	TopicObjectRawStored: true,
}

// Announce attaches the after event of a mutation to ctx. The object
// store writes it to event_outbox in the mutation's transaction
// (storage.WithOutboxEvent); call Publish with the returned event once
// the mutation succeeded. A mutation that fails leaves no row and
// Publish is not called.
func Announce(ctx context.Context, topic bus.Topic, source, origin, actor, objectID string, payload any) (context.Context, *storage.OutboxEvent) {
	ev := &storage.OutboxEvent{
		Topic:      string(topic),
		Source:     source,
		Origin:     origin,
		Actor:      actor,
		ObjectID:   objectID,
		Payload:    payload,
		OccurredAt: time.Now().UTC(),
	}
	return storage.WithOutboxEvent(ctx, ev), ev
}

// Publish sends the live copy of a committed after event on the bus,
// stamped with the time the row records. The stream reads the row, not
// this copy: the bus event is the signal that a row is there. Delivery
// is best effort (K6); the row is the durable record.
func Publish(ctx context.Context, b bus.Bus, ev *storage.OutboxEvent) error {
	if b == nil || ev == nil {
		return nil
	}
	e := bus.NewEvent(bus.Topic(ev.Topic), ev.Source, ev.Payload)
	e.Timestamp = ev.OccurredAt
	return b.Publish(ctx, e)
}

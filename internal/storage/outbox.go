package storage

import (
	"context"
	"errors"
	"time"
)

// ErrOutboxExpired is returned by OutboxStore.Since when the cursor
// precedes the oldest retained row: rows after it were pruned, so a
// replay from it would have a silent gap (ADR-075 §4). The error carries
// the oldest retained seq through OutboxExpiredError.
var ErrOutboxExpired = errors.New("event outbox: cursor older than retention")

// OutboxExpiredError reports the retention floor a stale cursor fell
// behind. It wraps ErrOutboxExpired.
type OutboxExpiredError struct {
	// Oldest is the oldest seq still retained; replay can resume from
	// Oldest-1 onwards.
	Oldest int64
}

func (e *OutboxExpiredError) Error() string { return ErrOutboxExpired.Error() }
func (e *OutboxExpiredError) Unwrap() error { return ErrOutboxExpired }

// OutboxEvent is one after event bound for the event_outbox table
// (ADR-075 §4). The id, instance id, and seq are the store's: ID,
// InstanceID and Seq are filled when the row is written.
type OutboxEvent struct {
	Topic string
	// Source names the emitting component, as the bus event's source.
	Source string
	// Origin is the surface the change came in through: "api" for a
	// request, "job" for the worker pool.
	Origin string
	// Actor is the authenticated principal behind the change; empty for
	// system work.
	Actor string
	// ObjectID is the object the event announces; empty when none.
	ObjectID string
	// Payload is the event's data: a JSON-marshalable value or a
	// json.RawMessage.
	Payload any
	// OccurredAt defaults to the write time when zero.
	OccurredAt time.Time

	// Set by the store once the row is written.
	Seq        int64
	ID         string
	InstanceID string
}

// OutboxRow is a stored outbox row.
type OutboxRow struct {
	// Seq is the monotonic position of the row; the SSE id.
	Seq int64
	// ID is the row's UUIDv7; consumers dedupe on it.
	ID         string
	Topic      string
	Source     string
	OccurredAt time.Time
	Origin     string
	Actor      string
	InstanceID string
	ObjectID   string
	// Payload is the event data as stored JSON.
	Payload []byte
}

// OutboxStore is the transactional event outbox (ADR-075 §4): after
// events written in the mutation's own transaction, read back in seq
// order, pruned after the retention window. It is separate from
// ADR-074's sync_log.
type OutboxStore interface {
	// Append writes ev in a transaction of its own. Mutations do not
	// call it: the object store writes the event it finds on the
	// context (WithOutboxEvent) in the mutation's transaction.
	Append(ctx context.Context, ev *OutboxEvent) error
	// Since returns up to limit rows with seq greater than after, in
	// seq order (limit <= 0 means 500). It fails with an
	// *OutboxExpiredError when rows after the cursor were pruned.
	Since(ctx context.Context, after int64, limit int) ([]OutboxRow, error)
	// Head is the seq of the newest row ever written, 0 when none was.
	// It survives pruning.
	Head(ctx context.Context) (int64, error)
	// Oldest is the oldest retained seq, 0 when no row is retained.
	Oldest(ctx context.Context) (int64, error)
	// Prune deletes rows that occurred before cutoff and returns how
	// many. The newest row is always kept, so Head stays derivable and
	// a cursor at the head is never expired.
	Prune(ctx context.Context, cutoff time.Time) (int64, error)
}

type outboxCtxKey struct{}

// WithOutboxEvent attaches ev to ctx. The object store's Create,
// Update and Delete write it to event_outbox in the transaction of the
// mutation, and set ev.Seq, ev.ID and ev.InstanceID once it commits;
// a mutation that fails writes nothing. The caller then publishes the
// live event on the bus.
func WithOutboxEvent(ctx context.Context, ev *OutboxEvent) context.Context {
	return context.WithValue(ctx, outboxCtxKey{}, ev)
}

// OutboxEventFrom returns the event attached by WithOutboxEvent, or nil.
func OutboxEventFrom(ctx context.Context) *OutboxEvent {
	ev, _ := ctx.Value(outboxCtxKey{}).(*OutboxEvent)
	return ev
}

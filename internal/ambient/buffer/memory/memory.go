// Package memory is the in-memory ambient.Buffer backend (per ADR-066
// §Decision item 5).
//
// Tests only. Not user-facing — production deployments use the local-FS
// backend (T-0506) by default or S3 (T-0510) for cross-machine /
// compliance retention. This backend exists so substrate tests can
// exercise the Buffer interface without disk dependencies.
//
// Implementation is a bounded ring keyed by insertion order. When full,
// Append evicts the oldest event (LRU policy) and increments the
// EvictedTotal counter. This differs from production FS/S3 backends that
// typically reject-and-emit-bus-event so the Runner can surface
// backpressure to the user.
package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// Default capacity for the in-memory backend. Tests usually configure their
// own via WithCapacity; the default is sized for substrate-test scenarios
// where event counts are small.
const DefaultCapacity = 1024

// Buffer is the in-memory ambient.Buffer backend.
//
// Goroutine-safe via single sync.Mutex. Counters are sync/atomic for
// lock-free Stats reads.
type Buffer struct {
	mu       sync.Mutex
	capacity int
	events   []ambient.RawEvent

	appendedTotal atomic.Uint64
	poppedTotal   atomic.Uint64
	evictedTotal  atomic.Uint64
}

// Option configures Buffer construction.
type Option func(*Buffer)

// WithCapacity sets the maximum number of events the buffer will hold
// before evicting LRU. Values < 1 fall back to DefaultCapacity.
func WithCapacity(capacity int) Option {
	return func(b *Buffer) { b.capacity = capacity }
}

// New constructs an in-memory buffer with the supplied options.
func New(opts ...Option) *Buffer {
	b := &Buffer{capacity: DefaultCapacity}
	for _, opt := range opts {
		opt(b)
	}
	if b.capacity < 1 {
		b.capacity = DefaultCapacity
	}
	b.events = make([]ambient.RawEvent, 0, b.capacity)
	return b
}

// Append adds ev to the buffer. When at capacity, the oldest event is
// evicted to make room. Memory backend never returns ErrBufferFull.
func (b *Buffer) Append(_ context.Context, ev ambient.RawEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) >= b.capacity {
		// Evict oldest (front of slice).
		b.events = b.events[1:]
		b.evictedTotal.Add(1)
	}
	b.events = append(b.events, ev)
	b.appendedTotal.Add(1)
	return nil
}

// Pop removes and returns the oldest buffered event. Returns
// ambient.ErrBufferEmpty when the buffer is empty.
func (b *Buffer) Pop(_ context.Context) (ambient.RawEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == 0 {
		return ambient.RawEvent{}, ambient.ErrBufferEmpty
	}
	ev := b.events[0]
	b.events = b.events[1:]
	b.poppedTotal.Add(1)
	return ev, nil
}

// Range iterates buffered events oldest-first without removing them.
// Implementation takes a snapshot under lock so callers can run for
// arbitrary durations without blocking concurrent Append / Pop. fn
// returning false stops iteration.
func (b *Buffer) Range(_ context.Context, fn func(ambient.RawEvent) bool) error {
	b.mu.Lock()
	snapshot := make([]ambient.RawEvent, len(b.events))
	copy(snapshot, b.events)
	b.mu.Unlock()
	for _, ev := range snapshot {
		if !fn(ev) {
			return nil
		}
	}
	return nil
}

// Len returns the current number of buffered events.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

// Stats returns a snapshot of buffer state for observability.
func (b *Buffer) Stats() ambient.BufferStats {
	b.mu.Lock()
	count := len(b.events)
	var oldest time.Time
	if count > 0 {
		oldest = b.events[0].OccurredAt
	}
	cap := b.capacity
	b.mu.Unlock()
	return ambient.BufferStats{
		Backend:       "memory",
		Count:         count,
		Capacity:      cap,
		OldestEventAt: oldest,
		AppendedTotal: b.appendedTotal.Load(),
		PoppedTotal:   b.poppedTotal.Load(),
		EvictedTotal:  b.evictedTotal.Load(),
	}
}

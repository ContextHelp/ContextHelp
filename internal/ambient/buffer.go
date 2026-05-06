package ambient

import (
	"context"
	"errors"
	"time"
)

// Buffer is the substrate's contract for client-side staging of RawEvents
// awaiting enqueue (per ADR-066 §Decision item 5). Backends:
//
//	internal/ambient/buffer/memory  — bounded in-memory ring; tests only
//	internal/ambient/buffer/local   — XDG-compliant filesystem (default)   [T-0506]
//	internal/ambient/buffer/s3      — S3-compatible (AWS/R2/B2/MinIO)      [T-0510]
//
// The Buffer is goroutine-safe; the Runner appends from its dispatch
// goroutine, the enqueue worker reads via Drain / Range / Pop, and status /
// MCP read paths inspect Len + Stats concurrently.
//
// The Runner does NOT yet hold a Buffer reference — buffering is wired in
// Phase 4 (T-0506 retention + T-0510 S3 backend). T-0511 lands the
// interface + the in-memory backend so Phase 2 substrate tests can exercise
// the eventual integration without waiting for the FS backend.
type Buffer interface {
	// Append adds ev to the buffer. Returns ErrBufferFull if the buffer is
	// at capacity AND the backend's overflow policy is to reject (vs. evict
	// LRU). Memory backend evicts; production FS/S3 backends usually
	// reject-and-emit-bus-event so the Runner can surface backpressure.
	Append(ctx context.Context, ev RawEvent) error

	// Pop removes and returns the oldest buffered event. Returns
	// ErrBufferEmpty when no events are buffered.
	Pop(ctx context.Context) (RawEvent, error)

	// Range iterates buffered events oldest-first without removing them.
	// fn returning false stops iteration. Range is safe to call
	// concurrently with Append; it observes a snapshot.
	Range(ctx context.Context, fn func(RawEvent) bool) error

	// Len returns the number of buffered events.
	Len() int

	// Stats returns a snapshot of buffer state for observability (status
	// CLI, MCP health tool). Fields are backend-specific via the Extra map.
	Stats() BufferStats
}

// BufferStats is the observability surface every Buffer backend exposes. New
// fields go in Extra; named fields stay stable across backends.
type BufferStats struct {
	// Backend identifies the implementation (e.g. "memory", "local-fs", "s3").
	Backend string
	// Count is the current number of buffered events.
	Count int
	// Capacity is the maximum number of events the backend will hold before
	// applying its overflow policy.
	Capacity int
	// OldestEventAt is the OccurredAt of the oldest buffered event, or zero
	// when the buffer is empty.
	OldestEventAt time.Time
	// AppendedTotal is the total number of events appended over the
	// lifetime of this Buffer instance (cumulative; not affected by Pop).
	AppendedTotal uint64
	// PoppedTotal is the total number of events popped (consumed) over
	// the lifetime of this Buffer instance.
	PoppedTotal uint64
	// EvictedTotal is the total number of events evicted by overflow
	// policy (LRU / TTL). Distinct from PoppedTotal which counts
	// successful consumption.
	EvictedTotal uint64
	// Extra holds backend-specific metrics (bytes-on-disk for local-fs,
	// objects-uploaded for s3, etc.). Read-only; the substrate does not
	// mutate it.
	Extra map[string]any
}

// ErrBufferEmpty is returned by Pop when the buffer has no events.
var ErrBufferEmpty = errors.New("ambient: buffer empty")

// ErrBufferFull is returned by Append when the buffer is at capacity and
// the backend's overflow policy is to reject. Memory backend evicts LRU
// instead of returning this error.
var ErrBufferFull = errors.New("ambient: buffer full")

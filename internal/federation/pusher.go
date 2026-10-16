package federation

import (
	"context"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Pusher pushes objects, edges, and referenced entities to a federation target.
//
// Mentions per ADR-049 are object→entity edges with edge_type='mentions'.
// They live in the edges slice. The entities slice carries the entity rows
// those mention edges point at, so the receiver can persist them and avoid
// dangling references (US-0319 AC #6).
//
// Each target keeps a watermark: the ChangeSeq of the last object it
// acknowledged. Sync pushes only objects with a greater ChangeSeq.
type Pusher interface {
	// Name returns the federation entry name from config.
	Name() string
	// Watermark returns the target's watermark; 0 when it has
	// acknowledged nothing.
	Watermark(ctx context.Context) (int64, error)
	// Push upserts objects, edges, and entities into the target. Idempotent
	// via content-hash dedup on objects and slug-based upsert on entities.
	// objects are in ChangeSeq order: once the target has acknowledged
	// them, the watermark moves to the last one's ChangeSeq. On error it
	// stays at the last object acknowledged before the failure. A failure
	// down to one object rather than the target is an *ObjectError.
	Push(ctx context.Context,
		objects []storage.KnowledgeObject,
		edges []storage.Edge,
		entities []storage.Entity) error
	// Advance moves the watermark to seq. Sync calls it once every
	// object up to seq is acknowledged or skipped, when the last of
	// them was skipped and Push could not move it there.
	Advance(ctx context.Context, seq int64) error
}

// ObjectError is a push failure down to one object, not the target:
// the source blob store does not hold its blob, or the receiver refuses
// it even sent alone. Retrying the target does not help; Sync skips
// the object after maxObjectFailures in a row.
type ObjectError struct {
	ID  string // the object's id
	Err error
}

func (e *ObjectError) Error() string { return fmt.Sprintf("object %q: %v", e.ID, e.Err) }

func (e *ObjectError) Unwrap() error { return e.Err }

// objectBlobError classifies err, reading object id's source blob: a
// blob the store reports missing (storage.ErrBlobNotFound) is the
// object's failure, an *ObjectError. Anything else — the store
// unreachable, timing out, answering 5xx, or no store at all — fails
// the push without counting toward a skip, so the next sync retries.
func objectBlobError(id string, err error) error {
	if errors.Is(err, storage.ErrBlobNotFound) {
		return &ObjectError{ID: id, Err: err}
	}
	return fmt.Errorf("object %q: %w", id, err)
}

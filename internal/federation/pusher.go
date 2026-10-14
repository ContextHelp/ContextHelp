package federation

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Pusher pushes objects, edges, and referenced entities to a federation target.
//
// Mentions per ADR-049 are object→entity edges with edge_type='mentions'.
// They live in the edges slice. The entities slice carries the entity rows
// those mention edges point at, so the receiver can persist them and avoid
// dangling references. T-0175 added entities to satisfy US-0319 AC #6.
//
// Each target keeps a watermark: the change position (storage.ChangeCursor)
// of the last object it acknowledged. Sync pushes only objects changed
// after it.
type Pusher interface {
	// Name returns the federation entry name from config.
	Name() string
	// Watermark returns the target's watermark; the zero position when
	// it has acknowledged nothing.
	Watermark(ctx context.Context) (storage.ChangeCursor, error)
	// Push upserts objects, edges, and entities into the target. Idempotent
	// via content-hash dedup on objects and slug-based upsert on entities.
	// objects are in change order: once the target has acknowledged
	// them, the watermark moves to the last one. On error it stays at the
	// last object acknowledged before the failure.
	Push(ctx context.Context,
		objects []storage.KnowledgeObject,
		edges []storage.Edge,
		entities []storage.Entity) error
}

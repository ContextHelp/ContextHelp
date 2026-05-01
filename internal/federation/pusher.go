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
type Pusher interface {
	// Name returns the federation entry name from config.
	Name() string
	// Push upserts objects, edges, and entities into the target. Idempotent
	// via content-hash dedup on objects and slug-based upsert on entities.
	Push(ctx context.Context,
		objects []storage.KnowledgeObject,
		edges []storage.Edge,
		entities []storage.Entity) error
}

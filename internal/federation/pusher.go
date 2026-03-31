package federation

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Pusher pushes objects and their edges to a federation target.
type Pusher interface {
	// Name returns the federation entry name from config.
	Name() string
	// Push upserts objects + edges into the target. Idempotent via content-hash dedup.
	Push(ctx context.Context, objects []storage.KnowledgeObject, edges []storage.Edge) error
}

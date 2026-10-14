package federation

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DefaultBatchSize caps the objects one Push carries.
const DefaultBatchSize = 100

// Sync pushes to p every object of src whose ChangeSeq is after p's
// watermark, in ChangeSeq order, at most batchSize objects per Push
// (DefaultBatchSize when batchSize <= 0), each with the edges from it
// and the entities those edges mention. It stops at the first failed
// Push; batches before it stay acknowledged. Returns the number of
// objects pushed.
//
// The store stamps ChangeSeq on every write and commits the values in
// increasing order, so everything up to the last object listed has
// landed: no change can later appear behind the watermark, whatever
// updated_at it carries (an import, a row a receiver forwards on, a
// restore).
func Sync(ctx context.Context, src storage.StorageDriver, p Pusher, batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	pos, err := p.Watermark(ctx)
	if err != nil {
		return 0, fmt.Errorf("read watermark: %w", err)
	}
	pushed := 0
	for {
		page, _, err := src.Objects().List(ctx, storage.ObjectFilter{
			Status:       "all",
			ChangedAfter: &pos,
			Limit:        batchSize,
		})
		if err != nil {
			return pushed, fmt.Errorf("list changed source objects: %w", err)
		}
		if len(page) == 0 {
			return pushed, nil
		}
		if err := pushPage(ctx, src, p, page); err != nil {
			return pushed, err
		}
		pushed += len(page)
		pos = page[len(page)-1].ChangeSeq
		if len(page) < batchSize {
			return pushed, nil
		}
	}
}

// pushPage hands one page of objects, with their edges and the entities
// those edges mention, to p. Entities accompany mention edges (ADR-049)
// so the receiver can resolve to_id slugs without a dangling reference.
func pushPage(ctx context.Context, src storage.StorageDriver, p Pusher, page []*storage.KnowledgeObject) error {
	objects := make([]storage.KnowledgeObject, 0, len(page))
	edges := make([]storage.Edge, 0)
	for _, o := range page {
		objects = append(objects, *o)
		from, err := src.Edges().ListFrom(ctx, "object", o.ID)
		if err != nil {
			return fmt.Errorf("list source edges from %q: %w", o.ID, err)
		}
		for _, e := range from {
			edges = append(edges, *e)
		}
	}
	entities, err := collectEntitiesForEdges(ctx, src, edges)
	if err != nil {
		return fmt.Errorf("collect mention entities: %w", err)
	}
	return p.Push(ctx, objects, edges, entities)
}

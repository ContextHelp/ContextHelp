package federation

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DefaultBatchSize caps the objects one Push carries.
const DefaultBatchSize = 100

// settleLag holds the most recent changes back from a worker tick.
// updated_at is stamped before the write commits and stored to the
// second, so a write still committing, or landing later in the second a
// tick ran in, can sort before the watermark that tick left and would
// never be pushed. Changes older than settleLag have landed.
const settleLag = 2 * time.Second

// Sync pushes to p every object of src changed after p's watermark and
// no later than until, in change order, at most batchSize objects per
// Push (DefaultBatchSize when batchSize <= 0), each with the edges from
// it and the entities those edges mention. It stops at the first failed
// Push; batches before it stay acknowledged. Returns the number of
// objects pushed.
func Sync(ctx context.Context, src storage.StorageDriver, p Pusher, until time.Time, batchSize int) (int, error) {
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
			ChangedUntil: &until,
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
		pos = storage.ChangeCursorOf(page[len(page)-1])
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

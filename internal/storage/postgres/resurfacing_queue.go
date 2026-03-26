package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ResurfacingQueueStore is a placeholder for the postgres backend.
// Full implementation deferred until postgres parity sprint.
type ResurfacingQueueStore struct{}

func (s *ResurfacingQueueStore) Upsert(_ context.Context, _ *storage.ResurfacingEntry) error {
	return fmt.Errorf("resurfacing_queue: postgres backend not yet implemented")
}

func (s *ResurfacingQueueStore) List(
	_ context.Context, _ storage.ResurfacingFilter,
) ([]*storage.ResurfacingEntry, error) {
	return nil, fmt.Errorf("resurfacing_queue: postgres backend not yet implemented")
}

func (s *ResurfacingQueueStore) MarkSurfaced(_ context.Context, _ string, _ time.Time) error {
	return fmt.Errorf("resurfacing_queue: postgres backend not yet implemented")
}

func (s *ResurfacingQueueStore) Dismiss(_ context.Context, _ string, _ time.Time) error {
	return fmt.Errorf("resurfacing_queue: postgres backend not yet implemented")
}

func (s *ResurfacingQueueStore) DeleteByProfile(_ context.Context, _ string) error {
	return fmt.Errorf("resurfacing_queue: postgres backend not yet implemented")
}

func (s *ResurfacingQueueStore) DeleteByObject(_ context.Context, _ string) error {
	return fmt.Errorf("resurfacing_queue: postgres backend not yet implemented")
}

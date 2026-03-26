package postgres

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MeteringStore is a placeholder for the postgres backend.
// Full implementation deferred until postgres parity sprint.
type MeteringStore struct{}

func (s *MeteringStore) Record(_ context.Context, _ *storage.MeteringEvent) error {
	return fmt.Errorf("metering: postgres backend not yet implemented")
}

func (s *MeteringStore) Aggregate(
	_ context.Context, _ storage.MeteringFilter,
) ([]*storage.MeteringAggregate, error) {
	return nil, fmt.Errorf("metering: postgres backend not yet implemented")
}

func (s *MeteringStore) List(
	_ context.Context, _ storage.MeteringFilter,
) ([]*storage.MeteringEvent, error) {
	return nil, fmt.Errorf("metering: postgres backend not yet implemented")
}

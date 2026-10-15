package postgres

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SavedSearchStore is a placeholder for the postgres backend.
// Full implementation deferred until postgres parity sprint. Every
// method wraps storage.ErrNotImplemented so callers and conformance
// suites can tell the gap from a failure.
type SavedSearchStore struct{}

func (s *SavedSearchStore) Create(_ context.Context, _ *storage.SavedSearch) error {
	return fmt.Errorf("saved_searches: postgres backend: %w", storage.ErrNotImplemented)
}

func (s *SavedSearchStore) GetByName(_ context.Context, _ string) (*storage.SavedSearch, error) {
	return nil, fmt.Errorf("saved_searches: postgres backend: %w", storage.ErrNotImplemented)
}

func (s *SavedSearchStore) List(
	_ context.Context, _ storage.SavedSearchFilter,
) ([]*storage.SavedSearch, error) {
	return nil, fmt.Errorf("saved_searches: postgres backend: %w", storage.ErrNotImplemented)
}

func (s *SavedSearchStore) Update(_ context.Context, _ *storage.SavedSearch) error {
	return fmt.Errorf("saved_searches: postgres backend: %w", storage.ErrNotImplemented)
}

func (s *SavedSearchStore) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("saved_searches: postgres backend: %w", storage.ErrNotImplemented)
}

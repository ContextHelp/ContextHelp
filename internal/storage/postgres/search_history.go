package postgres

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SearchHistoryStore is a placeholder for the postgres backend.
// Full implementation deferred until postgres parity sprint.
type SearchHistoryStore struct{}

func (s *SearchHistoryStore) Append(_ context.Context, _ *storage.SearchHistoryEntry) error {
	return fmt.Errorf("search_history: postgres backend not yet implemented")
}

func (s *SearchHistoryStore) List(
	_ context.Context, _ storage.SearchHistoryFilter,
) ([]*storage.SearchHistoryEntry, error) {
	return nil, fmt.Errorf("search_history: postgres backend not yet implemented")
}

func (s *SearchHistoryStore) ClearByProfile(_ context.Context, _ string) error {
	return fmt.Errorf("search_history: postgres backend not yet implemented")
}

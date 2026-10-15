package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

// SearchHistoryStore implements storage.SearchHistoryStore for SQLite.
type SearchHistoryStore struct {
	db *sql.DB
}

const searchHistoryColumns = `id, query, profile_id, strategies_used, result_count, owner_principal, searched_at`

// Append records e. An empty OwnerPrincipal is stored as the instance
// owner.
func (s *SearchHistoryStore) Append(ctx context.Context, e *storage.SearchHistoryEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO search_history
			(`+searchHistoryColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Query, e.ProfileID, e.StrategiesUsed, e.ResultCount,
		storage.PrincipalOrOwner(e.OwnerPrincipal),
		e.SearchedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("search_history append: %w", err)
	}
	return nil
}

func (s *SearchHistoryStore) List(
	ctx context.Context, f storage.SearchHistoryFilter,
) ([]*storage.SearchHistoryEntry, error) {
	query, args := principalPage(`SELECT `+searchHistoryColumns+`
		FROM search_history WHERE 1=1`, " ORDER BY searched_at DESC", f.OwnerPrincipal, f.ProfileID, f.Limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search_history list: %w", err)
	}
	defer rows.Close()

	return rowscan.AllScanner(rows, scanSearchHistoryEntry)
}

func (s *SearchHistoryStore) ClearByProfile(ctx context.Context, profileID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM search_history WHERE profile_id = ?`, profileID,
	)
	if err != nil {
		return fmt.Errorf("search_history clear: %w", err)
	}
	return nil
}

func scanSearchHistoryEntry(row interface{ Scan(...any) error }) (*storage.SearchHistoryEntry, error) {
	var e storage.SearchHistoryEntry
	var searchedAt string
	if err := row.Scan(
		&e.ID, &e.Query, &e.ProfileID, &e.StrategiesUsed, &e.ResultCount, &e.OwnerPrincipal, &searchedAt,
	); err != nil {
		return nil, fmt.Errorf("scan search_history: %w", err)
	}
	var err error
	e.SearchedAt, err = time.Parse(time.RFC3339, searchedAt)
	if err != nil {
		return nil, fmt.Errorf("parse search_history searched_at: %w", err)
	}
	return &e, nil
}

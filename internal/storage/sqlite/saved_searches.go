package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

// SavedSearchStore implements storage.SavedSearchStore for SQLite.
type SavedSearchStore struct {
	db *sql.DB
}

const savedSearchColumns = `id, name, query, profile_id, alert_on, notify, owner_principal, created_at, updated_at`

// Create inserts ss. An empty OwnerPrincipal is stored as the instance
// owner; ownership is fixed at creation (Update leaves it alone).
func (s *SavedSearchStore) Create(ctx context.Context, ss *storage.SavedSearch) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO saved_searches
			(`+savedSearchColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ss.ID, ss.Name, ss.Query, ss.ProfileID, ss.AlertOn, ss.Notify,
		storage.PrincipalOrOwner(ss.OwnerPrincipal),
		ss.CreatedAt.UTC().Format(time.RFC3339),
		ss.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("saved_search create: %w", err)
	}
	return nil
}

func (s *SavedSearchStore) GetByName(ctx context.Context, name string) (*storage.SavedSearch, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+savedSearchColumns+`
		FROM saved_searches WHERE name = ?`, name)
	ss, err := scanSavedSearch(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ss, err
}

func (s *SavedSearchStore) List(
	ctx context.Context, f storage.SavedSearchFilter,
) ([]*storage.SavedSearch, error) {
	query, args := principalPage(`SELECT `+savedSearchColumns+`
		FROM saved_searches WHERE 1=1`, orderNewestFirst, f.OwnerPrincipal, f.ProfileID, f.Limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("saved_search list: %w", err)
	}
	defer rows.Close()

	return rowscan.AllScanner(rows, scanSavedSearch)
}

func (s *SavedSearchStore) Update(ctx context.Context, ss *storage.SavedSearch) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE saved_searches SET
			query = ?, profile_id = ?, alert_on = ?, notify = ?, updated_at = ?
		WHERE name = ?`,
		ss.Query, ss.ProfileID, ss.AlertOn, ss.Notify,
		ss.UpdatedAt.UTC().Format(time.RFC3339),
		ss.Name,
	)
	if err != nil {
		return fmt.Errorf("saved_search update: %w", err)
	}
	return nil
}

func (s *SavedSearchStore) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM saved_searches WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("saved_search delete: %w", err)
	}
	return nil
}

func scanSavedSearch(row interface{ Scan(...any) error }) (*storage.SavedSearch, error) {
	var ss storage.SavedSearch
	var createdAt, updatedAt string
	if err := row.Scan(
		&ss.ID, &ss.Name, &ss.Query, &ss.ProfileID,
		&ss.AlertOn, &ss.Notify, &ss.OwnerPrincipal, &createdAt, &updatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan saved_search: %w", err)
	}
	var err error
	ss.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse saved_search created_at: %w", err)
	}
	ss.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse saved_search updated_at: %w", err)
	}
	return &ss, nil
}

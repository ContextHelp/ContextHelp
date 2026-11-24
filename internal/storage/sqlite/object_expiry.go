package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

// SetExpiry sets expires_at (stored as UTC RFC3339) and optional natural-language
// expiry_scope on the object identified by id. Refuses with storage.ErrObjectHeld
// when the object carries an active hold.
func (s *ObjectStore) SetExpiry(ctx context.Context, id string, at time.Time, scope string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set expiry: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := assertNotHeldTx(ctx, tx, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE objects SET expires_at=?, expiry_scope=?, updated_at=? WHERE id=?`,
		at.UTC().Format(time.RFC3339), scope, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("set expiry: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s: %w", id, storage.ErrNotFound)
	}
	return tx.Commit()
}

// ClearExpiry removes expires_at and expiry_scope from the object identified by id.
// Refuses with storage.ErrObjectHeld when the object carries an active hold.
func (s *ObjectStore) ClearExpiry(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("clear expiry: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := assertNotHeldTx(ctx, tx, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE objects SET expires_at=NULL, expiry_scope='', updated_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("clear expiry: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("object %s: %w", id, storage.ErrNotFound)
	}
	return tx.Commit()
}

// ListExpired returns up to limit objects whose expires_at <= now across all
// statuses, ordered earliest expiry first. limit <= 0 returns all matches.
func (s *ObjectStore) ListExpired(ctx context.Context, now time.Time, limit int) ([]*storage.KnowledgeObject, error) {
	query := reminderSelectCols + `
	FROM objects
	WHERE expires_at IS NOT NULL AND expires_at <= ?
	ORDER BY expires_at ASC`
	args := []any{now.UTC().Format(time.RFC3339)}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list expired objects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return rowscan.All(rows, scanObjectFromRows)
}

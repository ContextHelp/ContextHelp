package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

// SetExpiry sets expires_at (UTC) and optional natural-language expiry_scope on
// the object identified by id. Refuses with storage.ErrObjectHeld when the
// object carries an active hold.
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
		`UPDATE objects SET expires_at=$1, expiry_scope=$2, updated_at=$3 WHERE id=$4`,
		at.UTC(), scope, time.Now().UTC(), id,
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
		`UPDATE objects SET expires_at=NULL, expiry_scope='', updated_at=$1 WHERE id=$2`,
		time.Now().UTC(), id,
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
	query := objectSelectCols + `
	FROM objects
	WHERE expires_at IS NOT NULL AND expires_at <= $1
	ORDER BY expires_at ASC`
	args := []any{now.UTC()}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list expired objects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return rowscan.All(rows, scanObjectRows)
}

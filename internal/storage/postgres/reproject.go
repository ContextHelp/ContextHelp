package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

var _ storage.ProjectionStore = (*ObjectStore)(nil)

// CountStaleProjections implements storage.ProjectionStore.
func (s *ObjectStore) CountStaleProjections(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM objects WHERE projection_version <> $1`,
		indexsig.ProjectionVersion).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count stale projections: %w", err)
	}
	return n, nil
}

// ListStaleProjections implements storage.ProjectionStore. Pages walk the
// primary key from after, so a run reads each row once however few are
// stale.
func (s *ObjectStore) ListStaleProjections(ctx context.Context, after string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM objects WHERE id > $1 AND projection_version <> $2 ORDER BY id LIMIT $3`,
		after, indexsig.ProjectionVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("list stale projections: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, max(limit, 0))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list stale projections: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Reproject implements storage.ProjectionStore. The row is locked (FOR
// UPDATE) while it is read and rewritten, so a concurrent Update waits for
// this transaction, and one that committed first leaves the row current,
// which is skipped. The generated tsvector column follows the rewritten
// body.
func (s *ObjectStore) Reproject(ctx context.Context, id string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("reproject %s: begin tx: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()

	obj, err := scanObjectRow(tx.QueryRowContext(ctx,
		objectSelectCols+` FROM objects WHERE id = $1 AND projection_version <> $2 FOR UPDATE`,
		id, indexsig.ProjectionVersion))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return false, nil // gone, or already current
		}
		return false, fmt.Errorf("reproject %s: load: %w", id, err)
	}

	body := projection.ProjectIndex(obj).FTSBody
	if _, err := tx.ExecContext(ctx, `UPDATE objects
		SET projected_fts_body = $1,
		    fts_indexed = CASE WHEN $1 <> '' THEN TRUE ELSE fts_indexed END,
		    projection_version = $2
		WHERE id = $3`, body, indexsig.ProjectionVersion, id); err != nil {
		return false, fmt.Errorf("reproject %s: write body: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("reproject %s: commit: %w", id, err)
	}
	return true, nil
}

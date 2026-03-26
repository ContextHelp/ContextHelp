package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ResurfacingQueueStore implements storage.ResurfacingQueueStore for SQLite.
type ResurfacingQueueStore struct {
	db *sql.DB
}

// Upsert inserts or replaces an entry keyed on (object_id, profile_id).
func (s *ResurfacingQueueStore) Upsert(ctx context.Context, e *storage.ResurfacingEntry) error {
	if e.ID == "" {
		return fmt.Errorf("resurfacing_queue upsert: empty id")
	}
	created := e.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO resurfacing_queue (id, object_id, profile_id, score, reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			score      = excluded.score,
			reason     = excluded.reason
	`,
		e.ID, e.ObjectID, e.ProfileID, e.Score, e.Reason,
		created.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("resurfacing_queue upsert: %w", err)
	}
	return nil
}

// List returns entries matching f, ordered by score DESC.
func (s *ResurfacingQueueStore) List(
	ctx context.Context, f storage.ResurfacingFilter,
) ([]*storage.ResurfacingEntry, error) {
	query := `SELECT id, object_id, profile_id, score, reason,
		surfaced_at, dismissed_at, created_at
	FROM resurfacing_queue
	WHERE 1=1`
	var args []any

	if f.ProfileID != "" {
		query += " AND profile_id = ?"
		args = append(args, f.ProfileID)
	}
	if f.UnseenOnly {
		query += " AND surfaced_at IS NULL AND dismissed_at IS NULL"
	}
	if f.MinScore > 0 {
		query += " AND score >= ?"
		args = append(args, f.MinScore)
	}
	query += " ORDER BY score DESC"
	if f.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("resurfacing_queue list: %w", err)
	}
	defer rows.Close()
	return collectResurfacingRows(rows)
}

// MarkSurfaced sets surfaced_at for the given entry id.
func (s *ResurfacingQueueStore) MarkSurfaced(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE resurfacing_queue SET surfaced_at=? WHERE id=?`,
		now.UTC().Format(time.RFC3339), id,
	)
	return err
}

// Dismiss sets dismissed_at for the given entry id.
func (s *ResurfacingQueueStore) Dismiss(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE resurfacing_queue SET dismissed_at=? WHERE id=?`,
		now.UTC().Format(time.RFC3339), id,
	)
	return err
}

// DeleteByProfile removes all entries for a profile.
func (s *ResurfacingQueueStore) DeleteByProfile(ctx context.Context, profileID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM resurfacing_queue WHERE profile_id=?`, profileID,
	)
	return err
}

// DeleteByObject removes all entries for an object.
func (s *ResurfacingQueueStore) DeleteByObject(ctx context.Context, objectID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM resurfacing_queue WHERE object_id=?`, objectID,
	)
	return err
}

func collectResurfacingRows(rows *sql.Rows) ([]*storage.ResurfacingEntry, error) {
	var out []*storage.ResurfacingEntry
	for rows.Next() {
		e := &storage.ResurfacingEntry{}
		var surfacedAt, dismissedAt, createdAt sql.NullString
		if err := rows.Scan(
			&e.ID, &e.ObjectID, &e.ProfileID, &e.Score, &e.Reason,
			&surfacedAt, &dismissedAt, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan resurfacing_queue row: %w", err)
		}
		if surfacedAt.Valid {
			if t, err := time.Parse(time.RFC3339, surfacedAt.String); err == nil {
				e.SurfacedAt = &t
			}
		}
		if dismissedAt.Valid {
			if t, err := time.Parse(time.RFC3339, dismissedAt.String); err == nil {
				e.DismissedAt = &t
			}
		}
		if createdAt.Valid {
			if t, err := time.Parse(time.RFC3339, createdAt.String); err == nil {
				e.CreatedAt = t
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

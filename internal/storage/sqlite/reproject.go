package sqlite

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
		`SELECT COUNT(*) FROM objects WHERE projection_version != ?`,
		indexsig.ProjectionVersion).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count stale projections: %w", err)
	}
	return n, nil
}

// ListStaleProjections implements storage.ProjectionStore. Pages walk the
// id index from after, so a run reads each row once however few are stale.
func (s *ObjectStore) ListStaleProjections(ctx context.Context, after string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM objects WHERE id > ? AND projection_version != ? ORDER BY id LIMIT ?`,
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

// Reproject implements storage.ProjectionStore.
//
// The transaction writes first: stamping the row takes SQLite's write lock
// before the row is read, so a concurrent Create/Update either committed
// before (the read sees it, and a row it already stamped is skipped) or
// waits until this commits. A read-first deferred transaction would fail
// with SQLITE_BUSY instead of queueing (see Reinforce).
func (s *ObjectStore) Reproject(ctx context.Context, id string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("reproject %s: begin tx: %w", id, err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE objects SET projection_version = ? WHERE id = ? AND projection_version != ?`,
		indexsig.ProjectionVersion, id, indexsig.ProjectionVersion)
	if err != nil {
		return false, fmt.Errorf("reproject %s: stamp: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil // gone, or already current
	}

	obj, err := scanObject(tx.QueryRowContext(ctx, `SELECT
		id, type, subtype, raw_content, content_type, text_content,
		metadata, summaries, sections, tags, mentions,
		decisions, tasks, pipeline, source,
		registry_influences, plugins, content_hash, reinforcement_count, last_reinforced_at,
		created_at, updated_at, fts_indexed, status, inbox_note,
		remind_at, reminded_at, profile_id, graph_json, source_key
	FROM objects WHERE id = ?`, id))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("reproject %s: load: %w", id, err)
	}
	var stored string
	if err := tx.QueryRowContext(ctx,
		`SELECT projected_fts_body FROM objects WHERE id = ?`, id).Scan(&stored); err != nil {
		return false, fmt.Errorf("reproject %s: load body: %w", id, err)
	}

	body := projection.ProjectIndex(obj).FTSBody
	if body != stored {
		// Same order as Update: drop the entry while the row still holds
		// the old body, rewrite, then index the new body.
		if err := deleteObjectFTSTx(ctx, tx, id); err != nil {
			return false, fmt.Errorf("reproject %s: drop fts entry: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE objects SET projected_fts_body = ?,
			        fts_indexed = CASE WHEN ? != '' THEN 1 ELSE fts_indexed END
			  WHERE id = ?`, body, body, id); err != nil {
			return false, fmt.Errorf("reproject %s: write body: %w", id, err)
		}
		if body != "" {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO objects_fts(rowid, id, projected_fts_body) VALUES ((SELECT rowid FROM objects WHERE id = ?), ?, ?)`,
				id, id, body); err != nil {
				return false, fmt.Errorf("reproject %s: index body: %w", id, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("reproject %s: commit: %w", id, err)
	}
	return true, nil
}

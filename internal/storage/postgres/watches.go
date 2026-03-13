package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type WatchStore struct{ db *sql.DB }

func (s *WatchStore) CreateWatch(ctx context.Context, w *storage.WatchConfig) error {
	inclJSON, err := json.Marshal(w.IncludePatterns)
	if err != nil {
		return fmt.Errorf("marshal include_patterns: %w", err)
	}
	exclJSON, err := json.Marshal(w.ExcludePatterns)
	if err != nil {
		return fmt.Errorf("marshal exclude_patterns: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO watches
		(id, path, mode, include_patterns, exclude_patterns, debounce_ms, status, pipeline_override, last_error, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		w.ID, w.Path, w.Mode,
		inclJSON, exclJSON,
		w.DebounceMS, w.Status, w.PipelineOverride, w.LastError,
		w.CreatedAt.UTC(), w.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create watch: %w", err)
	}
	return nil
}

func (s *WatchStore) GetWatch(ctx context.Context, id string) (*storage.WatchConfig, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, path, mode, include_patterns, exclude_patterns, debounce_ms, status, pipeline_override, last_error, created_at, updated_at
		FROM watches WHERE id = $1`, id)
	return scanWatch(row)
}

func (s *WatchStore) ListWatches(ctx context.Context, status string) ([]*storage.WatchConfig, error) {
	query := `SELECT id, path, mode, include_patterns, exclude_patterns, debounce_ms, status, pipeline_override, last_error, created_at, updated_at FROM watches`
	var args []any
	if status != "" {
		query += " WHERE status = $1"
		args = append(args, status)
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list watches: %w", err)
	}
	defer rows.Close()

	var watches []*storage.WatchConfig
	for rows.Next() {
		w, err := scanWatchRow(rows)
		if err != nil {
			return nil, err
		}
		watches = append(watches, w)
	}
	return watches, rows.Err()
}

func (s *WatchStore) UpdateWatch(ctx context.Context, w *storage.WatchConfig) error {
	inclJSON, err := json.Marshal(w.IncludePatterns)
	if err != nil {
		return fmt.Errorf("marshal include_patterns: %w", err)
	}
	exclJSON, err := json.Marshal(w.ExcludePatterns)
	if err != nil {
		return fmt.Errorf("marshal exclude_patterns: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE watches SET
		path=$1, mode=$2, include_patterns=$3, exclude_patterns=$4, debounce_ms=$5,
		status=$6, pipeline_override=$7, last_error=$8, updated_at=$9
		WHERE id=$10`,
		w.Path, w.Mode, inclJSON, exclJSON, w.DebounceMS,
		w.Status, w.PipelineOverride, w.LastError, w.UpdatedAt.UTC(),
		w.ID,
	)
	if err != nil {
		return fmt.Errorf("update watch: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("watch %q: not found", w.ID)
	}
	return nil
}

func (s *WatchStore) DeleteWatch(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM watches WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete watch: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("watch %q: not found", id)
	}
	return nil
}

func (s *WatchStore) UpsertFileRecord(ctx context.Context, r *storage.WatchFileRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO watch_file_records
		(watch_id, file_path, object_id, content_hash, last_seen)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (watch_id, file_path) DO UPDATE SET
			object_id = EXCLUDED.object_id,
			content_hash = EXCLUDED.content_hash,
			last_seen = EXCLUDED.last_seen`,
		r.WatchID, r.FilePath, r.ObjectID, r.ContentHash, r.LastSeen.UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert file record: %w", err)
	}
	return nil
}

func (s *WatchStore) GetFileRecord(ctx context.Context, watchID, filePath string) (*storage.WatchFileRecord, error) {
	var r storage.WatchFileRecord
	err := s.db.QueryRowContext(ctx, `SELECT watch_id, file_path, object_id, content_hash, last_seen
		FROM watch_file_records WHERE watch_id = $1 AND file_path = $2`, watchID, filePath).
		Scan(&r.WatchID, &r.FilePath, &r.ObjectID, &r.ContentHash, &r.LastSeen)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("file record %q/%q: not found", watchID, filePath)
		}
		return nil, fmt.Errorf("get file record: %w", err)
	}
	return &r, nil
}

func (s *WatchStore) DeleteFileRecord(ctx context.Context, watchID, filePath string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM watch_file_records WHERE watch_id = $1 AND file_path = $2",
		watchID, filePath)
	return err
}

func (s *WatchStore) ListFileRecords(ctx context.Context, watchID string) ([]*storage.WatchFileRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT watch_id, file_path, object_id, content_hash, last_seen
		FROM watch_file_records WHERE watch_id = $1 ORDER BY file_path ASC`, watchID)
	if err != nil {
		return nil, fmt.Errorf("list file records: %w", err)
	}
	defer rows.Close()

	var records []*storage.WatchFileRecord
	for rows.Next() {
		var r storage.WatchFileRecord
		if err := rows.Scan(&r.WatchID, &r.FilePath, &r.ObjectID, &r.ContentHash, &r.LastSeen); err != nil {
			return nil, fmt.Errorf("scan file record: %w", err)
		}
		records = append(records, &r)
	}
	return records, rows.Err()
}

func scanWatch(row *sql.Row) (*storage.WatchConfig, error) {
	var w storage.WatchConfig
	var inclJSON, exclJSON []byte
	err := row.Scan(
		&w.ID, &w.Path, &w.Mode,
		&inclJSON, &exclJSON,
		&w.DebounceMS, &w.Status, &w.PipelineOverride, &w.LastError,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("watch: not found")
		}
		return nil, fmt.Errorf("scan watch: %w", err)
	}
	json.Unmarshal(inclJSON, &w.IncludePatterns)
	json.Unmarshal(exclJSON, &w.ExcludePatterns)
	return &w, nil
}

func scanWatchRow(rows *sql.Rows) (*storage.WatchConfig, error) {
	var w storage.WatchConfig
	var inclJSON, exclJSON []byte
	err := rows.Scan(
		&w.ID, &w.Path, &w.Mode,
		&inclJSON, &exclJSON,
		&w.DebounceMS, &w.Status, &w.PipelineOverride, &w.LastError,
		&w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan watch row: %w", err)
	}
	json.Unmarshal(inclJSON, &w.IncludePatterns)
	json.Unmarshal(exclJSON, &w.ExcludePatterns)
	return &w, nil
}

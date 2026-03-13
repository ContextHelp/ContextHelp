package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// WatchStore implements storage.WatchStore for SQLite.
type WatchStore struct{ db *sql.DB }

// CreateWatch inserts a new watch configuration.
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Path, w.Mode,
		string(inclJSON), string(exclJSON),
		w.DebounceMS, w.Status, w.PipelineOverride, w.LastError,
		w.CreatedAt.Format(time.RFC3339), w.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create watch: %w", err)
	}
	return nil
}

// GetWatch retrieves a watch by ID; returns error containing "not found" on miss.
func (s *WatchStore) GetWatch(ctx context.Context, id string) (*storage.WatchConfig, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, path, mode, include_patterns, exclude_patterns, debounce_ms, status, pipeline_override, last_error, created_at, updated_at
		FROM watches WHERE id = ?`, id)
	return scanWatch(row)
}

// ListWatches returns all watches, optionally filtered by status (empty = all).
func (s *WatchStore) ListWatches(ctx context.Context, status string) ([]*storage.WatchConfig, error) {
	query := `SELECT id, path, mode, include_patterns, exclude_patterns, debounce_ms, status, pipeline_override, last_error, created_at, updated_at FROM watches`
	var args []any
	if status != "" {
		query += " WHERE status = ?"
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

// UpdateWatch replaces all mutable fields for the given watch ID.
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
		path=?, mode=?, include_patterns=?, exclude_patterns=?, debounce_ms=?,
		status=?, pipeline_override=?, last_error=?, updated_at=?
		WHERE id=?`,
		w.Path, w.Mode, string(inclJSON), string(exclJSON), w.DebounceMS,
		w.Status, w.PipelineOverride, w.LastError, w.UpdatedAt.Format(time.RFC3339),
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

// DeleteWatch removes a watch and cascades to watch_file_records.
func (s *WatchStore) DeleteWatch(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM watches WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete watch: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("watch %q: not found", id)
	}
	return nil
}

// UpsertFileRecord inserts or replaces a file record.
func (s *WatchStore) UpsertFileRecord(ctx context.Context, r *storage.WatchFileRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO watch_file_records
		(watch_id, file_path, object_id, content_hash, last_seen)
		VALUES (?, ?, ?, ?, ?)`,
		r.WatchID, r.FilePath, r.ObjectID, r.ContentHash,
		r.LastSeen.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert file record: %w", err)
	}
	return nil
}

// GetFileRecord retrieves a file record for (watchID, filePath).
func (s *WatchStore) GetFileRecord(ctx context.Context, watchID, filePath string) (*storage.WatchFileRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT watch_id, file_path, object_id, content_hash, last_seen
		FROM watch_file_records WHERE watch_id = ? AND file_path = ?`, watchID, filePath)

	var r storage.WatchFileRecord
	var lastSeen string
	err := row.Scan(&r.WatchID, &r.FilePath, &r.ObjectID, &r.ContentHash, &lastSeen)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("file record %q/%q: not found", watchID, filePath)
		}
		return nil, fmt.Errorf("get file record: %w", err)
	}
	r.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
	return &r, nil
}

// DeleteFileRecord removes a specific file record.
func (s *WatchStore) DeleteFileRecord(ctx context.Context, watchID, filePath string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM watch_file_records WHERE watch_id = ? AND file_path = ?",
		watchID, filePath)
	return err
}

// ListFileRecords returns all file records for a watch.
func (s *WatchStore) ListFileRecords(ctx context.Context, watchID string) ([]*storage.WatchFileRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT watch_id, file_path, object_id, content_hash, last_seen
		FROM watch_file_records WHERE watch_id = ? ORDER BY file_path ASC`, watchID)
	if err != nil {
		return nil, fmt.Errorf("list file records: %w", err)
	}
	defer rows.Close()

	var records []*storage.WatchFileRecord
	for rows.Next() {
		var r storage.WatchFileRecord
		var lastSeen string
		if err := rows.Scan(&r.WatchID, &r.FilePath, &r.ObjectID, &r.ContentHash, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan file record: %w", err)
		}
		r.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
		records = append(records, &r)
	}
	return records, rows.Err()
}

// scanWatch scans a single *sql.Row into a WatchConfig.
func scanWatch(row *sql.Row) (*storage.WatchConfig, error) {
	var w storage.WatchConfig
	var inclJSON, exclJSON, createdAt, updatedAt string
	err := row.Scan(
		&w.ID, &w.Path, &w.Mode,
		&inclJSON, &exclJSON,
		&w.DebounceMS, &w.Status, &w.PipelineOverride, &w.LastError,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("watch: not found")
		}
		return nil, fmt.Errorf("scan watch: %w", err)
	}
	json.Unmarshal([]byte(inclJSON), &w.IncludePatterns)
	json.Unmarshal([]byte(exclJSON), &w.ExcludePatterns)
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &w, nil
}

// scanWatchRow scans a *sql.Rows row into a WatchConfig.
func scanWatchRow(rows *sql.Rows) (*storage.WatchConfig, error) {
	var w storage.WatchConfig
	var inclJSON, exclJSON, createdAt, updatedAt string
	err := rows.Scan(
		&w.ID, &w.Path, &w.Mode,
		&inclJSON, &exclJSON,
		&w.DebounceMS, &w.Status, &w.PipelineOverride, &w.LastError,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan watch row: %w", err)
	}
	json.Unmarshal([]byte(inclJSON), &w.IncludePatterns)
	json.Unmarshal([]byte(exclJSON), &w.ExcludePatterns)
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &w, nil
}

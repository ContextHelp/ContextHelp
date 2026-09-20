package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type DetectorStore struct {
	db *sql.DB
}

func (s *DetectorStore) Create(ctx context.Context, d *storage.DetectorRecord) error {
	var enabled int
	if d.Enabled {
		enabled = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO detectors (
		id, kind, name, pipeline_name, pattern, priority, enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, string(d.Kind), d.Name, d.PipelineName, d.Pattern, d.Priority, enabled,
		d.CreatedAt.Format(time.RFC3339),
		d.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create detector: %w", err)
	}
	return nil
}

func (s *DetectorStore) Get(ctx context.Context, id string) (*storage.DetectorRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, kind, name, pipeline_name, pattern, priority, enabled, created_at, updated_at
	FROM detectors WHERE id = ?`, id)
	return scanDetector(row)
}

func (s *DetectorStore) List(ctx context.Context, filter storage.DetectorFilter) ([]*storage.DetectorRecord, int, error) {
	var conditions []string
	var args []any

	if filter.Kind != "" {
		conditions = append(conditions, "kind = ?")
		args = append(args, string(filter.Kind))
	}
	if filter.Enabled != nil {
		if *filter.Enabled {
			conditions = append(conditions, "enabled = 1")
		} else {
			conditions = append(conditions, "enabled = 0")
		}
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + joinAnd(conditions)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM detectors "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count detectors: %w", err)
	}

	query := `SELECT id, kind, name, pipeline_name, pattern, priority, enabled, created_at, updated_at
	FROM detectors ` + where + " ORDER BY priority ASC, created_at ASC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", filter.Limit, filter.Offset) // #nosec G202 -- integer values
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query detectors: %w", err)
	}
	defer rows.Close()

	var detectors []*storage.DetectorRecord
	for rows.Next() {
		d, err := scanDetector(rows)
		if err != nil {
			return nil, 0, err
		}
		detectors = append(detectors, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate detectors: %w", err)
	}
	return detectors, total, nil
}

func (s *DetectorStore) Update(ctx context.Context, d *storage.DetectorRecord) error {
	var enabled int
	if d.Enabled {
		enabled = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE detectors SET
		kind = ?, name = ?, pipeline_name = ?, pattern = ?, priority = ?, enabled = ?, updated_at = ?
	WHERE id = ?`,
		string(d.Kind), d.Name, d.PipelineName, d.Pattern, d.Priority, enabled,
		d.UpdatedAt.Format(time.RFC3339), d.ID,
	)
	if err != nil {
		return fmt.Errorf("update detector: %w", err)
	}
	return nil
}

func (s *DetectorStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM detectors WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete detector: %w", err)
	}
	return nil
}

func (s *DetectorStore) Enable(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE detectors SET enabled = 1, updated_at = ? WHERE id = ?",
		time.Now().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("enable detector: %w", err)
	}
	return nil
}

func (s *DetectorStore) Disable(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE detectors SET enabled = 0, updated_at = ? WHERE id = ?",
		time.Now().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("disable detector: %w", err)
	}
	return nil
}

func scanDetector(row interface{ Scan(...any) error }) (*storage.DetectorRecord, error) {
	var d storage.DetectorRecord
	var kind string
	var enabled int
	var createdAt, updatedAt string

	err := row.Scan(
		&d.ID, &kind, &d.Name, &d.PipelineName, &d.Pattern, &d.Priority, &enabled,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan detector: %w", err)
	}

	d.Kind = storage.DetectorKind(kind)
	d.Enabled = enabled == 1

	d.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	d.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return &d, nil
}

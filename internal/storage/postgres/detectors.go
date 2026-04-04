package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type DetectorStore struct{ db *sql.DB }

func (s *DetectorStore) Create(ctx context.Context, d *storage.DetectorRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO detectors (
		id, kind, name, pipeline_name, pattern, priority, enabled, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		d.ID, string(d.Kind), d.Name, d.PipelineName, d.Pattern, d.Priority, d.Enabled,
		d.CreatedAt.UTC(), d.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create detector: %w", err)
	}
	return nil
}

func (s *DetectorStore) Get(ctx context.Context, id string) (*storage.DetectorRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, kind, name, pipeline_name, pattern, priority, enabled, created_at, updated_at
	FROM detectors WHERE id = $1`, id)
	return scanDetector(row)
}

func (s *DetectorStore) List(ctx context.Context, filter storage.DetectorFilter) ([]*storage.DetectorRecord, int, error) {
	var conditions []string
	var args []any
	idx := 1

	if filter.Kind != "" {
		conditions = append(conditions, fmt.Sprintf("kind = $%d", idx))
		args = append(args, string(filter.Kind))
		idx++
	}
	if filter.Enabled != nil {
		if *filter.Enabled {
			conditions = append(conditions, "enabled = TRUE")
		} else {
			conditions = append(conditions, "enabled = FALSE")
		}
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
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
	return detectors, total, rows.Err()
}

func (s *DetectorStore) Update(ctx context.Context, d *storage.DetectorRecord) error {
	_, err := s.db.ExecContext(ctx, `UPDATE detectors SET
		kind = $1, name = $2, pipeline_name = $3, pattern = $4, priority = $5, enabled = $6, updated_at = $7
	WHERE id = $8`,
		string(d.Kind), d.Name, d.PipelineName, d.Pattern, d.Priority, d.Enabled,
		d.UpdatedAt.UTC(), d.ID,
	)
	if err != nil {
		return fmt.Errorf("update detector: %w", err)
	}
	return nil
}

func (s *DetectorStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM detectors WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete detector: %w", err)
	}
	return nil
}

func (s *DetectorStore) Enable(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE detectors SET enabled = TRUE, updated_at = $1 WHERE id = $2",
		time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("enable detector: %w", err)
	}
	return nil
}

func (s *DetectorStore) Disable(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE detectors SET enabled = FALSE, updated_at = $1 WHERE id = $2",
		time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("disable detector: %w", err)
	}
	return nil
}

func scanDetector(row interface{ Scan(...any) error }) (*storage.DetectorRecord, error) {
	var d storage.DetectorRecord
	var kind string

	err := row.Scan(
		&d.ID, &kind, &d.Name, &d.PipelineName, &d.Pattern, &d.Priority, &d.Enabled,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("detector not found")
		}
		return nil, fmt.Errorf("scan detector: %w", err)
	}
	d.Kind = storage.DetectorKind(kind)
	return &d, nil
}

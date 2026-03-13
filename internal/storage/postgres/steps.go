package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func (s *StepStore) Create(ctx context.Context, step *storage.RegisteredStep) error {
	metadataJSON := []byte("{}")
	if step.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(step.Metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO steps (
		name, source, path, metadata, installed_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6)`,
		step.Name, step.Source, step.Path, metadataJSON,
		step.InstalledAt.UTC(), step.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create step: %w", err)
	}
	return nil
}

func (s *StepStore) Get(ctx context.Context, name string) (*storage.RegisteredStep, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		name, source, path, metadata, installed_at, updated_at
	FROM steps WHERE name = $1`, name)
	return scanStep(row)
}

func (s *StepStore) List(ctx context.Context, source string) ([]*storage.RegisteredStep, int, error) {
	var conditions []string
	var args []any
	idx := 1

	if source != "" {
		conditions = append(conditions, fmt.Sprintf("source = $%d", idx))
		args = append(args, source)
		idx++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM steps "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count steps: %w", err)
	}

	query := `SELECT name, source, path, metadata, installed_at, updated_at FROM steps ` + where + " ORDER BY installed_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query steps: %w", err)
	}
	defer rows.Close()

	var steps []*storage.RegisteredStep
	for rows.Next() {
		step, err := scanStep(rows)
		if err != nil {
			return nil, 0, err
		}
		steps = append(steps, step)
	}
	return steps, total, nil
}

func (s *StepStore) Unregister(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM steps WHERE name = $1", name)
	if err != nil {
		return fmt.Errorf("unregister step: %w", err)
	}
	return nil
}

func (s *StepStore) Update(ctx context.Context, step *storage.RegisteredStep) error {
	metadataJSON := []byte("{}")
	if step.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(step.Metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE steps SET
		source = $1, path = $2, metadata = $3, updated_at = $4
	WHERE name = $5`,
		step.Source, step.Path, metadataJSON, step.UpdatedAt.UTC(), step.Name,
	)
	if err != nil {
		return fmt.Errorf("update step: %w", err)
	}
	return nil
}

func scanStep(row interface{ Scan(...any) error }) (*storage.RegisteredStep, error) {
	var s storage.RegisteredStep
	var metadataJSON []byte

	err := row.Scan(
		&s.Name, &s.Source, &s.Path, &metadataJSON,
		&s.InstalledAt, &s.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("step not found")
		}
		return nil, fmt.Errorf("scan step: %w", err)
	}
	if len(metadataJSON) > 0 && string(metadataJSON) != "{}" {
		if err := json.Unmarshal(metadataJSON, &s.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}
	return &s, nil
}

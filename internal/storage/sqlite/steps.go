package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type StepStore struct {
	db *sql.DB
}

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
	) VALUES (?, ?, ?, ?, ?, ?)`,
		step.Name, step.Source, step.Path, metadataJSON,
		step.InstalledAt.Format(time.RFC3339),
		step.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create step: %w", err)
	}
	return nil
}

func (s *StepStore) Get(ctx context.Context, name string) (*storage.RegisteredStep, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		name, source, path, metadata, installed_at, updated_at
	FROM steps WHERE name = ?`, name)
	return scanStep(row)
}

func (s *StepStore) List(ctx context.Context, source string) ([]*storage.RegisteredStep, int, error) {
	var conditions []string
	var args []any

	if source != "" {
		conditions = append(conditions, "source = ?")
		args = append(args, source)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + joinAnd(conditions)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM steps "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count steps: %w", err)
	}

	query := `SELECT
		name, source, path, metadata, installed_at, updated_at
	FROM steps ` + where + " ORDER BY installed_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query steps: %w", err)
	}
	defer rows.Close()

	var steps []*storage.RegisteredStep
	for rows.Next() {
		s, err := scanStep(rows)
		if err != nil {
			return nil, 0, err
		}
		steps = append(steps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate steps: %w", err)
	}

	return steps, total, nil
}

func (s *StepStore) Unregister(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM steps WHERE name = ?", name)
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
		source = ?, path = ?, metadata = ?, updated_at = ?
	WHERE name = ?`,
		step.Source, step.Path, metadataJSON,
		step.UpdatedAt.Format(time.RFC3339),
		step.Name,
	)
	if err != nil {
		return fmt.Errorf("update step: %w", err)
	}
	return nil
}

func scanStep(row interface{ Scan(...any) error }) (*storage.RegisteredStep, error) {
	var s storage.RegisteredStep
	var metadataJSON string
	var installedAt, updatedAt string

	err := row.Scan(
		&s.Name, &s.Source, &s.Path, &metadataJSON,
		&installedAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan step: %w", err)
	}

	if metadataJSON != "{}" && metadataJSON != "" {
		if err := json.Unmarshal([]byte(metadataJSON), &s.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}

	s.InstalledAt, err = time.Parse(time.RFC3339, installedAt)
	if err != nil {
		return nil, fmt.Errorf("parse installed_at: %w", err)
	}

	s.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	return &s, nil
}

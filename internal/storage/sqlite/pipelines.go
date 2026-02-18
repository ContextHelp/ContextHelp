package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type PipelineStore struct {
	db *sql.DB
}

func (s *PipelineStore) Create(ctx context.Context, pipeline *storage.Pipeline) error {
	stepsJSON, err := json.Marshal(pipeline.Steps)
	if err != nil {
		return fmt.Errorf("marshal steps: %w", err)
	}

	sandboxJSON := []byte("{}")
	if pipeline.Sandbox != nil {
		sandboxJSON, err = json.Marshal(pipeline.Sandbox)
		if err != nil {
			return fmt.Errorf("marshal sandbox: %w", err)
		}
	}

	var isBuiltIn, archived int
	if pipeline.IsBuiltIn {
		isBuiltIn = 1
	}
	if pipeline.Archived {
		archived = 1
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO pipelines (
		id, name, description, steps, is_built_in, archived, sandbox,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pipeline.ID, pipeline.Name, pipeline.Description, stepsJSON,
		isBuiltIn, archived, sandboxJSON,
		pipeline.CreatedAt.Format(time.RFC3339),
		pipeline.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Get(ctx context.Context, name string) (*storage.Pipeline, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, name, description, steps, is_built_in, archived, sandbox,
		created_at, updated_at
	FROM pipelines WHERE name = ?`, name)
	return scanPipeline(row)
}

func (s *PipelineStore) List(ctx context.Context, filter storage.PipelineFilter) ([]*storage.Pipeline, int, error) {
	var conditions []string
	var args []any

	if filter.Name != "" {
		conditions = append(conditions, "name = ?")
		args = append(args, filter.Name)
	}

	if filter.OnlyArchived {
		conditions = append(conditions, "archived = 1")
	} else if !filter.IncludeArchived {
		conditions = append(conditions, "archived = 0")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + joinAnd(conditions)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pipelines "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count pipelines: %w", err)
	}

	query := `SELECT
		id, name, description, steps, is_built_in, archived, sandbox,
		created_at, updated_at
	FROM pipelines ` + where + " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query pipelines: %w", err)
	}
	defer rows.Close()

	var pipelines []*storage.Pipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, 0, err
		}
		pipelines = append(pipelines, p)
	}

	return pipelines, total, nil
}

func (s *PipelineStore) Update(ctx context.Context, pipeline *storage.Pipeline) error {
	stepsJSON, err := json.Marshal(pipeline.Steps)
	if err != nil {
		return fmt.Errorf("marshal steps: %w", err)
	}

	sandboxJSON := []byte("{}")
	if pipeline.Sandbox != nil {
		sandboxJSON, err = json.Marshal(pipeline.Sandbox)
		if err != nil {
			return fmt.Errorf("marshal sandbox: %w", err)
		}
	}

	var isBuiltIn, archived int
	if pipeline.IsBuiltIn {
		isBuiltIn = 1
	}
	if pipeline.Archived {
		archived = 1
	}

	_, err = s.db.ExecContext(ctx, `UPDATE pipelines SET
		description = ?, steps = ?, is_built_in = ?, archived = ?, sandbox = ?,
		updated_at = ?
	WHERE name = ?`,
		pipeline.Description, stepsJSON, isBuiltIn, archived, sandboxJSON,
		pipeline.UpdatedAt.Format(time.RFC3339),
		pipeline.Name,
	)
	if err != nil {
		return fmt.Errorf("update pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM pipelines WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("delete pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Archive(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE pipelines SET archived = 1, updated_at = ? WHERE name = ?",
		time.Now().Format(time.RFC3339), name)
	if err != nil {
		return fmt.Errorf("archive pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Unarchive(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE pipelines SET archived = 0, updated_at = ? WHERE name = ?",
		time.Now().Format(time.RFC3339), name)
	if err != nil {
		return fmt.Errorf("unarchive pipeline: %w", err)
	}
	return nil
}

func scanPipeline(row interface{ Scan(...any) error }) (*storage.Pipeline, error) {
	var p storage.Pipeline
	var isBuiltIn, archived int
	var stepsJSON, sandboxJSON string
	var createdAt, updatedAt string

	err := row.Scan(
		&p.ID, &p.Name, &p.Description, &stepsJSON,
		&isBuiltIn, &archived, &sandboxJSON,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan pipeline: %w", err)
	}

	p.IsBuiltIn = isBuiltIn == 1
	p.Archived = archived == 1

	if err := json.Unmarshal([]byte(stepsJSON), &p.Steps); err != nil {
		return nil, fmt.Errorf("unmarshal steps: %w", err)
	}

	if sandboxJSON != "{}" && sandboxJSON != "" {
		if err := json.Unmarshal([]byte(sandboxJSON), &p.Sandbox); err != nil {
			return nil, fmt.Errorf("unmarshal sandbox: %w", err)
		}
	}

	p.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}

	p.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	return &p, nil
}

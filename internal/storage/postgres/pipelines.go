package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

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
	_, err = s.db.ExecContext(ctx, `INSERT INTO pipelines (
		id, name, description, steps, is_built_in, archived, sandbox, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		pipeline.ID, pipeline.Name, pipeline.Description, stepsJSON,
		pipeline.IsBuiltIn, pipeline.Archived, sandboxJSON,
		pipeline.CreatedAt.UTC(), pipeline.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Get(ctx context.Context, name string) (*storage.Pipeline, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, name, description, steps, is_built_in, archived, sandbox, created_at, updated_at
	FROM pipelines WHERE name = $1`, name)
	return scanPipeline(row)
}

func (s *PipelineStore) List(ctx context.Context, filter storage.PipelineFilter) ([]*storage.Pipeline, int, error) {
	var conditions []string
	var args []any
	idx := 1

	if filter.Name != "" {
		conditions = append(conditions, fmt.Sprintf("name = $%d", idx))
		args = append(args, filter.Name)
	}
	if filter.OnlyArchived {
		conditions = append(conditions, "archived = TRUE")
	} else if !filter.IncludeArchived {
		conditions = append(conditions, "archived = FALSE")
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pipelines "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count pipelines: %w", err)
	}

	query := `SELECT
		id, name, description, steps, is_built_in, archived, sandbox, created_at, updated_at
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
	return pipelines, total, rows.Err()
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
	_, err = s.db.ExecContext(ctx, `UPDATE pipelines SET
		description = $1, steps = $2, is_built_in = $3, archived = $4, sandbox = $5, updated_at = $6
	WHERE name = $7`,
		pipeline.Description, stepsJSON, pipeline.IsBuiltIn, pipeline.Archived, sandboxJSON,
		pipeline.UpdatedAt.UTC(), pipeline.Name,
	)
	if err != nil {
		return fmt.Errorf("update pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM pipelines WHERE name = $1", name)
	if err != nil {
		return fmt.Errorf("delete pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Archive(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE pipelines SET archived = TRUE, updated_at = $1 WHERE name = $2",
		time.Now().UTC(), name)
	if err != nil {
		return fmt.Errorf("archive pipeline: %w", err)
	}
	return nil
}

func (s *PipelineStore) Unarchive(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE pipelines SET archived = FALSE, updated_at = $1 WHERE name = $2",
		time.Now().UTC(), name)
	if err != nil {
		return fmt.Errorf("unarchive pipeline: %w", err)
	}
	return nil
}

func scanPipeline(row interface{ Scan(...any) error }) (*storage.Pipeline, error) {
	var p storage.Pipeline
	var stepsJSON, sandboxJSON []byte

	err := row.Scan(
		&p.ID, &p.Name, &p.Description, &stepsJSON,
		&p.IsBuiltIn, &p.Archived, &sandboxJSON,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("pipeline not found")
		}
		return nil, fmt.Errorf("scan pipeline: %w", err)
	}
	if err := json.Unmarshal(stepsJSON, &p.Steps); err != nil {
		return nil, fmt.Errorf("unmarshal steps: %w", err)
	}
	if len(sandboxJSON) > 0 && string(sandboxJSON) != "{}" {
		if err := json.Unmarshal(sandboxJSON, &p.Sandbox); err != nil {
			return nil, fmt.Errorf("unmarshal sandbox: %w", err)
		}
	}
	return &p, nil
}

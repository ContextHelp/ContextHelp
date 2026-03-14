package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func (s *EntityStore) Upsert(ctx context.Context, entity *storage.Entity) error {
	aliases, _ := json.Marshal(entity.Aliases)
	metadata, _ := json.Marshal(entity.Metadata)

	_, err := s.db.ExecContext(ctx, `INSERT INTO entities (
		slug, title, description, namespace, aliases, metadata, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (slug) DO UPDATE SET
		title = EXCLUDED.title,
		description = EXCLUDED.description,
		namespace = EXCLUDED.namespace,
		aliases = EXCLUDED.aliases,
		metadata = EXCLUDED.metadata,
		updated_at = EXCLUDED.updated_at`,
		entity.Slug, entity.Title, entity.Description, entity.Namespace,
		aliases, metadata,
		entity.CreatedAt.UTC(), entity.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert entity: %w", err)
	}
	return nil
}

func (s *EntityStore) Get(ctx context.Context, slug string) (*storage.Entity, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		slug, title, description, namespace, aliases, metadata, created_at, updated_at
	FROM entities WHERE slug = $1`, slug)
	return scanEntity(row)
}

func (s *EntityStore) List(ctx context.Context, filter storage.EntityFilter) ([]*storage.Entity, error) {
	query := `SELECT slug, title, description, namespace, aliases, metadata, created_at, updated_at FROM entities`
	var args []any

	if filter.Namespace != "" {
		query += " WHERE namespace = $1"
		args = append(args, filter.Namespace)
	}
	query += " ORDER BY slug ASC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	defer rows.Close()

	var entities []*storage.Entity
	for rows.Next() {
		e, err := scanEntityRow(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}
	return entities, rows.Err()
}

func (s *EntityStore) Resolve(ctx context.Context, mention string) (*storage.Entity, error) {
	// Try exact slug match first.
	row := s.db.QueryRowContext(ctx, `SELECT
		slug, title, description, namespace, aliases, metadata, created_at, updated_at
	FROM entities WHERE slug = $1`, mention)
	entity, err := scanEntity(row)
	if err == nil {
		return entity, nil
	}

	// Try alias match using JSONB containment.
	row = s.db.QueryRowContext(ctx, `SELECT
		slug, title, description, namespace, aliases, metadata, created_at, updated_at
	FROM entities WHERE aliases @> $1::jsonb`, fmt.Sprintf(`[%q]`, mention))
	return scanEntity(row)
}

func scanEntity(row *sql.Row) (*storage.Entity, error) {
	var e storage.Entity
	var aliasesJSON, metadataJSON []byte

	err := row.Scan(&e.Slug, &e.Title, &e.Description, &e.Namespace,
		&aliasesJSON, &metadataJSON, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("entity not found")
		}
		return nil, fmt.Errorf("scan entity: %w", err)
	}
	json.Unmarshal(aliasesJSON, &e.Aliases)
	json.Unmarshal(metadataJSON, &e.Metadata)
	return &e, nil
}

func scanEntityRow(rows *sql.Rows) (*storage.Entity, error) {
	var e storage.Entity
	var aliasesJSON, metadataJSON []byte

	err := rows.Scan(&e.Slug, &e.Title, &e.Description, &e.Namespace,
		&aliasesJSON, &metadataJSON, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan entity row: %w", err)
	}
	json.Unmarshal(aliasesJSON, &e.Aliases)
	json.Unmarshal(metadataJSON, &e.Metadata)
	return &e, nil
}

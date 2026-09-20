package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func (s *EntityStore) Upsert(ctx context.Context, entity *storage.Entity) error {
	if entity.ContentStatus == "" {
		entity.ContentStatus = storage.ContentStatusFull
	}

	aliases, _ := json.Marshal(entity.Aliases)
	metadata, _ := json.Marshal(entity.Metadata)

	_, err := s.db.ExecContext(ctx, `INSERT INTO entities (
		slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	ON CONFLICT (slug) DO UPDATE SET
		title          = EXCLUDED.title,
		description    = EXCLUDED.description,
		namespace      = EXCLUDED.namespace,
		aliases        = EXCLUDED.aliases,
		metadata       = EXCLUDED.metadata,
		content_status = EXCLUDED.content_status,
		version_hash   = EXCLUDED.version_hash,
		registry_url   = EXCLUDED.registry_url,
		updated_at     = EXCLUDED.updated_at`,
		entity.Slug, entity.Title, entity.Description, entity.Namespace,
		aliases, metadata,
		string(entity.ContentStatus), entity.VersionHash, entity.RegistryURL,
		entity.CreatedAt.UTC(), entity.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert entity: %w", err)
	}
	return nil
}

// UpsertThin stores an index-only stub. Does NOT overwrite a 'full' record.
func (s *EntityStore) UpsertThin(ctx context.Context, entity *storage.Entity) error {
	aliases, _ := json.Marshal(entity.Aliases)

	_, err := s.db.ExecContext(ctx, `INSERT INTO entities (
		slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at
	) VALUES ($1, $2, '', $3, $4, '[]',
		'thin', $5, $6, $7, $8)
	ON CONFLICT (slug) DO UPDATE SET
		title        = EXCLUDED.title,
		namespace    = EXCLUDED.namespace,
		aliases      = EXCLUDED.aliases,
		version_hash = EXCLUDED.version_hash,
		registry_url = EXCLUDED.registry_url,
		updated_at   = EXCLUDED.updated_at
	WHERE entities.content_status != 'full'`,
		entity.Slug, entity.Title, entity.Namespace,
		aliases,
		entity.VersionHash, entity.RegistryURL,
		entity.CreatedAt.UTC(), entity.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("upsert thin entity: %w", err)
	}
	return nil
}

// SetContentStatus updates a single entity's content_status column.
func (s *EntityStore) SetContentStatus(ctx context.Context, slug string, status storage.ContentStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE entities SET content_status = $1, updated_at = $2 WHERE slug = $3`,
		string(status), time.Now().UTC(), slug,
	)
	if err != nil {
		return fmt.Errorf("set content status: %w", err)
	}
	return nil
}

func (s *EntityStore) Get(ctx context.Context, slug string) (*storage.Entity, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at
	FROM entities WHERE slug = $1`, slug)
	return scanEntity(row)
}

func (s *EntityStore) List(ctx context.Context, filter storage.EntityFilter) ([]*storage.Entity, error) {
	query := `SELECT slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at FROM entities WHERE 1=1`
	var args []any
	argIdx := 1

	if filter.Namespace != "" {
		query += fmt.Sprintf(" AND namespace = $%d", argIdx)
		args = append(args, filter.Namespace)
		argIdx++
	}
	if filter.ContentStatus != "" {
		query += fmt.Sprintf(" AND content_status = $%d", argIdx)
		args = append(args, string(filter.ContentStatus))
	}

	query += " ORDER BY slug ASC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset) // #nosec G202 -- integer value
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
		slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at
	FROM entities WHERE slug = $1`, mention)
	entity, err := scanEntity(row)
	if err == nil {
		return entity, nil
	}

	// Try alias match using JSONB containment.
	row = s.db.QueryRowContext(ctx, `SELECT
		slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at
	FROM entities WHERE aliases @> $1::jsonb`, fmt.Sprintf(`[%q]`, mention))
	return scanEntity(row)
}

func scanEntity(row *sql.Row) (*storage.Entity, error) {
	var e storage.Entity
	var aliasesJSON, metadataJSON []byte
	var contentStatus string

	err := row.Scan(
		&e.Slug, &e.Title, &e.Description, &e.Namespace,
		&aliasesJSON, &metadataJSON,
		&contentStatus, &e.VersionHash, &e.RegistryURL,
		&e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("entity %w", storage.ErrNotFound)
		}
		return nil, fmt.Errorf("scan entity: %w", err)
	}
	json.Unmarshal(aliasesJSON, &e.Aliases)
	json.Unmarshal(metadataJSON, &e.Metadata)
	e.ContentStatus = storage.ContentStatus(contentStatus)
	return &e, nil
}

func scanEntityRow(rows *sql.Rows) (*storage.Entity, error) {
	var e storage.Entity
	var aliasesJSON, metadataJSON []byte
	var contentStatus string

	err := rows.Scan(
		&e.Slug, &e.Title, &e.Description, &e.Namespace,
		&aliasesJSON, &metadataJSON,
		&contentStatus, &e.VersionHash, &e.RegistryURL,
		&e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan entity row: %w", err)
	}
	json.Unmarshal(aliasesJSON, &e.Aliases)
	json.Unmarshal(metadataJSON, &e.Metadata)
	e.ContentStatus = storage.ContentStatus(contentStatus)
	return &e, nil
}

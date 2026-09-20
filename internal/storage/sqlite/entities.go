package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type EntityStore struct {
	db *sql.DB
}

// Upsert inserts or fully replaces an entity. Sets content_status = 'full' unless
// the caller has already populated it.
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
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(slug) DO UPDATE SET
		title          = excluded.title,
		description    = excluded.description,
		namespace      = excluded.namespace,
		aliases        = excluded.aliases,
		metadata       = excluded.metadata,
		content_status = excluded.content_status,
		version_hash   = excluded.version_hash,
		registry_url   = excluded.registry_url,
		updated_at     = excluded.updated_at`,
		entity.Slug, entity.Title, entity.Description, entity.Namespace,
		string(aliases), string(metadata),
		string(entity.ContentStatus), entity.VersionHash, entity.RegistryURL,
		entity.CreatedAt.Format(time.RFC3339), entity.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert entity: %w", err)
	}
	return nil
}

// UpsertThin stores an index-only stub. Does NOT overwrite a 'full' record so that
// a prior on-demand pull is not clobbered by a subsequent thin sync.
func (s *EntityStore) UpsertThin(ctx context.Context, entity *storage.Entity) error {
	aliases, _ := json.Marshal(entity.Aliases)

	_, err := s.db.ExecContext(ctx, `INSERT INTO entities (
		slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at
	) VALUES (?, ?, '', ?, ?, '{}', 'thin', ?, ?, ?, ?)
	ON CONFLICT(slug) DO UPDATE SET
		title        = excluded.title,
		namespace    = excluded.namespace,
		aliases      = excluded.aliases,
		version_hash = excluded.version_hash,
		registry_url = excluded.registry_url,
		updated_at   = excluded.updated_at
	WHERE content_status != 'full'`,
		entity.Slug, entity.Title, entity.Namespace,
		string(aliases),
		entity.VersionHash, entity.RegistryURL,
		entity.CreatedAt.Format(time.RFC3339), entity.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert thin entity: %w", err)
	}
	return nil
}

// SetContentStatus updates a single entity's content_status column.
func (s *EntityStore) SetContentStatus(ctx context.Context, slug string, status storage.ContentStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE entities SET content_status = ?, updated_at = ? WHERE slug = ?`,
		string(status), time.Now().Format(time.RFC3339), slug,
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
	FROM entities WHERE slug = ?`, slug)
	return scanEntity(row)
}

func (s *EntityStore) List(ctx context.Context, filter storage.EntityFilter) ([]*storage.Entity, error) {
	query := `SELECT slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at FROM entities WHERE 1=1`
	var args []any

	if filter.Namespace != "" {
		query += " AND namespace = ?"
		args = append(args, filter.Namespace)
	}
	if filter.ContentStatus != "" {
		query += " AND content_status = ?"
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
		e, err := scanEntityFromRows(rows)
		if err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}
	return entities, rows.Err()
}

func (s *EntityStore) Resolve(ctx context.Context, mention string) (*storage.Entity, error) {
	// First try exact slug match.
	row := s.db.QueryRowContext(ctx, `SELECT
		slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at
	FROM entities WHERE slug = ?`, mention)
	entity, err := scanEntity(row)
	if err == nil {
		return entity, nil
	}

	// Try alias match.
	row = s.db.QueryRowContext(ctx, `SELECT
		slug, title, description, namespace, aliases, metadata,
		content_status, version_hash, registry_url,
		created_at, updated_at
	FROM entities WHERE EXISTS (
		SELECT 1 FROM json_each(aliases) WHERE json_each.value = ?
	)`, mention)
	return scanEntity(row)
}

func scanEntity(row *sql.Row) (*storage.Entity, error) {
	var e storage.Entity
	var aliasesJSON, metadataJSON, createdAt, updatedAt string
	var contentStatus string

	err := row.Scan(
		&e.Slug, &e.Title, &e.Description, &e.Namespace,
		&aliasesJSON, &metadataJSON,
		&contentStatus, &e.VersionHash, &e.RegistryURL,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("entity %w", storage.ErrNotFound)
		}
		return nil, fmt.Errorf("scan entity: %w", err)
	}

	json.Unmarshal([]byte(aliasesJSON), &e.Aliases)
	json.Unmarshal([]byte(metadataJSON), &e.Metadata)
	e.ContentStatus = storage.ContentStatus(contentStatus)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &e, nil
}

func scanEntityFromRows(rows *sql.Rows) (*storage.Entity, error) {
	var e storage.Entity
	var aliasesJSON, metadataJSON, createdAt, updatedAt string
	var contentStatus string

	err := rows.Scan(
		&e.Slug, &e.Title, &e.Description, &e.Namespace,
		&aliasesJSON, &metadataJSON,
		&contentStatus, &e.VersionHash, &e.RegistryURL,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan entity row: %w", err)
	}

	json.Unmarshal([]byte(aliasesJSON), &e.Aliases)
	json.Unmarshal([]byte(metadataJSON), &e.Metadata)
	e.ContentStatus = storage.ContentStatus(contentStatus)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &e, nil
}

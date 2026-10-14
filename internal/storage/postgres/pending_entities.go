package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// pendingEntityStore implements storage.PendingEntityStore over
// registry_pending_entities (Postgres migration 24).
type pendingEntityStore struct{ db *sql.DB }

const pendingEntityCols = `id, registry_name, registry_url, slug, namespace, title,
	version, payload, signature_status, first_seen_at, last_seen_at`

func (s *pendingEntityStore) Upsert(ctx context.Context, p *storage.PendingEntity) error {
	payload, err := json.Marshal(p.Entity)
	if err != nil {
		return fmt.Errorf("marshal pending entity %s: %w", p.Slug, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("upsert pending entity: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM registry_pending_entities
		WHERE registry_name = $1 AND slug = $2 AND version != $3`,
		p.RegistryName, p.Slug, p.Version); err != nil {
		return fmt.Errorf("drop superseded pending entity %s: %w", p.Slug, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO registry_pending_entities (`+pendingEntityCols+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			registry_url     = EXCLUDED.registry_url,
			namespace        = EXCLUDED.namespace,
			title            = EXCLUDED.title,
			payload          = EXCLUDED.payload,
			signature_status = EXCLUDED.signature_status,
			last_seen_at     = EXCLUDED.last_seen_at`,
		p.ID, p.RegistryName, p.RegistryURL, p.Slug, p.Namespace, p.Title,
		p.Version, string(payload), p.SignatureStatus,
		p.FirstSeenAt.UTC(), p.LastSeenAt.UTC(),
	); err != nil {
		return fmt.Errorf("upsert pending entity %s: %w", p.Slug, err)
	}
	return tx.Commit()
}

func (s *pendingEntityStore) List(ctx context.Context, f storage.PendingEntityFilter) ([]*storage.PendingEntity, error) {
	q := `SELECT ` + pendingEntityCols + ` FROM registry_pending_entities`
	var args []any
	if f.RegistryName != "" {
		q += ` WHERE registry_name = $1`
		args = append(args, f.RegistryName)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY registry_name COLLATE "C", slug COLLATE "C", version COLLATE "C"`, args...)
	if err != nil {
		return nil, fmt.Errorf("list pending entities: %w", err)
	}
	defer rows.Close()
	out := []*storage.PendingEntity{}
	for rows.Next() {
		p, err := scanPendingEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *pendingEntityStore) Get(ctx context.Context, id string) (*storage.PendingEntity, error) {
	p, err := scanPendingEntity(s.db.QueryRowContext(ctx,
		`SELECT `+pendingEntityCols+` FROM registry_pending_entities WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("pending entity %q: %w", id, storage.ErrNotFound)
	}
	return p, err
}

func (s *pendingEntityStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM registry_pending_entities WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete pending entity %q: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("pending entity %q: %w", id, storage.ErrNotFound)
	}
	return nil
}

func (s *pendingEntityStore) DeleteEntity(ctx context.Context, registryName, slug string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM registry_pending_entities WHERE registry_name = $1 AND slug = $2`, registryName, slug)
	if err != nil {
		return 0, fmt.Errorf("delete pending entity %s/%s: %w", registryName, slug, err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func scanPendingEntity(row interface{ Scan(...any) error }) (*storage.PendingEntity, error) {
	var p storage.PendingEntity
	var payload []byte
	if err := row.Scan(&p.ID, &p.RegistryName, &p.RegistryURL, &p.Slug, &p.Namespace, &p.Title,
		&p.Version, &payload, &p.SignatureStatus, &p.FirstSeenAt, &p.LastSeenAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan pending entity: %w", err)
	}
	if err := json.Unmarshal(payload, &p.Entity); err != nil {
		return nil, fmt.Errorf("decode pending entity %s payload: %w", p.ID, err)
	}
	return &p, nil
}

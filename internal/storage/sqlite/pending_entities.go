package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// pendingEntityStore implements storage.PendingEntityStore over
// registry_pending_entities (migration 044).
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
		WHERE registry_name = ? AND slug = ? AND version != ?`,
		p.RegistryName, p.Slug, p.Version); err != nil {
		return fmt.Errorf("drop superseded pending entity %s: %w", p.Slug, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO registry_pending_entities (`+pendingEntityCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			registry_url     = excluded.registry_url,
			namespace        = excluded.namespace,
			title            = excluded.title,
			payload          = excluded.payload,
			signature_status = excluded.signature_status,
			last_seen_at     = excluded.last_seen_at`,
		p.ID, p.RegistryName, p.RegistryURL, p.Slug, p.Namespace, p.Title,
		p.Version, string(payload), p.SignatureStatus,
		p.FirstSeenAt.UTC().Format(time.RFC3339), p.LastSeenAt.UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("upsert pending entity %s: %w", p.Slug, err)
	}
	return tx.Commit()
}

func (s *pendingEntityStore) List(ctx context.Context, f storage.PendingEntityFilter) ([]*storage.PendingEntity, error) {
	q := `SELECT ` + pendingEntityCols + ` FROM registry_pending_entities`
	var args []any
	if f.RegistryName != "" {
		q += ` WHERE registry_name = ?`
		args = append(args, f.RegistryName)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY registry_name, slug, version`, args...)
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
		`SELECT `+pendingEntityCols+` FROM registry_pending_entities WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("pending entity %q: %w", id, storage.ErrNotFound)
	}
	return p, err
}

func (s *pendingEntityStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM registry_pending_entities WHERE id = ?`, id)
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
		`DELETE FROM registry_pending_entities WHERE registry_name = ? AND slug = ?`, registryName, slug)
	if err != nil {
		return 0, fmt.Errorf("delete pending entity %s/%s: %w", registryName, slug, err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func scanPendingEntity(row interface{ Scan(...any) error }) (*storage.PendingEntity, error) {
	var p storage.PendingEntity
	var payload, first, last string
	if err := row.Scan(&p.ID, &p.RegistryName, &p.RegistryURL, &p.Slug, &p.Namespace, &p.Title,
		&p.Version, &payload, &p.SignatureStatus, &first, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan pending entity: %w", err)
	}
	if err := json.Unmarshal([]byte(payload), &p.Entity); err != nil {
		return nil, fmt.Errorf("decode pending entity %s payload: %w", p.ID, err)
	}
	var err error
	if p.FirstSeenAt, err = time.Parse(time.RFC3339, first); err != nil {
		return nil, fmt.Errorf("pending entity %s first_seen_at: %w", p.ID, err)
	}
	if p.LastSeenAt, err = time.Parse(time.RFC3339, last); err != nil {
		return nil, fmt.Errorf("pending entity %s last_seen_at: %w", p.ID, err)
	}
	return &p, nil
}

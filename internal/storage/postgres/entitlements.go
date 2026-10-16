package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

// EntitlementStore implements storage.EntitlementStore for Postgres.
type EntitlementStore struct {
	db *sql.DB
}

// Upsert inserts or replaces the row keyed by (e.Kind, e.RegistryName).
func (s *EntitlementStore) Upsert(ctx context.Context, e *storage.RegistryEntitlement) error {
	if !e.Kind.Valid() {
		return fmt.Errorf("upsert entitlement %q: invalid kind %q", e.RegistryName, e.Kind)
	}
	namespaces := e.Namespaces
	if namespaces == nil {
		namespaces = []string{}
	}
	nsJSON, err := json.Marshal(namespaces)
	if err != nil {
		return fmt.Errorf("marshal namespaces: %w", err)
	}

	var expiresAt *time.Time
	if !e.ExpiresAt.IsZero() {
		t := e.ExpiresAt.UTC()
		expiresAt = &t
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO registry_entitlements
		(kind, registry_name, plan, namespaces, expires_at, fetched_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (kind, registry_name) DO UPDATE SET
			plan       = EXCLUDED.plan,
			namespaces = EXCLUDED.namespaces,
			expires_at = EXCLUDED.expires_at,
			fetched_at = EXCLUDED.fetched_at`,
		string(e.Kind), e.RegistryName, e.Plan, string(nsJSON), expiresAt, now,
	)
	if err != nil {
		return fmt.Errorf("upsert entitlement: %w", err)
	}
	return nil
}

// Get returns the row keyed by (kind, name).
func (s *EntitlementStore) Get(ctx context.Context, kind storage.EntitlementKind, name string) (*storage.RegistryEntitlement, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		kind, registry_name, plan, namespaces, expires_at, fetched_at
	FROM registry_entitlements WHERE kind = $1 AND registry_name = $2`, string(kind), name)
	return scanEntitlement(row)
}

// List returns every row of kind, ordered by name.
func (s *EntitlementStore) List(ctx context.Context, kind storage.EntitlementKind) ([]*storage.RegistryEntitlement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		kind, registry_name, plan, namespaces, expires_at, fetched_at
	FROM registry_entitlements WHERE kind = $1 ORDER BY registry_name`, string(kind))
	if err != nil {
		return nil, fmt.Errorf("list entitlements: %w", err)
	}
	defer rows.Close()

	return rowscan.AllScanner(rows, scanEntitlement)
}

// Delete removes the row keyed by (kind, name); storage.ErrNotFound
// (wrapped) when there is none.
func (s *EntitlementStore) Delete(ctx context.Context, kind storage.EntitlementKind, name string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM registry_entitlements WHERE kind = $1 AND registry_name = $2`, string(kind), name)
	if err != nil {
		return fmt.Errorf("delete entitlement: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete entitlement: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("entitlement %s %q: %w", kind, name, storage.ErrNotFound)
	}
	return nil
}

func scanEntitlement(row interface{ Scan(...any) error }) (*storage.RegistryEntitlement, error) {
	var e storage.RegistryEntitlement
	var kind string
	var nsJSON string
	var expiresAt sql.NullTime
	var fetchedAt time.Time

	if err := row.Scan(&kind, &e.RegistryName, &e.Plan, &nsJSON, &expiresAt, &fetchedAt); err != nil {
		return nil, fmt.Errorf("scan entitlement: %w", err)
	}
	e.Kind = storage.EntitlementKind(kind)

	if err := json.Unmarshal([]byte(nsJSON), &e.Namespaces); err != nil {
		return nil, fmt.Errorf("unmarshal namespaces: %w", err)
	}

	if expiresAt.Valid {
		e.ExpiresAt = expiresAt.Time
	}
	e.FetchedAt = fetchedAt

	return &e, nil
}

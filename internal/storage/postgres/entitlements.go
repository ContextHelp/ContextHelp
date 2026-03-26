package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EntitlementStore implements storage.EntitlementStore for Postgres.
type EntitlementStore struct {
	db *sql.DB
}

// Upsert inserts or replaces the entitlement record for the registry.
func (s *EntitlementStore) Upsert(ctx context.Context, e *storage.RegistryEntitlement) error {
	nsJSON, err := json.Marshal(e.Namespaces)
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
		(registry_name, plan, namespaces, expires_at, fetched_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (registry_name) DO UPDATE SET
			plan       = EXCLUDED.plan,
			namespaces = EXCLUDED.namespaces,
			expires_at = EXCLUDED.expires_at,
			fetched_at = EXCLUDED.fetched_at`,
		e.RegistryName, e.Plan, string(nsJSON), expiresAt, now,
	)
	if err != nil {
		return fmt.Errorf("upsert entitlement: %w", err)
	}
	return nil
}

// Get returns the entitlement record for the given registry name.
func (s *EntitlementStore) Get(ctx context.Context, registryName string) (*storage.RegistryEntitlement, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		registry_name, plan, namespaces, expires_at, fetched_at
	FROM registry_entitlements WHERE registry_name = $1`, registryName)
	return scanEntitlement(row)
}

// List returns all entitlement records.
func (s *EntitlementStore) List(ctx context.Context) ([]*storage.RegistryEntitlement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		registry_name, plan, namespaces, expires_at, fetched_at
	FROM registry_entitlements ORDER BY registry_name`)
	if err != nil {
		return nil, fmt.Errorf("list entitlements: %w", err)
	}
	defer rows.Close()

	var out []*storage.RegistryEntitlement
	for rows.Next() {
		e, err := scanEntitlement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanEntitlement(row interface{ Scan(...any) error }) (*storage.RegistryEntitlement, error) {
	var e storage.RegistryEntitlement
	var nsJSON string
	var expiresAt sql.NullTime
	var fetchedAt time.Time

	if err := row.Scan(&e.RegistryName, &e.Plan, &nsJSON, &expiresAt, &fetchedAt); err != nil {
		return nil, fmt.Errorf("scan entitlement: %w", err)
	}

	if err := json.Unmarshal([]byte(nsJSON), &e.Namespaces); err != nil {
		return nil, fmt.Errorf("unmarshal namespaces: %w", err)
	}

	if expiresAt.Valid {
		e.ExpiresAt = expiresAt.Time
	}
	e.FetchedAt = fetchedAt

	return &e, nil
}

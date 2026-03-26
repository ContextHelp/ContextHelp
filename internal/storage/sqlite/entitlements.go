package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// entitlementStore implements storage.EntitlementStore for SQLite.
type entitlementStore struct {
	db *sql.DB
}

// Upsert inserts or replaces the entitlement record for the registry.
func (s *entitlementStore) Upsert(ctx context.Context, e *storage.RegistryEntitlement) error {
	nsJSON, err := json.Marshal(e.Namespaces)
	if err != nil {
		return fmt.Errorf("marshal namespaces: %w", err)
	}

	var expiresAt *string
	if !e.ExpiresAt.IsZero() {
		t := e.ExpiresAt.UTC().Format(time.RFC3339)
		expiresAt = &t
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO registry_entitlements
		(registry_name, plan, namespaces, expires_at, fetched_at)
		VALUES (?, ?, ?, ?, ?)`,
		e.RegistryName, e.Plan, string(nsJSON), expiresAt, now,
	)
	if err != nil {
		return fmt.Errorf("upsert entitlement: %w", err)
	}
	return nil
}

// Get returns the entitlement record for the given registry name.
// Returns sql.ErrNoRows (wrapped) when not found.
func (s *entitlementStore) Get(ctx context.Context, registryName string) (*storage.RegistryEntitlement, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		registry_name, plan, namespaces, expires_at, fetched_at
	FROM registry_entitlements WHERE registry_name = ?`, registryName)
	return scanEntitlement(row)
}

// List returns all entitlement records.
func (s *entitlementStore) List(ctx context.Context) ([]*storage.RegistryEntitlement, error) {
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
	var expiresAt sql.NullString
	var fetchedAt string

	if err := row.Scan(&e.RegistryName, &e.Plan, &nsJSON, &expiresAt, &fetchedAt); err != nil {
		return nil, fmt.Errorf("scan entitlement: %w", err)
	}

	if err := json.Unmarshal([]byte(nsJSON), &e.Namespaces); err != nil {
		return nil, fmt.Errorf("unmarshal namespaces: %w", err)
	}

	if expiresAt.Valid && expiresAt.String != "" {
		t, err := time.Parse(time.RFC3339, expiresAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse expires_at: %w", err)
		}
		e.ExpiresAt = t
	}

	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return nil, fmt.Errorf("parse fetched_at: %w", err)
	}
	e.FetchedAt = t

	return &e, nil
}

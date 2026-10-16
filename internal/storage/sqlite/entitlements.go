package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

// entitlementStore implements storage.EntitlementStore for SQLite.
type entitlementStore struct {
	db *sql.DB
}

// Upsert inserts or replaces the row keyed by (e.Kind, e.RegistryName).
func (s *entitlementStore) Upsert(ctx context.Context, e *storage.RegistryEntitlement) error {
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

	var expiresAt *string
	if !e.ExpiresAt.IsZero() {
		t := e.ExpiresAt.UTC().Format(time.RFC3339)
		expiresAt = &t
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO registry_entitlements
		(kind, registry_name, plan, namespaces, expires_at, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		string(e.Kind), e.RegistryName, e.Plan, string(nsJSON), expiresAt, now,
	)
	if err != nil {
		return fmt.Errorf("upsert entitlement: %w", err)
	}
	return nil
}

// Get returns the row keyed by (kind, name).
// Returns sql.ErrNoRows (wrapped) when not found.
func (s *entitlementStore) Get(ctx context.Context, kind storage.EntitlementKind, name string) (*storage.RegistryEntitlement, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		kind, registry_name, plan, namespaces, expires_at, fetched_at
	FROM registry_entitlements WHERE kind = ? AND registry_name = ?`, string(kind), name)
	return scanEntitlement(row)
}

// List returns every row of kind, ordered by name.
func (s *entitlementStore) List(ctx context.Context, kind storage.EntitlementKind) ([]*storage.RegistryEntitlement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		kind, registry_name, plan, namespaces, expires_at, fetched_at
	FROM registry_entitlements WHERE kind = ? ORDER BY registry_name`, string(kind))
	if err != nil {
		return nil, fmt.Errorf("list entitlements: %w", err)
	}
	defer rows.Close()

	return rowscan.AllScanner(rows, scanEntitlement)
}

// Delete removes the row keyed by (kind, name); storage.ErrNotFound
// (wrapped) when there is none.
func (s *entitlementStore) Delete(ctx context.Context, kind storage.EntitlementKind, name string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM registry_entitlements WHERE kind = ? AND registry_name = ?`, string(kind), name)
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
	var expiresAt sql.NullString
	var fetchedAt string

	if err := row.Scan(&kind, &e.RegistryName, &e.Plan, &nsJSON, &expiresAt, &fetchedAt); err != nil {
		return nil, fmt.Errorf("scan entitlement: %w", err)
	}
	e.Kind = storage.EntitlementKind(kind)

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

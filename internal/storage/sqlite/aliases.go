package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type aliasStore struct{ db *sql.DB }

func (s *aliasStore) Create(ctx context.Context, a *storage.Alias) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO aliases (alias, object_id, scope, profile, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
		a.Alias, a.ObjectID, a.Scope, a.Profile,
		a.CreatedAt.UTC().Format(time.RFC3339),
		a.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("alias create: %w", err)
	}
	return nil
}

// Resolve checks profile-scope first, then global scope.
func (s *aliasStore) Resolve(ctx context.Context, alias, profile string) (string, error) {
	// Profile-scoped lookup first (if profile is non-empty).
	if profile != "" {
		var id string
		err := s.db.QueryRowContext(ctx,
			`SELECT object_id FROM aliases WHERE alias=? AND scope='profile' AND profile=?`,
			alias, profile,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	// Fall through to global lookup.
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT object_id FROM aliases WHERE alias=? AND scope='global'`,
		alias,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("alias %q not found", alias)
	}
	return id, err
}

func (s *aliasStore) List(ctx context.Context, filter storage.AliasFilter) ([]*storage.Alias, error) {
	query := `SELECT alias, object_id, scope, profile, created_at, updated_at FROM aliases WHERE 1=1`
	var args []interface{}
	if filter.ObjectID != "" {
		query += ` AND object_id=?`
		args = append(args, filter.ObjectID)
	}
	if filter.Scope != "" {
		query += ` AND scope=?`
		args = append(args, filter.Scope)
	}
	if filter.Profile != "" {
		query += ` AND profile=?`
		args = append(args, filter.Profile)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*storage.Alias
	for rows.Next() {
		var a storage.Alias
		var createdStr, updatedStr string
		if err := rows.Scan(&a.Alias, &a.ObjectID, &a.Scope, &a.Profile, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (s *aliasStore) Delete(ctx context.Context, alias, scope, profile string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM aliases WHERE alias=? AND scope=? AND profile=?`,
		alias, scope, profile,
	)
	return err
}

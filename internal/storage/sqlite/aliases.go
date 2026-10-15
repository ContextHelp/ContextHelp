package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

type aliasStore struct {
	db *sql.DB
	// scope, when set, keeps every read to the aliases of objects the
	// viewer may see (see scope.go).
	scope *storage.Scope
}

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

// Resolve checks profile-scope first, then global scope. A miss wraps
// storage.ErrNotFound.
func (s *aliasStore) Resolve(ctx context.Context, alias, profile string) (string, error) {
	clause, sargs := andObjectIn("object_id", s.scope)
	// Profile-scoped lookup first (if profile is non-empty).
	if profile != "" {
		var id string
		// #nosec G202 -- clause is fixed SQL over compiled predicates; values are bound.
		err := s.db.QueryRowContext(ctx,
			`SELECT object_id FROM aliases WHERE alias=? AND scope='profile' AND profile=?`+clause,
			append([]any{alias, profile}, sargs...)...,
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
	// #nosec G202 -- clause is fixed SQL over compiled predicates; values are bound.
	err := s.db.QueryRowContext(ctx,
		`SELECT object_id FROM aliases WHERE alias=? AND scope='global'`+clause,
		append([]any{alias}, sargs...)...,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("alias %q: %w", alias, storage.ErrNotFound)
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
	if clause, sargs := andObjectIn("object_id", s.scope); clause != "" {
		// #nosec G202 -- clause is fixed SQL over compiled predicates; values are bound.
		query += clause
		args = append(args, sargs...)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return rowscan.All(rows, func(rows *sql.Rows) (*storage.Alias, error) {
		var a storage.Alias
		var createdStr, updatedStr string
		if err := rows.Scan(&a.Alias, &a.ObjectID, &a.Scope, &a.Profile, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		return &a, nil
	})
}

// Delete removes one alias by its full key. A miss wraps storage.ErrNotFound.
func (s *aliasStore) Delete(ctx context.Context, alias, scope, profile string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM aliases WHERE alias=? AND scope=? AND profile=?`,
		alias, scope, profile,
	)
	if err != nil {
		return fmt.Errorf("alias delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("alias delete: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("alias %q: %w", alias, storage.ErrNotFound)
	}
	return nil
}

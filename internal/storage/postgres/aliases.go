package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type AliasStore struct{ db *sql.DB }

func (s *AliasStore) Create(ctx context.Context, a *storage.Alias) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO aliases (alias, object_id, scope, profile, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6)`,
		a.Alias, a.ObjectID, a.Scope, a.Profile,
		a.CreatedAt.UTC(), a.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("alias create: %w", err)
	}
	return nil
}

func (s *AliasStore) Resolve(ctx context.Context, alias, profile string) (string, error) {
	if profile != "" {
		var id string
		err := s.db.QueryRowContext(ctx,
			`SELECT object_id FROM aliases WHERE alias=$1 AND scope='profile' AND profile=$2`,
			alias, profile,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT object_id FROM aliases WHERE alias=$1 AND scope='global'`,
		alias,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("alias %q not found", alias)
	}
	return id, err
}

func (s *AliasStore) List(ctx context.Context, filter storage.AliasFilter) ([]*storage.Alias, error) {
	query := `SELECT alias, object_id, scope, profile, created_at, updated_at FROM aliases WHERE 1=1`
	var args []any
	idx := 1

	if filter.ObjectID != "" {
		query += fmt.Sprintf(` AND object_id=$%d`, idx)
		args = append(args, filter.ObjectID)
		idx++
	}
	if filter.Scope != "" {
		query += fmt.Sprintf(` AND scope=$%d`, idx)
		args = append(args, filter.Scope)
		idx++
	}
	if filter.Profile != "" {
		query += fmt.Sprintf(` AND profile=$%d`, idx) // #nosec G202 -- idx is a parameterized placeholder index
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
		if err := rows.Scan(&a.Alias, &a.ObjectID, &a.Scope, &a.Profile, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (s *AliasStore) Delete(ctx context.Context, alias, scope, profile string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM aliases WHERE alias=$1 AND scope=$2 AND profile=$3`,
		alias, scope, profile,
	)
	return err
}

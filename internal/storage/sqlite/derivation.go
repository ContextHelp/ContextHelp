package sqlite

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var _ storage.DerivationStore = (*ObjectStore)(nil)

// StampDerivationBaseline implements storage.DerivationStore.
func (s *ObjectStore) StampDerivationBaseline(ctx context.Context, baseline map[string]string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("stamp derivation baseline: begin tx: %w", err)
	}
	defer tx.Rollback()
	// Name order: two daemons stamping one database lock rows alike.
	total := 0
	names := make([]string, 0, len(baseline))
	for name := range baseline {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sig := baseline[name]
		res, err := tx.ExecContext(ctx,
			`UPDATE objects SET derivation_sig = ? WHERE derivation_sig = '' AND pipeline IN (?, ?)`,
			sig, name, name+"@v0")
		if err != nil {
			return 0, fmt.Errorf("stamp derivation baseline %q: %w", name, err)
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("stamp derivation baseline: commit: %w", err)
	}
	return total, nil
}

// staleClause is the WHERE condition for a stale row, with its arguments.
func staleClause(current []string) (string, []any) {
	if len(current) == 0 {
		return `derivation_sig != ''`, nil
	}
	args := make([]any, len(current))
	for i, c := range current {
		args[i] = c
	}
	return `derivation_sig != '' AND derivation_sig NOT IN (?` + strings.Repeat(`, ?`, len(current)-1) + `)`, args
}

// CountStaleDerivations implements storage.DerivationStore.
func (s *ObjectStore) CountStaleDerivations(ctx context.Context, current []string) (int, error) {
	where, args := staleClause(current)
	var n int
	if err := s.db.QueryRowContext(ctx, staleQuery(`SELECT COUNT(*)`, where, ``), args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count stale derivations: %w", err)
	}
	return n, nil
}

// ListStaleDerivations implements storage.DerivationStore.
func (s *ObjectStore) ListStaleDerivations(ctx context.Context, current []string, after string, limit int) ([]string, error) {
	where, args := staleClause(current)
	args = append(args, after, limit)
	rows, err := s.db.QueryContext(ctx,
		staleQuery(`SELECT id`, where, ` AND id > ? ORDER BY id LIMIT ?`), args...)
	if err != nil {
		return nil, fmt.Errorf("list stale derivations: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, max(limit, 0))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list stale derivations: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// StampDerivation implements storage.DerivationStore.
func (s *ObjectStore) StampDerivation(ctx context.Context, id, sig string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE objects SET derivation_sig = ? WHERE id = ?`, sig, id)
	if err != nil {
		return fmt.Errorf("stamp derivation %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("stamp derivation %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

// staleQuery joins the fixed parts of a stale-row query; where holds only
// constant text and placeholders, never a value.
func staleQuery(selectClause, where, tail string) string {
	var b strings.Builder
	b.WriteString(selectClause)
	b.WriteString(" FROM objects WHERE ")
	b.WriteString(where)
	b.WriteString(tail)
	return b.String()
}

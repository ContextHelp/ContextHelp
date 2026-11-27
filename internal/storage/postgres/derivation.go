package postgres

import (
	"context"
	"fmt"
	"sort"
	"strconv"
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
	defer func() { _ = tx.Rollback() }()
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
			`UPDATE objects SET derivation_sig = $1 WHERE derivation_sig = '' AND pipeline IN ($2, $3)`,
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

// staleClause is the WHERE condition for a stale row, numbering its
// placeholders from $1, with its arguments.
func staleClause(current []string) (string, []any) {
	if len(current) == 0 {
		return `derivation_sig <> ''`, nil
	}
	args := make([]any, len(current))
	ph := make([]string, len(current))
	for i, c := range current {
		args[i] = c
		ph[i] = "$" + strconv.Itoa(i+1)
	}
	return `derivation_sig <> '' AND derivation_sig NOT IN (` + strings.Join(ph, ", ") + `)`, args
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
	n := len(args)
	args = append(args, after, limit)
	rows, err := s.db.QueryContext(ctx,
		staleQuery(`SELECT id`, where, fmt.Sprintf(` AND id > $%d ORDER BY id LIMIT $%d`, n+1, n+2)), args...)
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
	res, err := s.db.ExecContext(ctx, `UPDATE objects SET derivation_sig = $1 WHERE id = $2`, sig, id)
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

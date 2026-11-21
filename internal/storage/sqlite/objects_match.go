package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MatchBySQL implements storage.ObjectStore: one SELECT over the rows
// of ids (the scope ANDed in for a scoped store) whose columns after
// the id are each predicate as 0 or 1. SQLite binds placeholders in
// order: the predicates' arguments, then the ids, then the scope's.
func (s *ObjectStore) MatchBySQL(ctx context.Context, ids []string, preds []storage.SQLPredicate) (map[string][]bool, error) {
	out := make(map[string][]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	cols := make([]string, 0, len(preds)+1)
	cols = append(cols, "id")
	args := make([]any, 0, len(ids))
	for _, p := range preds {
		cols = append(cols, "CASE WHEN ("+p.SQL+") THEN 1 ELSE 0 END")
		args = append(args, p.Args...)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, s.scopeArgs()...)
	// The predicates are compiled RSQL (internal/search) and the scope
	// is the viewer's compiled predicate: literals are bound as ?
	// placeholders in args, only operators and column names reach the
	// SQL text.
	// #nosec G202 -- see above; caller values travel in args, not the string.
	query := "SELECT " + strings.Join(cols, ", ") + " FROM objects WHERE id IN (" + joinPlaceholders(len(ids)) + ")" + s.andScope()
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("match by sql: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		vals := make([]int64, len(preds))
		dest := make([]any, 0, len(preds)+1)
		dest = append(dest, &id)
		for i := range vals {
			dest = append(dest, &vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("match by sql: %w", err)
		}
		matched := make([]bool, len(preds))
		for i, v := range vals {
			matched[i] = v == 1
		}
		out[id] = matched
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("match by sql: %w", err)
	}
	return out, nil
}

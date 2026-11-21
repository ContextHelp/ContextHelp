package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MatchBySQL implements storage.ObjectStore: one SELECT over the rows
// of ids (the scope ANDed in for a scoped store) whose columns after
// the id are each predicate as 0 or 1. The predicates' ? placeholders
// are numbered first, in column order, then the id array, then the
// scope's.
func (s *ObjectStore) MatchBySQL(ctx context.Context, ids []string, preds []storage.SQLPredicate) (map[string][]bool, error) {
	out := make(map[string][]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	idx := 1
	cols := make([]string, 0, len(preds)+1)
	cols = append(cols, "id")
	var args []any
	for _, p := range preds {
		term, next := rebindFrom(p.SQL, idx)
		idx = next
		cols = append(cols, "CASE WHEN ("+term+") THEN 1 ELSE 0 END")
		args = append(args, p.Args...)
	}
	idsAt := idx
	idx++
	args = append(args, pq.Array(ids))
	clause, sargs := s.andScope(&idx)
	args = append(args, sargs...)
	// The predicates are compiled RSQL (internal/search) and the scope
	// is the viewer's compiled predicate: literals are bound as $N
	// placeholders in args, only operators and column names reach the
	// SQL text; the id array is bound too.
	// #nosec G202 -- see above; caller values travel in args, not the string.
	query := "SELECT " + strings.Join(cols, ", ") + fmt.Sprintf(" FROM objects WHERE id = ANY($%d)", idsAt) + clause
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

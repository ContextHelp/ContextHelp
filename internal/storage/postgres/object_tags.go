package postgres

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TagCounts implements storage.ObjectStore: the labels of the jsonb
// tags column of the active, visible objects, unnested and grouped.
// Labels order by byte value, as the other drivers do.
func (s *ObjectStore) TagCounts(ctx context.Context, q storage.TagQuery) ([]storage.TagCount, error) {
	args := []any{storage.ObjectStatusActive}
	idx := 2
	inner := "SELECT id AS oid, CASE WHEN jsonb_typeof(tags) = 'array' THEN tags ELSE '[]'::jsonb END AS tags FROM objects WHERE status = $1"
	if term, sargs := scopeTerm(s.scope, &idx); term != "" {
		inner += " AND " + term
		args = append(args, sargs...)
	}
	query := "SELECT t->>'label' AS label, COUNT(DISTINCT o.oid) AS n FROM ("
	query += inner
	query += ") AS o, jsonb_array_elements(o.tags) AS t WHERE t->>'label' IS NOT NULL AND t->>'label' <> ''"
	if q.Prefix != "" {
		query += fmt.Sprintf(` AND lower(t->>'label') LIKE $%d ESCAPE '\'`, idx)
		args = append(args, storage.PrefixPattern(q.Prefix))
		idx++
	}
	query += ` GROUP BY t->>'label' ORDER BY n DESC, t->>'label' COLLATE "C" ASC`
	if q.Limit > 0 {
		// #nosec G202 -- only the integer placeholder index is formatted in;
		// the limit value itself is bound as a query argument.
		query += fmt.Sprintf(" LIMIT $%d", idx) //nolint:gosec // see #nosec above
		args = append(args, q.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("count tags: %w", err)
	}
	defer rows.Close()
	var out []storage.TagCount
	for rows.Next() {
		var tc storage.TagCount
		if err := rows.Scan(&tc.Label, &tc.Count); err != nil {
			return nil, fmt.Errorf("count tags: %w", err)
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

package sqlite

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TagCounts implements storage.ObjectStore. Tags live in the objects
// table's JSON tags column, so the labels are unnested with json_each
// over the active, visible objects (the scope predicate runs in the
// inner query, where its column names are unambiguous) and grouped. A
// malformed tags value counts as no tags.
func (s *ObjectStore) TagCounts(ctx context.Context, q storage.TagQuery) ([]storage.TagCount, error) {
	inner := "SELECT id AS oid, CASE WHEN json_valid(tags) THEN tags ELSE '[]' END AS tags FROM objects WHERE status = ?"
	args := []any{storage.ObjectStatusActive}
	if term, sargs := scopeTerm(s.scope); term != "" {
		inner += " AND " + term
		args = append(args, sargs...)
	}
	query := "SELECT j.value->>'label' AS label, COUNT(DISTINCT o.oid) AS n FROM ("
	query += inner
	query += ") AS o, json_each(o.tags) AS j WHERE label IS NOT NULL AND label <> ''"
	if q.Prefix != "" {
		query += ` AND lower(label) LIKE ? ESCAPE '\'`
		args = append(args, storage.PrefixPattern(q.Prefix))
	}
	query += " GROUP BY label ORDER BY n DESC, label ASC"
	if q.Limit > 0 {
		query += " LIMIT ?" //nolint:goconst // the same clause in each paged query
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

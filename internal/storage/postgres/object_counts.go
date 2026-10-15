package postgres

import (
	"context"
	"fmt"
)

// CountByType counts every object, whatever its status, by type; a
// scoped store counts the objects its viewer may see.
func (s *ObjectStore) CountByType(ctx context.Context) (map[string]int, error) {
	q := "SELECT type, COUNT(*) FROM objects"
	idx := 1
	term, args := scopeTerm(s.scope, &idx)
	if term != "" {
		q += " WHERE " + term
	}
	rows, err := s.db.QueryContext(ctx, q+" GROUP BY type", args...)
	if err != nil {
		return nil, fmt.Errorf("count objects by type: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			return nil, fmt.Errorf("count objects by type: %w", err)
		}
		counts[typ] = n
	}
	return counts, rows.Err()
}

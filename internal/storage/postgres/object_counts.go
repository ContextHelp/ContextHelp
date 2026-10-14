package postgres

import (
	"context"
	"fmt"
)

// CountByType counts every object, whatever its status, by type.
func (s *ObjectStore) CountByType(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT type, COUNT(*) FROM objects GROUP BY type")
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

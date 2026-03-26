package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func (s *EdgeStore) Create(ctx context.Context, edge *storage.Edge) error {
	metadata, _ := json.Marshal(edge.Metadata)
	_, err := s.db.ExecContext(ctx, `INSERT INTO edges (
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		edge.ID, edge.FromType, edge.FromID, edge.ToType, edge.ToID,
		edge.EdgeType, edge.Weight, metadata, edge.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("create edge: %w", err)
	}
	return nil
}

func (s *EdgeStore) ListFrom(ctx context.Context, fromType, fromID string) ([]*storage.Edge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	FROM edges WHERE from_type = $1 AND from_id = $2`, fromType, fromID)
	if err != nil {
		return nil, fmt.Errorf("list edges from: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *EdgeStore) ListTo(ctx context.Context, toType, toID string) ([]*storage.Edge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	FROM edges WHERE to_type = $1 AND to_id = $2`, toType, toID)
	if err != nil {
		return nil, fmt.Errorf("list edges to: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *EdgeStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM edges WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete edge: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("edge %s not found", id)
	}
	return nil
}

func (s *EdgeStore) DeleteByObject(ctx context.Context, objectID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM edges WHERE (from_type = 'object' AND from_id = $1) OR (to_type = 'object' AND to_id = $1)",
		objectID)
	if err != nil {
		return fmt.Errorf("delete edges by object: %w", err)
	}
	return nil
}

// CountMentionsTo returns the count of edges pointing to the given node (toType, toID).
func (s *EdgeStore) CountMentionsTo(ctx context.Context, toType, toID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM edges WHERE to_type = $1 AND to_id = $2`, toType, toID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count mentions to: %w", err)
	}
	return count, nil
}

// RelatedObjectIDs returns object IDs reachable from objectID by traversing
// shared mention-target edges up to depth hops. Postgres implementation uses
// iterative BFS capped at 3 hops. The seed objectID is never included.
func (s *EdgeStore) RelatedObjectIDs(ctx context.Context, objectID string, depth, limit int) ([]string, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}

	frontier := map[string]struct{}{objectID: {}}
	visited := map[string]struct{}{objectID: {}}
	var result []string

	for hop := 0; hop < depth; hop++ {
		if len(frontier) == 0 {
			break
		}

		ids := make([]any, 0, len(frontier))
		placeholders := make([]string, 0, len(frontier))
		i := 1
		for id := range frontier {
			ids = append(ids, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", i))
			i++
		}

		query := fmt.Sprintf(`
			SELECT DISTINCT e2.from_id
			FROM edges e1
			JOIN edges e2 ON e1.to_id = e2.to_id
			WHERE e1.from_type = 'object'
			  AND e1.from_id IN (%s)
			  AND e1.edge_type = 'mentions'
			  AND e2.from_type = 'object'
			  AND e2.edge_type = 'mentions'`,
			strings.Join(placeholders, ","),
		)

		rows, err := s.db.QueryContext(ctx, query, ids...)
		if err != nil {
			return nil, fmt.Errorf("related objects hop %d: %w", hop+1, err)
		}

		nextFrontier := map[string]struct{}{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan related id: %w", err)
			}
			if _, seen := visited[id]; seen {
				continue
			}
			visited[id] = struct{}{}
			nextFrontier[id] = struct{}{}
			result = append(result, id)
			if limit > 0 && len(result) >= limit {
				rows.Close()
				return result, nil
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("related objects rows: %w", err)
		}

		frontier = nextFrontier
	}

	return result, nil
}

func scanEdges(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*storage.Edge, error) {
	var edges []*storage.Edge
	for rows.Next() {
		var e storage.Edge
		var metadataJSON []byte
		err := rows.Scan(&e.ID, &e.FromType, &e.FromID, &e.ToType, &e.ToID,
			&e.EdgeType, &e.Weight, &metadataJSON, &e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		json.Unmarshal(metadataJSON, &e.Metadata)
		edges = append(edges, &e)
	}
	return edges, rows.Err()
}

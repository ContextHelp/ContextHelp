package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type EdgeStore struct {
	db *sql.DB
}

func (s *EdgeStore) Create(ctx context.Context, edge *storage.Edge) error {
	metadata, _ := json.Marshal(edge.Metadata)

	_, err := s.db.ExecContext(ctx, `INSERT INTO edges (
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		edge.ID, edge.FromType, edge.FromID, edge.ToType, edge.ToID,
		edge.EdgeType, edge.Weight, string(metadata),
		edge.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create edge: %w", err)
	}
	return nil
}

func (s *EdgeStore) ListFrom(ctx context.Context, fromType, fromID string) ([]*storage.Edge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	FROM edges WHERE from_type = ? AND from_id = ?`, fromType, fromID)
	if err != nil {
		return nil, fmt.Errorf("list edges from: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *EdgeStore) ListTo(ctx context.Context, toType, toID string) ([]*storage.Edge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	FROM edges WHERE to_type = ? AND to_id = ?`, toType, toID)
	if err != nil {
		return nil, fmt.Errorf("list edges to: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *EdgeStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM edges WHERE id = ?", id)
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
		"DELETE FROM edges WHERE (from_type = 'object' AND from_id = ?) OR (to_type = 'object' AND to_id = ?)",
		objectID, objectID)
	if err != nil {
		return fmt.Errorf("delete edges by object: %w", err)
	}
	return nil
}

// CountMentionsTo returns the count of edges pointing to the given node (toType, toID).
func (s *EdgeStore) CountMentionsTo(ctx context.Context, toType, toID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM edges WHERE to_type = ? AND to_id = ?`, toType, toID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count mentions to: %w", err)
	}
	return count, nil
}

// RelatedObjectIDs returns object IDs reachable from objectID by traversing
// shared mention-target edges up to depth hops. depth must be >= 1 and is
// capped at 3 to prevent runaway traversal. The seed objectID is never
// included in the result. limit caps the total returned IDs (0 = no cap).
func (s *EdgeStore) RelatedObjectIDs(ctx context.Context, objectID string, depth, limit int) ([]string, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}

	// frontier = current set of object IDs to expand; visited prevents revisiting.
	frontier := map[string]struct{}{objectID: {}}
	visited := map[string]struct{}{objectID: {}}
	var result []string

	for hop := 0; hop < depth; hop++ {
		if len(frontier) == 0 {
			break
		}

		// Collect mention targets for all frontier objects.
		args := make([]any, 0, len(frontier))
		for id := range frontier {
			args = append(args, id)
		}

		// Find shared-target neighbours: objects that share any mention target
		// with any object in the current frontier.
		// #nosec G201 -- placeholders are literal "?" strings, not user input
		query := fmt.Sprintf(`
			SELECT DISTINCT e2.from_id
			FROM edges e1
			JOIN edges e2 ON e1.to_id = e2.to_id
			WHERE e1.from_type = 'object'
			  AND e1.from_id IN (%s)
			  AND e1.edge_type = 'mentions'
			  AND e2.from_type = 'object'
			  AND e2.edge_type = 'mentions'`,
			joinPlaceholders(len(frontier)),
		)

		rows, err := s.db.QueryContext(ctx, query, args...)
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

func joinPlaceholders(n int) string {
	s := make([]string, n)
	for i := range s {
		s[i] = "?"
	}
	return strings.Join(s, ",")
}

func scanEdges(rows *sql.Rows) ([]*storage.Edge, error) {
	var edges []*storage.Edge
	for rows.Next() {
		var e storage.Edge
		var metadataJSON, createdAt string
		err := rows.Scan(&e.ID, &e.FromType, &e.FromID, &e.ToType, &e.ToID,
			&e.EdgeType, &e.Weight, &metadataJSON, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		json.Unmarshal([]byte(metadataJSON), &e.Metadata)
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		edges = append(edges, &e)
	}
	return edges, rows.Err()
}

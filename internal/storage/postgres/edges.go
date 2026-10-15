package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

// edgeExecer is what an edge write runs on: the database or a
// transaction.
type edgeExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *EdgeStore) Create(ctx context.Context, edge *storage.Edge) error {
	return insertEdge(ctx, s.db, edge)
}

// CreateAll stores every edge in one transaction.
func (s *EdgeStore) CreateAll(ctx context.Context, edges []*storage.Edge) error {
	if len(edges) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create edges: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, e := range edges {
		if err := insertEdge(ctx, tx, e); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create edges: commit: %w", err)
	}
	return nil
}

// CreateMissing stores, in one transaction, the edges no stored edge
// matches on (from, to, edge type) and returns how many it stored.
func (s *EdgeStore) CreateMissing(ctx context.Context, edges []*storage.Edge) (int, error) {
	if len(edges) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("create missing edges: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stored := 0
	for _, e := range edges {
		n, err := insertMissingEdge(ctx, tx, e)
		if err != nil {
			return 0, err
		}
		stored += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("create missing edges: commit: %w", err)
	}
	return stored, nil
}

// DeleteMatching deletes every edge the matches select in one
// transaction.
func (s *EdgeStore) DeleteMatching(ctx context.Context, matches []storage.EdgeMatch) (int, error) {
	for _, m := range matches {
		if len(m.EdgeTypes) == 0 {
			return 0, fmt.Errorf("delete edges %s/%s -> %s/%s: no edge types", m.FromType, m.FromID, m.ToType, m.ToID)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("delete edges: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	deleted := 0
	for _, m := range matches {
		res, err := tx.ExecContext(ctx, `DELETE FROM edges
			WHERE from_type = $1 AND from_id = $2 AND to_type = $3 AND to_id = $4
			AND edge_type = ANY($5)`,
			m.FromType, m.FromID, m.ToType, m.ToID, pq.Array(m.EdgeTypes))
		if err != nil {
			return 0, fmt.Errorf("delete edges: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("delete edges: %w", err)
		}
		deleted += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("delete edges: commit: %w", err)
	}
	return deleted, nil
}

func insertEdge(ctx context.Context, db edgeExecer, edge *storage.Edge) error {
	metadata, _ := json.Marshal(edge.Metadata)
	_, err := db.ExecContext(ctx, `INSERT INTO edges (
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

// insertMissingEdge inserts edge unless an edge between the same nodes
// with the same type is stored, and returns 1 when it inserted.
func insertMissingEdge(ctx context.Context, db edgeExecer, edge *storage.Edge) (int, error) {
	metadata, _ := json.Marshal(edge.Metadata)
	res, err := db.ExecContext(ctx, `INSERT INTO edges (
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	) SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9
	WHERE NOT EXISTS (
		SELECT 1 FROM edges
		WHERE from_type = $10 AND from_id = $11 AND to_type = $12 AND to_id = $13 AND edge_type = $14
	)`,
		edge.ID, edge.FromType, edge.FromID, edge.ToType, edge.ToID,
		edge.EdgeType, edge.Weight, metadata, edge.CreatedAt.UTC(),
		edge.FromType, edge.FromID, edge.ToType, edge.ToID, edge.EdgeType,
	)
	if err != nil {
		return 0, fmt.Errorf("create missing edge: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("create missing edge: %w", err)
	}
	return int(n), nil
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

		// #nosec G201 -- placeholders are "$N" parameter markers, not user input
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

		nextFrontier, done, err := s.relatedHop(ctx, query, ids, visited, &result, limit)
		if err != nil {
			return nil, fmt.Errorf("related objects hop %d: %w", hop+1, err)
		}
		if done {
			return result, nil
		}

		frontier = nextFrontier
	}

	return result, nil
}

// relatedHop runs one breadth-first hop of RelatedObjects. It is a separate
// function so rows.Close can be deferred per hop rather than closed by hand on
// every early-return path. done reports that the caller-supplied limit was
// reached and iteration should stop.
func (s *EdgeStore) relatedHop(
	ctx context.Context,
	query string,
	ids []any,
	visited map[string]struct{},
	result *[]string,
	limit int,
) (map[string]struct{}, bool, error) {
	rows, err := s.db.QueryContext(ctx, query, ids...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	nextFrontier := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, false, fmt.Errorf("scan related id: %w", err)
		}
		if _, seen := visited[id]; seen {
			continue
		}
		visited[id] = struct{}{}
		nextFrontier[id] = struct{}{}
		*result = append(*result, id)
		if limit > 0 && len(*result) >= limit {
			return nextFrontier, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("related objects rows: %w", err)
	}
	return nextFrontier, false, nil
}

func scanEdges(rows *sql.Rows) ([]*storage.Edge, error) {
	return rowscan.All(rows, func(rows *sql.Rows) (*storage.Edge, error) {
		var e storage.Edge
		var metadataJSON []byte
		err := rows.Scan(&e.ID, &e.FromType, &e.FromID, &e.ToType, &e.ToID,
			&e.EdgeType, &e.Weight, &metadataJSON, &e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		if err := decodeJSONColumn(metadataJSON, "metadata", &e.Metadata); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		return &e, nil
	})
}

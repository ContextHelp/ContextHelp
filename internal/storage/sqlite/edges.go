package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

type EdgeStore struct {
	db *sql.DB
	// scope, when set, keeps every read to the edges whose object ends
	// the viewer may see (see scope.go).
	scope *storage.Scope
}

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
		args := []any{m.FromType, m.FromID, m.ToType, m.ToID}
		for _, t := range m.EdgeTypes {
			args = append(args, t)
		}
		// #nosec G202 -- the IN list is "?" placeholders only
		res, err := tx.ExecContext(ctx, `DELETE FROM edges
			WHERE from_type = ? AND from_id = ? AND to_type = ? AND to_id = ?
			AND edge_type IN (`+strings.Repeat("?,", len(m.EdgeTypes)-1)+`?)`, args...)
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

// insertMissingEdge inserts edge unless an edge between the same nodes
// with the same type is stored, and returns 1 when it inserted.
func insertMissingEdge(ctx context.Context, db edgeExecer, edge *storage.Edge) (int, error) {
	metadata, _ := json.Marshal(edge.Metadata)

	res, err := db.ExecContext(ctx, `INSERT INTO edges (
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	) SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?
	WHERE NOT EXISTS (
		SELECT 1 FROM edges
		WHERE from_type = ? AND from_id = ? AND to_type = ? AND to_id = ? AND edge_type = ?
	)`,
		edge.ID, edge.FromType, edge.FromID, edge.ToType, edge.ToID,
		edge.EdgeType, edge.Weight, string(metadata),
		edge.CreatedAt.Format(time.RFC3339),
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
	clause, sargs := edgeScope(s.scope)
	// #nosec G202 -- clause is fixed SQL over compiled predicates; values are bound.
	rows, err := s.db.QueryContext(ctx, `SELECT
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	FROM edges WHERE from_type = ? AND from_id = ?`+clause, append([]any{fromType, fromID}, sargs...)...)
	if err != nil {
		return nil, fmt.Errorf("list edges from: %w", err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

func (s *EdgeStore) ListTo(ctx context.Context, toType, toID string) ([]*storage.Edge, error) {
	clause, sargs := edgeScope(s.scope)
	// #nosec G202 -- clause is fixed SQL over compiled predicates; values are bound.
	rows, err := s.db.QueryContext(ctx, `SELECT
		id, from_type, from_id, to_type, to_id, edge_type, weight, metadata, created_at
	FROM edges WHERE to_type = ? AND to_id = ?`+clause, append([]any{toType, toID}, sargs...)...)
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

// CountMentionsTo returns the count of edges pointing to the given node
// (toType, toID); a scoped store counts the edges from visible objects
// (ADR-080 §6: rerank signals over visible objects).
func (s *EdgeStore) CountMentionsTo(ctx context.Context, toType, toID string) (int, error) {
	var count int
	clause, sargs := edgeScope(s.scope)
	// #nosec G202 -- clause is fixed SQL over compiled predicates; values are bound.
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM edges WHERE to_type = ? AND to_id = ?`+clause, append([]any{toType, toID}, sargs...)...).Scan(&count)
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

		ids, err := s.sharedMentionNeighbours(ctx, frontier)
		if err != nil {
			return nil, fmt.Errorf("related objects hop %d: %w", hop+1, err)
		}

		nextFrontier := map[string]struct{}{}
		for _, id := range ids {
			if _, seen := visited[id]; seen {
				continue
			}
			visited[id] = struct{}{}
			nextFrontier[id] = struct{}{}
			result = append(result, id)
			if limit > 0 && len(result) >= limit {
				return result, nil
			}
		}

		frontier = nextFrontier
	}

	return result, nil
}

// sharedMentionNeighbours returns the distinct objects that share any mention
// target with an object in frontier, in the order the query yields them.
func (s *EdgeStore) sharedMentionNeighbours(ctx context.Context, frontier map[string]struct{}) ([]string, error) {
	args := make([]any, 0, len(frontier))
	for id := range frontier {
		args = append(args, id)
	}

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
	// Both sides of the traversal stay among visible objects: the seed
	// and the neighbour (ADR-080 §6).
	if s.scope != nil {
		vis, vargs := visibleObjects(*s.scope)
		query += " AND e1.from_id IN " + vis + " AND e2.from_id IN " + vis
		args = append(append(args, vargs...), vargs...)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return rowscan.All(rows, func(rows *sql.Rows) (string, error) {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("scan related id: %w", err)
		}
		return id, nil
	})
}

func joinPlaceholders(n int) string {
	s := make([]string, n)
	for i := range s {
		s[i] = "?"
	}
	return strings.Join(s, ",")
}

func scanEdges(rows *sql.Rows) ([]*storage.Edge, error) {
	return rowscan.All(rows, func(rows *sql.Rows) (*storage.Edge, error) {
		var e storage.Edge
		var metadataJSON, createdAt string
		err := rows.Scan(&e.ID, &e.FromType, &e.FromID, &e.ToType, &e.ToID,
			&e.EdgeType, &e.Weight, &metadataJSON, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		if err := decodeJSONColumn(metadataJSON, "metadata", &e.Metadata); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		return &e, nil
	})
}

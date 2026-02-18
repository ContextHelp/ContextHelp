package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

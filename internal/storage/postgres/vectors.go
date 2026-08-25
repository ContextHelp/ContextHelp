package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// pgCaps carries server capabilities detected at migration time, shared by
// the stores that adapt query strategy to them.
type pgCaps struct {
	// iterativeScan is true when the vector extension supports
	// hnsw.iterative_scan (>= 0.8.0). pgvector applies WHERE after HNSW
	// traversal, so without iterative scans a filtered KNN query can
	// under-return below LIMIT even when matches exist.
	iterativeScan bool
}

// pgvectorSupportsIterativeScan reports whether the given pgvector extension
// version (e.g. "0.8.6") supports the hnsw.iterative_scan GUC, introduced in
// 0.8.0. Unknown or unparsable versions report false: SET LOCAL of an
// unrecognized GUC errors, so the guard must fail closed.
func pgvectorSupportsIterativeScan(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return major > 0 || minor >= 8
}

// isZeroVector reports whether every component is zero.
func isZeroVector(v []float32) bool {
	for _, f := range v {
		if f != 0 {
			return false
		}
	}
	return true
}

// VectorStore implements storage.VectorStore over the objects.embedding
// pgvector column — the same column ObjectStore.VectorSearch queries and the
// HNSW cosine index (migration 11) covers. Unlike SQLite's standalone vec0
// virtual table, the vector index on Postgres is not separable from the
// objects row, so Upsert requires the object to exist; everything a vector
// search returns must resolve to an object anyway, and a second copy of the
// embeddings would only drift.
type VectorStore struct {
	db   *sql.DB
	caps *pgCaps
}

// Upsert stores the vector on the object's embedding column.
//
// Zero-magnitude vectors are not indexed: cosine distance — the pinned
// cross-driver metric — is undefined for them. Any stored embedding is
// cleared instead, so a re-embed to zero removes the index entry, mirroring
// the SQLite VecStore contract.
func (s *VectorStore) Upsert(ctx context.Context, id string, vector []float32) error {
	if isZeroVector(vector) {
		return s.Delete(ctx, id)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE objects SET embedding = $2::vector, vector_indexed = TRUE WHERE id = $1`,
		id, encodePgVector(vector),
	)
	if err != nil {
		return fmt.Errorf("vector store upsert: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("vector store upsert: object %s not found", id)
	}
	return nil
}

// Search returns the top-K nearest neighbours by cosine distance, ascending.
// Score carries the raw distance, matching the SQLite VecStore contract.
// Rows whose distance is undefined (zero-magnitude embeddings written
// outside Upsert) are excluded: cosine distance lives in [0, 2], and the
// range predicate is false for NaN.
func (s *VectorStore) Search(ctx context.Context, vector []float32, topK int) ([]storage.VectorHit, error) {
	if topK <= 0 {
		topK = 10
	}
	// Cosine distance to a zero-magnitude query is undefined for every row;
	// SQLite's vec0 reports NULL distances and returns nothing. Match that.
	if isZeroVector(vector) {
		return nil, nil
	}

	var hits []storage.VectorHit
	err := queryVectorRows(ctx, s.db, s.caps, func(rows *sql.Rows) error {
		for rows.Next() {
			var h storage.VectorHit
			if err := rows.Scan(&h.ID, &h.Score); err != nil {
				return err
			}
			hits = append(hits, h)
		}
		return nil
	},
		`SELECT id, embedding <=> $1::vector AS distance
		   FROM objects
		  WHERE embedding IS NOT NULL
		    AND (embedding <=> $1::vector) <= 2
		  ORDER BY embedding <=> $1::vector
		  LIMIT $2`,
		encodePgVector(vector), topK,
	)
	if err != nil {
		return nil, fmt.Errorf("vector store search: %w", err)
	}
	return hits, nil
}

// Delete clears the vector for the given object ID. A missing ID is a no-op,
// mirroring SQLite.
func (s *VectorStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE objects SET embedding = NULL, vector_indexed = FALSE WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("vector store delete: %w", err)
	}
	return nil
}

// Count returns the number of objects currently carrying a vector.
func (s *VectorStore) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM objects WHERE embedding IS NOT NULL`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("vector store count: %w", err)
	}
	return n, nil
}

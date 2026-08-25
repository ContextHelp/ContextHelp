package sqlite

import (
	"context"
	"database/sql"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// VecStore implements storage.VectorStore using the sqlite-vec vec0 virtual table.
// vec0 provides ANN search via a KNN query; the table is created with
// distance_metric=cosine (migration 034), the pinned cross-driver contract.
type VecStore struct {
	db *sql.DB
}

// Upsert inserts or replaces the vector for the given object ID.
//
// Zero-magnitude vectors are not indexed: cosine distance is undefined for
// them (the vec0 table pins distance_metric=cosine), and vec0 would report
// NULL distances that occupy KNN result slots. Any stale row is removed
// instead, so a re-embed to zero clears the index entry.
func (s *VecStore) Upsert(ctx context.Context, id string, vector []float32) error {
	if isZeroVector(vector) {
		return s.Delete(ctx, id)
	}
	blob, err := sqlite_vec.SerializeFloat32(vector)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO vec_objects(id, embedding) VALUES (?, ?)`,
		id, blob,
	)
	return err
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

// Search returns the top-K nearest neighbours to vector, ordered by distance ascending.
// Ties are broken by insertion order (SQLite internal rowid).
func (s *VecStore) Search(ctx context.Context, vector []float32, topK int) ([]storage.VectorHit, error) {
	if topK <= 0 {
		topK = 10
	}
	blob, err := sqlite_vec.SerializeFloat32(vector)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, distance
		 FROM vec_objects
		 WHERE embedding MATCH ?
		 ORDER BY distance
		 LIMIT ?`,
		blob, topK,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []storage.VectorHit
	for rows.Next() {
		var h storage.VectorHit
		var dist sql.NullFloat64
		if err := rows.Scan(&h.ID, &dist); err != nil {
			return nil, err
		}
		if !dist.Valid {
			// Defensive: rows indexed before the zero-vector guard (or by
			// external writers) have no defined cosine distance and cannot
			// be ranked.
			continue
		}
		h.Score = dist.Float64
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// Delete removes the vector for the given object ID.
func (s *VecStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM vec_objects WHERE id = ?`, id,
	)
	return err
}

// Count returns the number of vectors currently indexed.
func (s *VecStore) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vec_objects`).Scan(&n)
	return n, err
}

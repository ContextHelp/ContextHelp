package sqlite

import (
	"context"
	"database/sql"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// VecStore implements storage.VectorStore using the sqlite-vec vec0 virtual table.
// vec0 provides ANN search via a KNN query; exact distance metric is L2 by default.
type VecStore struct {
	db *sql.DB
}

// Upsert inserts or replaces the vector for the given object ID.
func (s *VecStore) Upsert(ctx context.Context, id string, vector []float32) error {
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
		if err := rows.Scan(&h.ID, &h.Score); err != nil {
			return nil, err
		}
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

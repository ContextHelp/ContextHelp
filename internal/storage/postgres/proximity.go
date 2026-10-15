package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
)

type ProximityStore struct {
	db *sql.DB
	// scope, when set, keeps every neighbour read to pairs whose both
	// objects the viewer may see (see scope.go).
	scope *storage.Scope
}

// pairScope is " AND object_a IN visible AND object_b IN visible",
// numbered from *idx, with its arguments; empty without a scope.
func (s *ProximityStore) pairScope(idx *int) (string, []any) {
	if s.scope == nil {
		return "", nil
	}
	vis, args := visibleObjects(*s.scope, idx)
	return " AND object_a IN " + vis + " AND object_b IN " + vis, args
}

func (s *ProximityStore) GetNeighbors(ctx context.Context, objectID string, limit int) ([]*storage.ProximityScore, error) {
	idx := 3
	clause, sargs := s.pairScope(&idx)
	// #nosec G202 -- clause is fixed SQL over compiled predicates; values are bound.
	rows, err := s.db.QueryContext(ctx, `
		SELECT object_a, object_b, score, semantic, temporal, entity, origin, behavioral, computed_at
		FROM object_proximity
		WHERE (object_a = $1 OR object_b = $1)`+clause+`
		ORDER BY score DESC
		LIMIT $2`, append([]any{objectID, limit}, sargs...)...)
	if err != nil {
		return nil, fmt.Errorf("proximity.GetNeighbors: %w", err)
	}
	defer rows.Close()
	return scanProximityRows(rows)
}

func (s *ProximityStore) GetNeighborsAbove(ctx context.Context, objectID string, threshold float64) ([]*storage.ProximityScore, error) {
	idx := 3
	clause, sargs := s.pairScope(&idx)
	// #nosec G202 -- clause is fixed SQL over compiled predicates; values are bound.
	rows, err := s.db.QueryContext(ctx, `
		SELECT object_a, object_b, score, semantic, temporal, entity, origin, behavioral, computed_at
		FROM object_proximity
		WHERE (object_a = $1 OR object_b = $1) AND score >= $2`+clause+`
		ORDER BY score DESC`, append([]any{objectID, threshold}, sargs...)...)
	if err != nil {
		return nil, fmt.Errorf("proximity.GetNeighborsAbove: %w", err)
	}
	defer rows.Close()
	return scanProximityRows(rows)
}

func (s *ProximityStore) Get(ctx context.Context, objectA, objectB string) (*storage.ProximityScore, error) {
	if objectA > objectB {
		objectA, objectB = objectB, objectA
	}
	var p storage.ProximityScore
	idx := 3
	clause, sargs := s.pairScope(&idx)
	// #nosec G202 -- clause is fixed SQL over compiled predicates; values are bound.
	err := s.db.QueryRowContext(ctx, `
		SELECT object_a, object_b, score, semantic, temporal, entity, origin, behavioral, computed_at
		FROM object_proximity
		WHERE object_a = $1 AND object_b = $2`+clause, append([]any{objectA, objectB}, sargs...)...).Scan(
		&p.ObjectA, &p.ObjectB, &p.Score,
		&p.Factors.Semantic, &p.Factors.Temporal, &p.Factors.Entity,
		&p.Factors.Origin, &p.Factors.Behavioral,
		&p.ComputedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proximity.Get: %w", err)
	}
	return &p, nil
}

func (s *ProximityStore) Put(ctx context.Context, score *storage.ProximityScore) error {
	a, b := score.ObjectA, score.ObjectB
	if a > b {
		a, b = b, a
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO object_proximity (object_a, object_b, score, semantic, temporal, entity, origin, behavioral, computed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (object_a, object_b) DO UPDATE SET
			score = EXCLUDED.score,
			semantic = EXCLUDED.semantic,
			temporal = EXCLUDED.temporal,
			entity = EXCLUDED.entity,
			origin = EXCLUDED.origin,
			behavioral = EXCLUDED.behavioral,
			computed_at = EXCLUDED.computed_at`,
		a, b, score.Score,
		score.Factors.Semantic, score.Factors.Temporal, score.Factors.Entity,
		score.Factors.Origin, score.Factors.Behavioral,
		score.ComputedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("proximity.Put: %w", err)
	}
	return nil
}

func (s *ProximityStore) PutBatch(ctx context.Context, scores []*storage.ProximityScore) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("proximity.PutBatch: begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO object_proximity (object_a, object_b, score, semantic, temporal, entity, origin, behavioral, computed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (object_a, object_b) DO UPDATE SET
			score = EXCLUDED.score,
			semantic = EXCLUDED.semantic,
			temporal = EXCLUDED.temporal,
			entity = EXCLUDED.entity,
			origin = EXCLUDED.origin,
			behavioral = EXCLUDED.behavioral,
			computed_at = EXCLUDED.computed_at`)
	if err != nil {
		return fmt.Errorf("proximity.PutBatch: prepare: %w", err)
	}
	// The statement belongs to tx: Commit or Rollback releases it anyway,
	// and the outcome that matters is Commit's error.
	defer func() { _ = stmt.Close() }()

	for _, score := range scores {
		a, b := score.ObjectA, score.ObjectB
		if a > b {
			a, b = b, a
		}
		if _, err := stmt.ExecContext(ctx,
			a, b, score.Score,
			score.Factors.Semantic, score.Factors.Temporal, score.Factors.Entity,
			score.Factors.Origin, score.Factors.Behavioral,
			score.ComputedAt.UTC(),
		); err != nil {
			return fmt.Errorf("proximity.PutBatch: exec: %w", err)
		}
	}
	return tx.Commit()
}

func (s *ProximityStore) Delete(ctx context.Context, objectID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM object_proximity WHERE object_a = $1 OR object_b = $1`, objectID)
	if err != nil {
		return fmt.Errorf("proximity.Delete: %w", err)
	}
	return nil
}

func (s *ProximityStore) FindStale(ctx context.Context, cutoff time.Time, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT object_a FROM object_proximity WHERE computed_at < $1
		UNION
		SELECT DISTINCT object_b FROM object_proximity WHERE computed_at < $1
		ORDER BY 1 LIMIT $2`,
		cutoff.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("proximity.FindStale: %w", err)
	}
	defer rows.Close()

	return rowscan.All(rows, func(rows *sql.Rows) (string, error) {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("proximity.FindStale: scan: %w", err)
		}
		return id, nil
	})
}

func (s *ProximityStore) Stats(ctx context.Context) (*storage.ProximityStats, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(AVG(score), 0),
			COALESCE(MAX(score), 0),
			COALESCE(SUM(CASE WHEN score >= 0.7 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN score >= 0.4 AND score < 0.7 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN score >= 0.2 AND score < 0.4 THEN 1 ELSE 0 END), 0)
		FROM object_proximity`)

	var stats storage.ProximityStats
	if err := row.Scan(
		&stats.TotalPairs, &stats.AvgScore, &stats.MaxScore,
		&stats.HighProximity, &stats.MediumProximity, &stats.LowProximity,
	); err != nil {
		return nil, fmt.Errorf("proximity.Stats: %w", err)
	}
	return &stats, nil
}

func scanProximityRows(rows *sql.Rows) ([]*storage.ProximityScore, error) {
	return rowscan.All(rows, func(rows *sql.Rows) (*storage.ProximityScore, error) {
		var p storage.ProximityScore
		err := rows.Scan(
			&p.ObjectA, &p.ObjectB, &p.Score,
			&p.Factors.Semantic, &p.Factors.Temporal, &p.Factors.Entity,
			&p.Factors.Origin, &p.Factors.Behavioral,
			&p.ComputedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("proximity: scan: %w", err)
		}
		return &p, nil
	})
}

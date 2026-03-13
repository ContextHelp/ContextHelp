package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ProximityStore implements storage.ProximityStore for SQLite.
type ProximityStore struct {
	db *sql.DB
}

func (s *ProximityStore) GetNeighbors(ctx context.Context, objectID string, limit int) ([]*storage.ProximityScore, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT object_a, object_b, score, semantic, temporal, entity, origin, behavioral, computed_at
		FROM object_proximity
		WHERE object_a = ? OR object_b = ?
		ORDER BY score DESC
		LIMIT ?`, objectID, objectID, limit)
	if err != nil {
		return nil, fmt.Errorf("proximity.GetNeighbors: %w", err)
	}
	defer rows.Close()
	return scanProximityRows(rows)
}

func (s *ProximityStore) GetNeighborsAbove(ctx context.Context, objectID string, threshold float64) ([]*storage.ProximityScore, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT object_a, object_b, score, semantic, temporal, entity, origin, behavioral, computed_at
		FROM object_proximity
		WHERE (object_a = ? OR object_b = ?) AND score >= ?
		ORDER BY score DESC`, objectID, objectID, threshold)
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
	var computedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT object_a, object_b, score, semantic, temporal, entity, origin, behavioral, computed_at
		FROM object_proximity
		WHERE object_a = ? AND object_b = ?`, objectA, objectB).Scan(
		&p.ObjectA, &p.ObjectB, &p.Score,
		&p.Factors.Semantic, &p.Factors.Temporal, &p.Factors.Entity,
		&p.Factors.Origin, &p.Factors.Behavioral,
		&computedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proximity.Get: %w", err)
	}
	if t, parseErr := time.Parse(time.RFC3339, computedAt); parseErr == nil {
		p.ComputedAt = t
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(object_a, object_b) DO UPDATE SET
			score=excluded.score,
			semantic=excluded.semantic,
			temporal=excluded.temporal,
			entity=excluded.entity,
			origin=excluded.origin,
			behavioral=excluded.behavioral,
			computed_at=excluded.computed_at`,
		a, b, score.Score,
		score.Factors.Semantic, score.Factors.Temporal, score.Factors.Entity,
		score.Factors.Origin, score.Factors.Behavioral,
		score.ComputedAt.UTC().Format(time.RFC3339),
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(object_a, object_b) DO UPDATE SET
			score=excluded.score,
			semantic=excluded.semantic,
			temporal=excluded.temporal,
			entity=excluded.entity,
			origin=excluded.origin,
			behavioral=excluded.behavioral,
			computed_at=excluded.computed_at`)
	if err != nil {
		return fmt.Errorf("proximity.PutBatch: prepare: %w", err)
	}
	defer stmt.Close()

	for _, score := range scores {
		a, b := score.ObjectA, score.ObjectB
		if a > b {
			a, b = b, a
		}
		if _, err := stmt.ExecContext(ctx,
			a, b, score.Score,
			score.Factors.Semantic, score.Factors.Temporal, score.Factors.Entity,
			score.Factors.Origin, score.Factors.Behavioral,
			score.ComputedAt.UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("proximity.PutBatch: exec: %w", err)
		}
	}
	return tx.Commit()
}

func (s *ProximityStore) Delete(ctx context.Context, objectID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM object_proximity WHERE object_a = ? OR object_b = ?`,
		objectID, objectID)
	if err != nil {
		return fmt.Errorf("proximity.Delete: %w", err)
	}
	return nil
}

func (s *ProximityStore) FindStale(ctx context.Context, cutoff time.Time, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT object_a FROM object_proximity WHERE computed_at < ?
		UNION
		SELECT DISTINCT object_b FROM object_proximity WHERE computed_at < ?
		ORDER BY 1 LIMIT ?`,
		cutoff.UTC().Format(time.RFC3339), cutoff.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("proximity.FindStale: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("proximity.FindStale: scan: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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

// scanProximityRows scans all rows from a proximity query.
func scanProximityRows(rows *sql.Rows) ([]*storage.ProximityScore, error) {
	var results []*storage.ProximityScore
	for rows.Next() {
		row := &rowScanner{rows: rows}
		p, err := scanProximityRow(row)
		if err != nil {
			return nil, err
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

// rowScanner adapts *sql.Rows to the same Scan interface as *sql.Row.
type rowScanner struct {
	rows *sql.Rows
}

func (r *rowScanner) Scan(dest ...any) error {
	return r.rows.Scan(dest...)
}

// scanner is satisfied by both *sql.Row and *rowScanner.
type scanner interface {
	Scan(dest ...any) error
}

func scanProximityRow(s scanner) (*storage.ProximityScore, error) {
	var p storage.ProximityScore
	var computedAt string
	err := s.Scan(
		&p.ObjectA, &p.ObjectB, &p.Score,
		&p.Factors.Semantic, &p.Factors.Temporal, &p.Factors.Entity,
		&p.Factors.Origin, &p.Factors.Behavioral,
		&computedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("proximity: scan: %w", err)
	}
	if t, err := time.Parse(time.RFC3339, computedAt); err == nil {
		p.ComputedAt = t
	}
	return &p, nil
}

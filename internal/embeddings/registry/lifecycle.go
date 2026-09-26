package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Lifecycle refusals (ADR-071 "Default-flip control"). Every refusal leaves
// the registry, the rows and the indexes exactly as they were.
var (
	// ErrBelowCoverage refuses a set-default whose candidate covers less of
	// the corpus than the threshold. The error is a *CoverageError.
	ErrBelowCoverage = errors.New("embedding model coverage is below the set-default threshold")
	// ErrModelDeprecated refuses promoting a model with a deprecation
	// scheduled or in effect.
	ErrModelDeprecated = errors.New("embedding model is deprecated")
	// ErrModelNotMeasured refuses promoting a model without a measured
	// dimension: it has no index for the query path to search.
	ErrModelNotMeasured = errors.New("embedding model has no measured dimension")
	// ErrIsDefault refuses deprecating or purging the default model; the
	// operator promotes a successor first.
	ErrIsDefault = errors.New("embedding model is the default")
	// ErrNotDeprecated refuses purging a model that was never deprecated.
	ErrNotDeprecated = errors.New("embedding model is not deprecated")
	// ErrGracePeriod refuses purging before the deprecation plus the grace
	// period has passed. The error is a *GraceError.
	ErrGracePeriod = errors.New("embedding model's purge grace period has not elapsed")
)

// coverageEpsilon absorbs float rounding when comparing a measured coverage
// fraction with the threshold, so 99/100 meets 0.99.
const coverageEpsilon = 1e-9

// CoverageError is the ErrBelowCoverage refusal with the measured values.
type CoverageError struct {
	ModelID     string
	Coverage    float64
	MinCoverage float64
}

func (e *CoverageError) Error() string {
	return fmt.Sprintf("%s: %s covers %.4f of the corpus, below the minimum %.4f",
		ErrBelowCoverage, e.ModelID, e.Coverage, e.MinCoverage)
}

func (e *CoverageError) Unwrap() error { return ErrBelowCoverage }

// GraceError is the ErrGracePeriod refusal with the dates involved.
type GraceError struct {
	ModelID      string
	DeprecatedAt time.Time
	EligibleAt   time.Time
}

func (e *GraceError) Error() string {
	return fmt.Sprintf("%s: %s was deprecated at %s and can be purged from %s",
		ErrGracePeriod, e.ModelID, e.DeprecatedAt.UTC().Format(time.RFC3339), e.EligibleAt.UTC().Format(time.RFC3339))
}

func (e *GraceError) Unwrap() error { return ErrGracePeriod }

// Promotion reports a set-default.
type Promotion struct {
	ModelID string
	// Previous is the default before the flip; empty when there was none.
	// Equal to ModelID when the model already was the default.
	Previous    string
	Coverage    float64
	MinCoverage float64
	// Changed is false when the model already was the default.
	Changed bool
}

// SetDefault makes modelID the default model, the one the query path reads.
// It refuses, changing nothing, when the model is unknown, deprecated
// (scheduled or effective), unmeasured, or covers less than minCoverage
// (0..1) of the corpus. Coverage is measured inside the flip's transaction,
// the same way ListWithCoverage reports it.
//
// The flip is one transaction and flips serialize: the first statement
// takes the write lock (SQLite) or a self-exclusive table lock (Postgres),
// so concurrent flips queue instead of racing, and the partial unique index
// on is_default backs the at-most-one-default invariant. Promoting the
// current default is a no-op that reports Changed = false.
func (s *Store) SetDefault(ctx context.Context, modelID string, minCoverage float64) (Promotion, error) {
	return s.setDefault(ctx, modelID, minCoverage, true)
}

// PlanSetDefault runs SetDefault's guards and reports what the flip would
// do, then rolls back.
func (s *Store) PlanSetDefault(ctx context.Context, modelID string, minCoverage float64) (Promotion, error) {
	return s.setDefault(ctx, modelID, minCoverage, false)
}

func (s *Store) setDefault(ctx context.Context, modelID string, minCoverage float64, commit bool) (Promotion, error) {
	if minCoverage < 0 || minCoverage > 1 {
		return Promotion{}, fmt.Errorf("registry.SetDefault: minimum coverage %v is outside 0..1", minCoverage)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Promotion{}, fmt.Errorf("registry.SetDefault: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if s.postgres {
		// SHARE ROW EXCLUSIVE conflicts with itself and with writers but
		// not with readers: concurrent flips wait here, and each then reads
		// the default the previous one committed.
		if _, err := tx.ExecContext(ctx, `LOCK TABLE embedding_models IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return Promotion{}, fmt.Errorf("registry.SetDefault: lock registry: %w", err)
		}
	}
	// Write first. On SQLite a deferred transaction that reads before
	// writing fails with SQLITE_BUSY when another writer commits in
	// between, bypassing busy_timeout; writing first takes the lock, so
	// concurrent flips queue, and every read below sees the latest commit.
	// A refusal rolls this back.
	var prev sql.NullString
	err = tx.QueryRowContext(
		ctx,
		`UPDATE embedding_models SET is_default = 0 WHERE is_default = 1 RETURNING model_id`,
	).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Promotion{}, fmt.Errorf("registry.SetDefault: clear existing default: %w", err)
	}
	p := Promotion{ModelID: modelID, Previous: prev.String, MinCoverage: minCoverage}

	m, err := s.get(ctx, tx, modelID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrModelNotFound) {
			return Promotion{}, fmt.Errorf("registry.SetDefault %s: %w", modelID, ErrModelNotFound)
		}
		return Promotion{}, fmt.Errorf("registry.SetDefault %s: %w", modelID, err)
	}
	total, err := objectCount(ctx, tx)
	if err != nil {
		return Promotion{}, fmt.Errorf("registry.SetDefault: %w", err)
	}
	if p.Coverage, err = s.coverage(ctx, tx, modelID, total); err != nil {
		return Promotion{}, fmt.Errorf("registry.SetDefault: %w", err)
	}
	if p.Previous == modelID {
		// Already the default: nothing to change; the rollback restores
		// the cleared flag.
		return p, nil
	}
	switch {
	case m.DeprecatedAt != nil:
		return Promotion{}, fmt.Errorf("registry.SetDefault %s (deprecated at %s): %w",
			modelID, m.DeprecatedAt.UTC().Format(time.RFC3339), ErrModelDeprecated)
	case m.Dimension <= 0:
		return Promotion{}, fmt.Errorf("registry.SetDefault %s: %w", modelID, ErrModelNotMeasured)
	case p.Coverage+coverageEpsilon < minCoverage:
		return Promotion{}, &CoverageError{ModelID: modelID, Coverage: p.Coverage, MinCoverage: minCoverage}
	}

	if _, err := tx.ExecContext(
		ctx,
		s.q(`UPDATE embedding_models SET is_default = 1 WHERE model_id = ?`), modelID,
	); err != nil {
		return Promotion{}, fmt.Errorf("registry.SetDefault: mark default: %w", err)
	}
	p.Changed = true
	if !commit {
		return p, nil
	}
	if err := tx.Commit(); err != nil {
		return Promotion{}, fmt.Errorf("registry.SetDefault: commit: %w", err)
	}
	return p, nil
}

// Deprecate schedules modelID's retirement at when (zero means now): from
// then on Populating leaves it out, so ingest stops writing its vectors,
// and Purge may remove it once the grace period has passed. Deprecating
// again reschedules. The model row and its vectors stay until Purge.
//
// It refuses the default model (ErrIsDefault) in the same statement that
// stamps the date, so a concurrent set-default cannot slip a deprecated
// model into the default slot: promote a successor first.
func (s *Store) Deprecate(ctx context.Context, modelID string, when time.Time) error {
	return s.deprecate(ctx, modelID, when, true)
}

// PlanDeprecate runs Deprecate's guards, then rolls back.
func (s *Store) PlanDeprecate(ctx context.Context, modelID string, when time.Time) error {
	return s.deprecate(ctx, modelID, when, false)
}

func (s *Store) deprecate(ctx context.Context, modelID string, when time.Time, commit bool) error {
	if when.IsZero() {
		when = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registry.Deprecate: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(
		ctx,
		s.q(`UPDATE embedding_models SET deprecated_at = ? WHERE model_id = ? AND is_default = 0`),
		when.UTC().Format(time.RFC3339), modelID,
	)
	if err != nil {
		return fmt.Errorf("registry.Deprecate: update: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("registry.Deprecate: rows affected: %w", err)
	}
	if rows == 0 {
		if _, err := s.get(ctx, tx, modelID); err != nil {
			if errors.Is(err, ErrModelNotFound) {
				return fmt.Errorf("registry.Deprecate %s: %w", modelID, ErrModelNotFound)
			}
			return fmt.Errorf("registry.Deprecate %s: %w", modelID, err)
		}
		return fmt.Errorf("registry.Deprecate %s: %w", modelID, ErrIsDefault)
	}
	if !commit {
		return nil
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("registry.Deprecate: commit: %w", err)
	}
	return nil
}

// Purged reports a purge.
type Purged struct {
	ModelID      string
	DeprecatedAt time.Time
	// EligibleAt is DeprecatedAt plus the grace period.
	EligibleAt time.Time
	// Rows is the number of embedding rows (chunks) removed, or that a
	// plan would remove.
	Rows int64
}

// Purge removes a deprecated model for good: its embedding rows, its
// per-model index and its index signature (EmbeddingStore.PurgeModel), then
// its registry row, so nothing rebuilds the index on the next open (ADR-071
// amendment 2026-09-26, "purge becomes EmbeddingStore.PurgeModel plus
// removal of the registry row").
//
// It refuses, deleting nothing, when the model is unknown, the default,
// not deprecated, or deprecated less than grace before now. A purge that
// stops between the two steps leaves a registry row without rows or
// index; running it again completes it.
func (s *Store) Purge(ctx context.Context, store storage.EmbeddingStore, modelID string, grace time.Duration, now time.Time) (Purged, error) {
	p, err := s.PlanPurge(ctx, modelID, grace, now)
	if err != nil {
		return Purged{}, err
	}
	if err := store.PurgeModel(ctx, modelID); err != nil {
		return Purged{}, fmt.Errorf("registry.Purge %s: %w", modelID, err)
	}
	if _, err := s.db.ExecContext(
		ctx,
		s.q(`DELETE FROM embedding_models WHERE model_id = ? AND is_default = 0 AND deprecated_at IS NOT NULL`),
		modelID,
	); err != nil {
		return Purged{}, fmt.Errorf("registry.Purge %s: delete registry row: %w", modelID, err)
	}
	return p, nil
}

// PlanPurge runs Purge's guards and counts the rows it would remove.
func (s *Store) PlanPurge(ctx context.Context, modelID string, grace time.Duration, now time.Time) (Purged, error) {
	if grace < 0 {
		return Purged{}, fmt.Errorf("registry.Purge: grace period %s is negative", grace)
	}
	m, err := s.Get(ctx, modelID)
	if err != nil {
		if errors.Is(err, ErrModelNotFound) {
			return Purged{}, fmt.Errorf("registry.Purge %s: %w", modelID, ErrModelNotFound)
		}
		return Purged{}, fmt.Errorf("registry.Purge %s: %w", modelID, err)
	}
	if m.IsDefault {
		return Purged{}, fmt.Errorf("registry.Purge %s: %w", modelID, ErrIsDefault)
	}
	if m.DeprecatedAt == nil {
		return Purged{}, fmt.Errorf("registry.Purge %s: %w", modelID, ErrNotDeprecated)
	}
	p := Purged{ModelID: modelID, DeprecatedAt: *m.DeprecatedAt, EligibleAt: m.DeprecatedAt.Add(grace)}
	if now.Before(p.EligibleAt) {
		return Purged{}, &GraceError{ModelID: modelID, DeprecatedAt: p.DeprecatedAt, EligibleAt: p.EligibleAt}
	}
	if err := s.db.QueryRowContext(
		ctx,
		s.q(`SELECT COUNT(*) FROM embeddings WHERE model_id = ?`), modelID,
	).Scan(&p.Rows); err != nil {
		return Purged{}, fmt.Errorf("registry.Purge %s: count rows: %w", modelID, err)
	}
	return p, nil
}

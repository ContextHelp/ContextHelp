// Package registry implements the embedding-model registry of ADR-071: a
// layer over the `embedding_models` table. Register, List, Get and the
// coverage report serve `ctxt embeddings register` and `list`; the
// lifecycle operations SetDefault (coverage-guarded, atomic), Deprecate and
// Purge live in lifecycle.go.
//
// The registry is database-backed, not mocked. Every test operates against a
// real sqlite driver — see registry_test.go.
package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// Provider hints used at Register time. Not enforced server-side; the registry
// stores whatever provider string the operator supplies.
const (
	ProviderOpenAI = "openai"
	ProviderOllama = "ollama"
	ProviderVoyage = "voyage"
)

// Model represents one row in the embedding_models table.
//
// Field names are stable JSON identifiers used by the registry layer.
// The CLI surface for `ctxt embeddings list` is pinned by the eva contract
// at `contracts/embeddings-list.eva.yaml` (T-0586) and is constructed from
// ListWithCoverage rather than this struct directly — `coverage` is a
// computed value, not a column.
type Model struct {
	ModelID      string     `json:"model_id"`
	Provider     string     `json:"provider"`
	Dimension    int        `json:"dimension"`
	IsDefault    bool       `json:"is_default"`
	RegisteredAt time.Time  `json:"registered_at"`
	DeprecatedAt *time.Time `json:"deprecated_at,omitempty"`
	ConfigJSON   string     `json:"config_json"`
}

// ErrModelNotFound is returned by Get / SetDefault / Deprecate when the
// requested model_id is not present in the registry.
var ErrModelNotFound = errors.New("embedding model not found")

// ErrModelAlreadyRegistered is returned by Register when the model_id already
// exists. Registration is the operator-driven add path; replacing config on an
// existing model_id is a separate (unimplemented Phase-1) operation.
var ErrModelAlreadyRegistered = errors.New("embedding model already registered")

// Store is the registry's database handle. Construct via New(driver.DB())
// for SQLite or NewFor(driver.DB(), driver.SQLDialect()) to match the
// backend explicitly.
type Store struct {
	db       *sql.DB
	postgres bool
	// maxIndexableDim is the backend's ANN-indexable dimension ceiling:
	// sqlite-vec's vec0 column limit on SQLite, pgvector HNSW on Postgres.
	maxIndexableDim int
}

// New returns a SQLite-dialect Store wired to db. The caller owns the
// lifetime of db; the registry never closes it.
func New(db *sql.DB) *Store {
	return NewFor(db, "sqlite")
}

// NewFor returns a Store speaking the given SQL dialect ("sqlite" or
// "postgres", matching Driver.SQLDialect()). The Postgres dialect rebinds
// placeholders. Each dialect enforces its per-model index dimension ceiling
// at Register time (8192 for sqlite-vec vec0, 2000 for pgvector HNSW), so an
// un-indexable model fails loudly at registration, not at first query.
func NewFor(db *sql.DB, dialect string) *Store {
	s := &Store{db: db, maxIndexableDim: storage.SQLiteVecMaxDimension}
	if dialect == "postgres" {
		s.postgres = true
		s.maxIndexableDim = indexsig.PostgresHNSWMaxDimension
	}
	return s
}

// q adapts a ?-placeholder query to the store's dialect.
func (s *Store) q(query string) string {
	if !s.postgres {
		return query
	}
	var b strings.Builder
	n := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			fmt.Fprintf(&b, "$%d", n)
			n++
		} else {
			b.WriteByte(query[i])
		}
	}
	return b.String()
}

// Register inserts a new model row. The model_id must pass
// storage.ValidateEmbeddingModelID (it is embedded as a literal in per-model
// index DDL). ConfigJSON defaults to "{}" when empty.
// The first model registered may be marked is_default = 1 by passing
// makeDefault=true; subsequent registrations must call SetDefault to flip the
// active model. Returns ErrModelAlreadyRegistered if model_id already exists.
func (s *Store) Register(ctx context.Context, m Model, makeDefault bool) error {
	if m.ModelID == "" {
		return fmt.Errorf("registry.Register: model_id is required")
	}
	if err := storage.ValidateEmbeddingModelID(m.ModelID); err != nil {
		return fmt.Errorf("registry.Register: %w", err)
	}
	if m.Dimension < 0 {
		return fmt.Errorf("registry.Register: dimension must be non-negative")
	}
	if m.Dimension > s.maxIndexableDim {
		if s.postgres {
			return fmt.Errorf(
				"registry.Register: model %s has dimension %d, above this backend's ANN-indexable ceiling of %d (pgvector HNSW; halfvec extends to %d but is not wired) — register a smaller-dimension model or use the sqlite backend",
				m.ModelID, m.Dimension, s.maxIndexableDim, indexsig.PostgresHalfvecMaxDimension,
			)
		}
		return fmt.Errorf(
			"registry.Register: model %s has dimension %d, above this backend's ANN-indexable ceiling of %d (sqlite-vec vec0) — register a smaller-dimension model",
			m.ModelID, m.Dimension, s.maxIndexableDim,
		)
	}
	if m.ConfigJSON == "" {
		m.ConfigJSON = "{}"
	}
	if m.RegisteredAt.IsZero() {
		m.RegisteredAt = time.Now().UTC()
	}

	defFlag := 0
	if makeDefault {
		defFlag = 1
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registry.Register: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if makeDefault {
		// Clear any existing default — the partial unique index forbids two
		// is_default = 1 rows. We honour the operator's makeDefault intent
		// even if a different default already exists, mirroring the
		// SetDefault semantics. (For Phase 1 this is the only way to swap
		// a default at register time without going through SetDefault.)
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE embedding_models SET is_default = 0 WHERE is_default = 1`,
		); err != nil {
			return fmt.Errorf("registry.Register: clear existing default: %w", err)
		}
	}

	res, err := tx.ExecContext(
		ctx, s.q(`
		INSERT INTO embedding_models
			(model_id, provider, dimension, is_default, registered_at, config_json)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(model_id) DO NOTHING`),
		m.ModelID, m.Provider, m.Dimension, defFlag,
		m.RegisteredAt.UTC().Format(time.RFC3339), m.ConfigJSON,
	)
	if err != nil {
		return fmt.Errorf("registry.Register: insert: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("registry.Register: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrModelAlreadyRegistered
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("registry.Register: commit: %w", err)
	}
	return nil
}

// List returns every registered model, ordered by registered_at ASC for
// stable CLI output. Deprecated models are included; callers filter when
// they need only-active. There is no pagination — the registry is
// expected to hold tens of rows at most.
func (s *Store) List(ctx context.Context) ([]Model, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT model_id, provider, dimension, is_default,
		       registered_at, deprecated_at, config_json
		  FROM embedding_models
		 ORDER BY registered_at ASC, model_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("registry.List: query: %w", err)
	}
	defer rows.Close()

	var out []Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ModelWithCoverage pairs a registered model with its corpus-coverage fraction
// (0.0 .. 1.0): the fraction of distinct objects that have at least one
// embedding row under this model_id. The CLI surface for `ctxt embeddings
// list` requires coverage (eva contract `contracts/embeddings-list.eva.yaml`).
type ModelWithCoverage struct {
	Model
	Coverage float64 `json:"coverage"`
}

// ListWithCoverage returns ListWithCoverage in registered_at ASC order,
// matching List, plus a per-row corpus-coverage fraction. Coverage is
// COUNT(DISTINCT embeddings.object_id WHERE model_id = m) / COUNT(objects).
// When the objects table is empty, coverage is reported as 1.0 (vacuous
// coverage on an empty corpus — the operator-useful interpretation).
func (s *Store) ListWithCoverage(ctx context.Context) ([]ModelWithCoverage, error) {
	models, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, nil
	}

	total, err := objectCount(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("registry.ListWithCoverage: %w", err)
	}

	out := make([]ModelWithCoverage, 0, len(models))
	for _, m := range models {
		coverage, err := s.coverage(ctx, s.db, m.ModelID, total)
		if err != nil {
			return nil, fmt.Errorf("registry.ListWithCoverage: %w", err)
		}
		out = append(out, ModelWithCoverage{Model: m, Coverage: coverage})
	}
	return out, nil
}

// queryer is what the read helpers need: a *sql.DB, or a *sql.Tx when the
// read must see the transaction's own writes and locks.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// objectCount is the coverage denominator: every object in the corpus.
func objectCount(ctx context.Context, q queryer) (int64, error) {
	var total int64
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM objects`).Scan(&total); err != nil {
		return 0, fmt.Errorf("total objects: %w", err)
	}
	return total, nil
}

// coverage is the fraction of total objects that have at least one row
// under modelID; 1.0 on an empty corpus. It is the one coverage definition:
// `ctxt embeddings list` reports it and the set-default guard compares it.
func (s *Store) coverage(ctx context.Context, q queryer, modelID string, total int64) (float64, error) {
	if total == 0 {
		return 1.0, nil
	}
	var covered int64
	if err := q.QueryRowContext(
		ctx,
		s.q(`SELECT COUNT(DISTINCT object_id) FROM embeddings WHERE model_id = ?`),
		modelID,
	).Scan(&covered); err != nil {
		return 0, fmt.Errorf("covered count for %s: %w", modelID, err)
	}
	return float64(covered) / float64(total), nil
}

// Get returns the model row for modelID. Returns ErrModelNotFound when no row
// matches.
func (s *Store) Get(ctx context.Context, modelID string) (*Model, error) {
	return s.get(ctx, s.db, modelID)
}

func (s *Store) get(ctx context.Context, q queryer, modelID string) (*Model, error) {
	row := q.QueryRowContext(ctx, s.q(`
		SELECT model_id, provider, dimension, is_default,
		       registered_at, deprecated_at, config_json
		  FROM embedding_models
		 WHERE model_id = ?`), modelID)
	m, err := scanModel(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModelNotFound
		}
		return nil, err
	}
	return &m, nil
}

// rowScanner is the minimal interface implemented by *sql.Row and *sql.Rows
// so scanModel can serve both.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanModel(r rowScanner) (Model, error) {
	var (
		m            Model
		registeredAt any
		deprecatedAt any
		isDefault    int
	)
	if err := r.Scan(
		&m.ModelID, &m.Provider, &m.Dimension, &isDefault,
		&registeredAt, &deprecatedAt, &m.ConfigJSON,
	); err != nil {
		return Model{}, err
	}
	m.IsDefault = isDefault == 1
	// Timestamps are TEXT (RFC3339) on SQLite, TIMESTAMPTZ (time.Time) on
	// Postgres; normalize both.
	if t, ok := scanTime(registeredAt); ok {
		m.RegisteredAt = t
	}
	if t, ok := scanTime(deprecatedAt); ok {
		m.DeprecatedAt = &t
	}
	return m, nil
}

// scanTime normalizes a scanned timestamp column: time.Time from Postgres
// TIMESTAMPTZ, RFC3339 text/bytes from SQLite. Returns ok=false for NULL,
// empty, or unparsable values.
func scanTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t.UTC(), true
	case string:
		if t == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return parsed.UTC(), true
		}
	case []byte:
		if len(t) == 0 {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339, string(t)); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

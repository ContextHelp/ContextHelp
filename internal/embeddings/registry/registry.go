// Package registry implements the embedding-model registry described in
// ADR-071 Phase 1 (T-0582). It is a thin CRUD layer over the
// `embedding_models` table created by migration 032: Register, List, Get,
// SetDefault, Deprecate. Coverage / recall guards on SetDefault are out of
// scope for Phase 1 (Phase 3, T-0584).
//
// The registry is database-backed, not mocked. Every test operates against a
// real sqlite driver — see registry_test.go.
package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Provider hints used at Register time. Not enforced server-side; the registry
// stores whatever provider string the operator supplies.
const (
	ProviderOpenAI    = "openai"
	ProviderOllama    = "ollama"
	ProviderVoyage    = "voyage"
	ProviderLegacy    = "legacy-blob"
)

// Model represents one row in the embedding_models table.
//
// Field names are stable JSON identifiers — they double as the operator-facing
// CLI shape (`ctxt embeddings list --format=json`). T-0586 will add the eva
// contract that pins these names; Phase 1 documents them here.
type Model struct {
	ModelID       string     `json:"model_id"`
	Provider      string     `json:"provider"`
	Dimension     int        `json:"dimension"`
	IsDefault     bool       `json:"is_default"`
	RegisteredAt  time.Time  `json:"registered_at"`
	DeprecatedAt  *time.Time `json:"deprecated_at,omitempty"`
	ConfigJSON    string     `json:"config_json"`
}

// ErrModelNotFound is returned by Get / SetDefault / Deprecate when the
// requested model_id is not present in the registry.
var ErrModelNotFound = errors.New("embedding model not found")

// ErrModelAlreadyRegistered is returned by Register when the model_id already
// exists. Registration is the operator-driven add path; replacing config on an
// existing model_id is a separate (unimplemented Phase-1) operation.
var ErrModelAlreadyRegistered = errors.New("embedding model already registered")

// Store is the registry's database handle. Construct via New(driver.DB()).
type Store struct {
	db *sql.DB
}

// New returns a Store wired to db. The caller owns the lifetime of db; the
// registry never closes it.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Register inserts a new model row. ConfigJSON defaults to "{}" when empty.
// The first model registered may be marked is_default = 1 by passing
// makeDefault=true; subsequent registrations must call SetDefault to flip the
// active model. Returns ErrModelAlreadyRegistered if model_id already exists.
func (s *Store) Register(ctx context.Context, m Model, makeDefault bool) error {
	if m.ModelID == "" {
		return fmt.Errorf("registry.Register: model_id is required")
	}
	if m.Dimension < 0 {
		return fmt.Errorf("registry.Register: dimension must be non-negative")
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
		if _, err := tx.ExecContext(ctx,
			`UPDATE embedding_models SET is_default = 0 WHERE is_default = 1`,
		); err != nil {
			return fmt.Errorf("registry.Register: clear existing default: %w", err)
		}
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO embedding_models
			(model_id, provider, dimension, is_default, registered_at, config_json)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(model_id) DO NOTHING`,
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

// Get returns the model row for modelID. Returns ErrModelNotFound when no row
// matches.
func (s *Store) Get(ctx context.Context, modelID string) (*Model, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT model_id, provider, dimension, is_default,
		       registered_at, deprecated_at, config_json
		  FROM embedding_models
		 WHERE model_id = ?`, modelID)
	m, err := scanModel(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModelNotFound
		}
		return nil, err
	}
	return &m, nil
}

// SetDefault atomically clears the existing is_default = 1 row and marks
// modelID as the new default. Returns ErrModelNotFound when modelID is not
// registered.
//
// Phase 1 has no coverage / recall guards — those land in Phase 3 (T-0584).
// Operators are expected to know what they're doing; the CLI surface should
// surface a confirmation prompt before invoking this in human contexts.
func (s *Store) SetDefault(ctx context.Context, modelID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registry.SetDefault: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Verify target exists before mutating anything.
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM embedding_models WHERE model_id = ?`, modelID,
	).Scan(&n); err != nil {
		return fmt.Errorf("registry.SetDefault: lookup: %w", err)
	}
	if n == 0 {
		return ErrModelNotFound
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE embedding_models SET is_default = 0 WHERE is_default = 1`,
	); err != nil {
		return fmt.Errorf("registry.SetDefault: clear existing default: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE embedding_models SET is_default = 1 WHERE model_id = ?`, modelID,
	); err != nil {
		return fmt.Errorf("registry.SetDefault: mark default: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("registry.SetDefault: commit: %w", err)
	}
	return nil
}

// Deprecate sets the deprecated_at timestamp on modelID. The model row is
// retained — purge is a separate operation (Phase 4, T-0585). Returns
// ErrModelNotFound when modelID is not registered.
//
// Deprecation does not flip is_default; an operator wanting to retire the
// active default must call SetDefault on a successor first. This Phase-1
// behaviour is intentional: the recall-guarded swap belongs to Phase 3.
func (s *Store) Deprecate(ctx context.Context, modelID string, when time.Time) error {
	if when.IsZero() {
		when = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE embedding_models SET deprecated_at = ? WHERE model_id = ?`,
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
		return ErrModelNotFound
	}
	return nil
}

// rowScanner is the minimal interface implemented by *sql.Row and *sql.Rows
// so scanModel can serve both.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanModel(r rowScanner) (Model, error) {
	var (
		m            Model
		registeredAt string
		deprecatedAt sql.NullString
		isDefault    int
	)
	if err := r.Scan(
		&m.ModelID, &m.Provider, &m.Dimension, &isDefault,
		&registeredAt, &deprecatedAt, &m.ConfigJSON,
	); err != nil {
		return Model{}, err
	}
	m.IsDefault = isDefault == 1
	if t, err := time.Parse(time.RFC3339, registeredAt); err == nil {
		m.RegisteredAt = t.UTC()
	}
	if deprecatedAt.Valid && deprecatedAt.String != "" {
		if t, err := time.Parse(time.RFC3339, deprecatedAt.String); err == nil {
			tt := t.UTC()
			m.DeprecatedAt = &tt
		}
	}
	return m, nil
}

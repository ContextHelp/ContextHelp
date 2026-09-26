package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoDefaultModel is returned by Default when no registered model carries
// is_default = 1. The query path degrades to FTS (with a visible notice) and
// ingest writes no vectors until an operator registers and flips a default.
var ErrNoDefaultModel = errors.New("no default embedding model")

// Default returns the model the query path reads (is_default = 1). Callers
// read it per query rather than caching it, so a set-default flip in another
// process takes effect on the next query without an invalidation protocol.
func (s *Store) Default(ctx context.Context) (*Model, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT model_id, provider, dimension, is_default,
		       registered_at, deprecated_at, config_json
		  FROM embedding_models
		 WHERE is_default = 1`)
	m, err := scanModel(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoDefaultModel
		}
		return nil, fmt.Errorf("registry.Default: %w", err)
	}
	return &m, nil
}

// Populating returns the models ingest writes vectors for (ADR-071
// amendment 2026-09-26, replacing the populate_models policy): the default
// plus every registered model whose deprecation is not yet effective at now.
// Models without a measured dimension (0) are excluded; they have no index to
// write into. The default, when present, is first; the rest keep List order.
func (s *Store) Populating(ctx context.Context, now time.Time) ([]Model, error) {
	models, err := s.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("registry.Populating: %w", err)
	}
	var def []Model
	var rest []Model
	for _, m := range models {
		if m.Dimension <= 0 {
			continue
		}
		switch {
		case m.IsDefault:
			def = append(def, m)
		case m.DeprecatedAt == nil || m.DeprecatedAt.After(now):
			rest = append(rest, m)
		}
	}
	return append(def, rest...), nil
}

// ForDriver returns a Store over a storage driver's database, speaking the
// driver's SQL dialect (drivers without SQLDialect are SQLite). One place
// for every caller that holds a storage.StorageDriver rather than a *sql.DB.
func ForDriver(driver any) (*Store, error) {
	d, ok := driver.(interface{ DB() *sql.DB })
	if !ok {
		return nil, fmt.Errorf("registry: storage backend %T exposes no database handle", driver)
	}
	dialect := "sqlite"
	if dd, ok := driver.(interface{ SQLDialect() string }); ok {
		dialect = dd.SQLDialect()
	}
	return NewFor(d.DB(), dialect), nil
}

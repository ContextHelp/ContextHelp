package search

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Engine ties together parsing, compilation, and storage to execute RSQL queries.
type Engine struct {
	store   storage.StorageDriver
	dialect Dialect
}

// NewEngine creates a new search engine backed by the given storage driver.
// The SQL dialect is auto-detected via DialectFor; use NewEngineWithDialect to
// override.
func NewEngine(store storage.StorageDriver) *Engine {
	return &Engine{store: store, dialect: DialectFor(store)}
}

// NewEngineWithDialect creates an engine with an explicit dialect override.
// Use this in tests or when auto-detection is insufficient.
func NewEngineWithDialect(store storage.StorageDriver, d Dialect) *Engine {
	return &Engine{store: store, dialect: d}
}

// Search parses an RSQL query, compiles it to SQL, and executes it.
// profileID, when non-empty, restricts results to objects owned by that profile.
func (e *Engine) Search(ctx context.Context, query string, limit, offset int, profileID ...string) ([]*storage.KnowledgeObject, int, error) {
	ast, err := Parse(query)
	if err != nil {
		return nil, 0, fmt.Errorf("parse error: %w", err)
	}

	where, args, err := buildWhere(e.dialect, ast, profileID...)
	if err != nil {
		return nil, 0, fmt.Errorf("compile error: %w", err)
	}

	return e.store.Objects().ListBySQL(ctx, where, args, limit, offset)
}

// buildWhere compiles the AST into a WHERE clause (with optional profile scope)
// for the given dialect. A single rebind pass is applied at the end so that
// the profile_id placeholder and the RSQL placeholders share the same $N sequence
// on Postgres.
func buildWhere(d Dialect, ast Node, profileID ...string) (string, []any, error) {
	// Always compile to ?-form first.
	rawSQL, rawArgs, err := compileNode(d, ast)
	if err != nil {
		return "", nil, err
	}

	var where string
	var args []any

	if len(profileID) > 0 && profileID[0] != "" {
		scope := "profile_id = ?"
		if rawSQL != "" {
			where = scope + " AND (" + rawSQL + ")"
		} else {
			where = scope
		}
		args = append([]any{profileID[0]}, rawArgs...)
	} else {
		where = rawSQL
		args = rawArgs
	}

	// Single rebind pass for Postgres.
	if d == DialectPostgres {
		where = rebindPostgres(where)
	}
	return where, args, nil
}

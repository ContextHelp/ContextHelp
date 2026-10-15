// Package search parses RSQL queries, compiles them to the storage driver's
// SQL dialect, and runs them; it also holds the query-mode, decomposition and
// alias-expansion rules applied before a query reaches the store.
package search

import (
	"context"
	"errors"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrInvalidQuery marks a query the engine refused before reading the
// store: it did not parse, or it named a field or operator the dialect
// cannot compile. errors.Is(err, ErrInvalidQuery) holds for both, so a
// transport answers the caller's mistake (400, InvalidArgument) instead
// of a store failure.
var ErrInvalidQuery = errors.New("invalid query")

// invalidQuery is the error Search returns for a refused query. It keeps
// the "<stage> error: <cause>" text callers see, and reports
// ErrInvalidQuery through Is without embedding its text.
type invalidQuery struct {
	stage string
	cause error
}

func (e *invalidQuery) Error() string { return e.stage + " error: " + e.cause.Error() }

func (e *invalidQuery) Unwrap() error { return e.cause }

func (e *invalidQuery) Is(target error) bool { return target == ErrInvalidQuery }

// Engine ties together parsing and compilation to execute RSQL queries.
// It holds no store: every Search names the object store it reads, so a
// caller with only a viewer-scoped driver cannot search past it
// (ADR-080 §4).
type Engine struct {
	dialect Dialect
}

// NewEngine creates a search engine compiling for the given driver's SQL
// dialect, auto-detected via DialectFor; use NewEngineWithDialect to
// override. The driver is consulted for its dialect only.
func NewEngine(store storage.StorageDriver) *Engine {
	return &Engine{dialect: DialectFor(store)}
}

// NewEngineWithDialect creates an engine with an explicit dialect.
// Use this in tests or when auto-detection is insufficient.
func NewEngineWithDialect(d Dialect) *Engine {
	return &Engine{dialect: d}
}

// Search parses an RSQL query, compiles it to SQL, and executes it through
// objects.ListBySQL with a List filter, so it reads like a List: only active
// objects, since the query language has no status field and none is asked
// for. profileID, when non-empty, restricts results to objects owned by
// that profile.
func (e *Engine) Search(ctx context.Context, objects storage.ObjectStore, query string, limit, offset int, profileID ...string) ([]*storage.KnowledgeObject, int, error) {
	ast, err := Parse(query)
	if err != nil {
		return nil, 0, &invalidQuery{stage: "parse", cause: err}
	}

	where, args, err := CompileFor(e.dialect, ast)
	if err != nil {
		return nil, 0, &invalidQuery{stage: "compile", cause: err}
	}

	filter := storage.ObjectFilter{Limit: limit, Offset: offset}
	if len(profileID) > 0 {
		filter.ProfileID = profileID[0]
	}
	return objects.ListBySQL(ctx, where, args, filter)
}

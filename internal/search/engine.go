package search

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Engine ties together parsing, compilation, and storage to execute RSQL queries.
type Engine struct {
	store storage.StorageDriver
}

// NewEngine creates a new search engine backed by the given storage driver.
func NewEngine(store storage.StorageDriver) *Engine {
	return &Engine{store: store}
}

// Search parses an RSQL query, compiles it to SQL, and executes it.
func (e *Engine) Search(ctx context.Context, query string, limit, offset int) ([]*storage.KnowledgeObject, int, error) {
	ast, err := Parse(query)
	if err != nil {
		return nil, 0, fmt.Errorf("parse error: %w", err)
	}

	where, args, err := Compile(ast)
	if err != nil {
		return nil, 0, fmt.Errorf("compile error: %w", err)
	}

	return e.store.Objects().ListBySQL(ctx, where, args, limit, offset)
}

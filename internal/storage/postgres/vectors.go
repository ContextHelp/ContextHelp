package postgres

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// vectorStoreStub satisfies storage.VectorStore for the postgres driver.
// pgvector integration is tracked as future work (see docs/plans/P-010).
type vectorStoreStub struct{}

func (s *vectorStoreStub) Upsert(_ context.Context, _ string, _ []float32) error {
	return fmt.Errorf("vector store: postgres backend not yet implemented")
}

func (s *vectorStoreStub) Search(_ context.Context, _ []float32, _ int) ([]storage.VectorHit, error) {
	return nil, fmt.Errorf("vector store: postgres backend not yet implemented")
}

func (s *vectorStoreStub) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("vector store: postgres backend not yet implemented")
}

func (s *vectorStoreStub) Count(_ context.Context) (int, error) {
	return 0, fmt.Errorf("vector store: postgres backend not yet implemented")
}

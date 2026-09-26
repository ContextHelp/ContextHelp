package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EmbeddingStore is the SQLite storage.EmbeddingStore: canonical rows in the
// embeddings table plus one vec0 table per registered model (ADR-071,
// amendment 2026-09-26). Contract stub: every method reports
// errors.ErrUnsupported until the per-model index lands.
type EmbeddingStore struct {
	db *sql.DB
}

var _ storage.EmbeddingStore = (*EmbeddingStore)(nil)

// Embeddings returns the per-model embedding store.
func (d *Driver) Embeddings() storage.EmbeddingStore { return &EmbeddingStore{db: d.db} }

func errEmbeddingsUnsupported(op string) error {
	return fmt.Errorf("sqlite embeddings %s: %w", op, errors.ErrUnsupported)
}

func (s *EmbeddingStore) EnsureIndex(context.Context, storage.EmbeddingModelSpec) error {
	return errEmbeddingsUnsupported("ensure index")
}

func (s *EmbeddingStore) Put(context.Context, string, []storage.ObjectVector) error {
	return errEmbeddingsUnsupported("put")
}

func (s *EmbeddingStore) Get(context.Context, string, string) ([]storage.ObjectVector, error) {
	return nil, errEmbeddingsUnsupported("get")
}

func (s *EmbeddingStore) Search(context.Context, storage.VectorQuery) ([]storage.EmbeddingHit, error) {
	return nil, errEmbeddingsUnsupported("search")
}

func (s *EmbeddingStore) ListMissing(context.Context, string, string, int) ([]string, error) {
	return nil, errEmbeddingsUnsupported("list missing")
}

func (s *EmbeddingStore) PurgeModel(context.Context, string) error {
	return errEmbeddingsUnsupported("purge model")
}

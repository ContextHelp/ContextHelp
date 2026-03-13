package stub

import (
	"context"
	"fmt"
	"io"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Store is a no-op BlobStore for testing.
type Store struct{}

func New() *Store { return &Store{} }

func (s *Store) Put(_ context.Context, _ string, _ io.Reader, _ storage.BlobMeta) error {
	return nil
}

func (s *Store) Get(_ context.Context, key string) (io.ReadCloser, storage.BlobMeta, error) {
	return nil, storage.BlobMeta{}, fmt.Errorf("blob %q not found (stub)", key)
}

func (s *Store) Delete(_ context.Context, _ string) error { return nil }

func (s *Store) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

func (s *Store) List(_ context.Context, _ string) ([]storage.BlobInfo, error) { return nil, nil }

func (s *Store) URL(_ context.Context, key string) (string, error) {
	return "", fmt.Errorf("blob %q: URL not available (stub)", key)
}

package storageutil

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// Open creates the StorageDriver of type typ at path with the blob store
// that blobCfg (storage.blob) configures. Every command that opens the
// configured store goes through it, so content above
// storage.blob.threshold always reaches a real blob store.
func Open(typ, path string, blobCfg config.BlobConfig) (storage.StorageDriver, error) {
	blobs, err := blob.New(blobCfg)
	if err != nil {
		return nil, fmt.Errorf("open blob store: %w", err)
	}
	return NewDriver(typ, path, blobs)
}

// NewDriver creates a StorageDriver from a type string and path that
// writes externalized content to blobs. blobs must not be nil.
func NewDriver(typ, path string, blobs storage.BlobStore) (storage.StorageDriver, error) {
	switch typ {
	case "sqlite":
		return sqlite.New(path, blobs)
	case "postgres":
		return postgres.New(path, blobs)
	default:
		return nil, fmt.Errorf("unknown storage type: %s", typ)
	}
}

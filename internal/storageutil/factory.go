package storageutil

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// OpenOption configures the driver Open and NewDriver build.
type OpenOption func(*openOptions)

type openOptions struct {
	skipMigrateSnapshot bool
}

// WithMigrateSnapshot applies storage.migrate_snapshot: whether opening
// a SQLite database with pending schema migrations first snapshots it
// into backups/ beside the database file. On unless set false. Postgres
// ignores it.
func WithMigrateSnapshot(enabled bool) OpenOption {
	return func(o *openOptions) { o.skipMigrateSnapshot = !enabled }
}

// Open creates the StorageDriver of type typ at path with the blob store
// that blobCfg (storage.blob) configures. Every command that opens the
// configured store goes through it, so content above
// storage.blob.threshold always reaches a real blob store.
func Open(typ, path string, blobCfg config.BlobConfig, opts ...OpenOption) (storage.StorageDriver, error) {
	blobs, err := blob.New(blobCfg)
	if err != nil {
		return nil, fmt.Errorf("open blob store: %w", err)
	}
	return NewDriver(typ, path, blobs, opts...)
}

// NewDriver creates a StorageDriver from a type string and path that
// writes externalized content to blobs. blobs must not be nil.
func NewDriver(typ, path string, blobs storage.BlobStore, opts ...OpenOption) (storage.StorageDriver, error) {
	var o openOptions
	for _, opt := range opts {
		opt(&o)
	}
	switch typ {
	case "sqlite":
		return sqlite.New(path, blobs, sqlite.WithMigrateSnapshot(!o.skipMigrateSnapshot))
	case "postgres":
		return postgres.New(path, blobs)
	default:
		return nil, fmt.Errorf("unknown storage type: %s", typ)
	}
}

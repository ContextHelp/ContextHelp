// Package storageutil opens the configured storage driver and blob store,
// and carries shared storage helpers: content hashing, SQLite snapshots and
// test fixtures.
package storageutil

import (
	"context"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// Storage types the factory opens.
const (
	typeSQLite   = "sqlite"
	typePostgres = "postgres"
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
	case typeSQLite:
		return sqlite.New(path, blobs, sqlite.WithMigrateSnapshot(!o.skipMigrateSnapshot))
	case typePostgres:
		return postgres.New(path, blobs)
	default:
		return nil, fmt.Errorf("unknown storage type: %s", typ)
	}
}

// ErrNoSchema is returned by OpenReadOnly for a store with no data to
// read: no database yet, or one with no tables of its own. A read never
// creates one.
var ErrNoSchema = errors.New("database has no schema yet")

// ErrSchemaMismatch is returned by OpenReadOnly when the store's schema
// version is not the one this binary migrates to: a read-only handle
// cannot migrate it, and reading another version's tables is a guess.
var ErrSchemaMismatch = errors.New("database schema version does not match this binary")

// OpenReadOnly opens the existing store of type typ at path read-only,
// with the blob store blobCfg configures. Nothing is migrated or
// snapshotted, and no statement on the handle can write: SQLite opens
// mode=ro with query_only, Postgres with default_transaction_read_only.
// Calling Init on the result fails. A missing or empty store fails with
// ErrNoSchema, and one at another schema version with ErrSchemaMismatch.
func OpenReadOnly(ctx context.Context, typ, path string, blobCfg config.BlobConfig) (storage.StorageDriver, error) {
	st, err := CheckSchema(ctx, typ, path)
	if err != nil {
		return nil, err
	}
	switch {
	case st.New:
		return nil, fmt.Errorf("%w: %s", ErrNoSchema, path)
	case st.Current != st.Latest:
		return nil, fmt.Errorf("%w: %s is at version %d, this binary expects %d", ErrSchemaMismatch, path, st.Current, st.Latest)
	}
	blobs, err := blob.New(blobCfg)
	if err != nil {
		return nil, fmt.Errorf("open blob store: %w", err)
	}
	switch typ {
	case typeSQLite:
		return sqlite.NewReadOnly(path, blobs)
	case typePostgres:
		return postgres.NewReadOnly(path, blobs)
	default:
		return nil, fmt.Errorf("unknown storage type: %s", typ)
	}
}

// CheckSchema reports the schema version of the store of type typ at
// path against the latest this binary migrates to, without opening the
// store normally: nothing is migrated or snapshotted.
func CheckSchema(ctx context.Context, typ, path string) (storage.SchemaStatus, error) {
	switch typ {
	case typeSQLite:
		return sqlite.CheckSchema(ctx, path)
	case typePostgres:
		return postgres.CheckSchema(ctx, path)
	default:
		return storage.SchemaStatus{}, fmt.Errorf("unknown storage type: %s", typ)
	}
}

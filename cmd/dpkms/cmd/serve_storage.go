package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/server/stack"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// serveStorageTarget returns the storage type and path serve opens.
// Storage defaults when the config leaves storage.type or storage.path
// unset.
const (
	storageTypeSQLite = "sqlite"
	fallbackDBPath    = "dpkms.db"
)

func serveStorageTarget(c *config.Config) (typ, path string) {
	typ, path = c.Storage.Type, c.Storage.Path
	if typ == "" {
		typ = storageTypeSQLite
	}
	if path == "" {
		path = fallbackDBPath
	}
	return typ, path
}

// openServeStorage opens and migrates the storage driver serve runs on.
func openServeStorage(ctx context.Context, c *config.Config) (storage.StorageDriver, error) {
	storageType, storagePath := serveStorageTarget(c)
	driver, err := storageutil.Open(storageType, storagePath, c.Storage.Blob,
		storageutil.WithMigrateSnapshot(c.Storage.MigrateSnapshot))
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}
	if err := driver.Init(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("init storage: %w", err)
	}
	return driver, nil
}

// serveLockBudget is how long serve keeps retrying a held database lock
// before it gives up. It covers a restart that overlaps the previous
// serve's shutdown drain; a second serve on a live database still fails
// once the budget runs out, naming the holder.
var serveLockBudget = 10 * time.Second

// acquireServeLock takes the database lock serve holds, for its whole
// lifetime, on the SQLite database it opens: a second serve on that
// database, or a restore over it, fails within serveLockBudget instead
// of both writing to it. The pidfile cannot say this: it names a port,
// and writing it is best effort. Postgres and in-memory SQLite have no
// database file to guard; they get a nil lock and no error.
func acquireServeLock(ctx context.Context, c *config.Config, instance string) (*dblock.Lock, error) {
	typ, path := serveStorageTarget(c)
	if typ != storageTypeSQLite || sqlite.IsMemoryPath(path) {
		return nil, nil
	}
	l, err := dblock.AcquireRetry(ctx, path, dblock.Info{Role: dblock.RoleDaemon, Instance: instance}, serveLockBudget)
	if errors.Is(err, dblock.ErrHeld) {
		return nil, fmt.Errorf("database %s is in use: %w; stop that process first (dpkms ps, dpkms shutdown)", path, err)
	}
	if err != nil {
		return nil, fmt.Errorf("lock database %s: %w", path, err)
	}
	return l, nil
}

// closeServeStack shuts the request stack down within drain, which
// writes the security audit rows still queued (and cancels alert sends
// still running when drain runs out), and only then closes the store
// those rows are written to. Failures are warnings on warn: serve is
// exiting either way.
func closeServeStack(warn io.Writer, st *stack.Stack, store interface{ Close(context.Context) error }, drain time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	if err := st.Shutdown(ctx); err != nil {
		_, _ = fmt.Fprintf(warn, "warning: stack shutdown: %v\n", err)
	}
	if err := store.Close(context.Background()); err != nil {
		_, _ = fmt.Fprintf(warn, "warning: close storage: %v\n", err)
	}
}

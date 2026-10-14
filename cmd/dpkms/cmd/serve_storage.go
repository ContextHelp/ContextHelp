package cmd

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// serveStorageTarget returns the storage type and path serve opens.
func serveStorageTarget(c *config.Config) (typ, path string) {
	typ, path = c.Storage.Type, c.Storage.Path
	if typ == "" {
		typ = "sqlite"
	}
	if path == "" {
		path = "dpkms.db"
	}
	return typ, path
}

// openServeStorage opens and migrates the storage driver serve runs on.
func openServeStorage(ctx context.Context, c *config.Config) (storage.StorageDriver, error) {
	storageType, storagePath := serveStorageTarget(c)
	driver, err := storageutil.Open(storageType, storagePath, c.Storage.Blob)
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}
	if err := driver.Init(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("init storage: %w", err)
	}
	return driver, nil
}

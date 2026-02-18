package storageutil

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// NewTestDriver creates an initialized SQLite driver in a temp directory.
// It registers cleanup to close the driver when the test finishes.
func NewTestDriver(t *testing.T) storage.StorageDriver {
	t.Helper()
	dir := t.TempDir()
	driver, err := sqlite.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("new test driver: %v", err)
	}
	if err := driver.Init(context.Background()); err != nil {
		t.Fatalf("init test driver: %v", err)
	}
	t.Cleanup(func() { driver.Close(context.Background()) })
	return driver
}

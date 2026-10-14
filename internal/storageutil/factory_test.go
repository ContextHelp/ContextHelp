package storageutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

func TestNewDriverSQLite(t *testing.T) {
	dir := t.TempDir()
	driver, err := NewDriver("sqlite", filepath.Join(dir, "test.db"), NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}
	if driver == nil {
		t.Fatal("driver is nil")
	}
}

func TestNewDriverPostgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		t.Skip("POSTGRES_URL not set; skipping postgres driver test")
	}
	driver, err := NewDriver("postgres", dsn, NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("new postgres driver: %v", err)
	}
	if driver == nil {
		t.Fatal("driver is nil")
	}
}

func TestNewDriverUnknown(t *testing.T) {
	_, err := NewDriver("foo", "", NewTestBlobStore(t))
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "unknown storage type") {
		t.Errorf("error: got %q", err)
	}
}

// No constructor hands out a driver without a real blob store: a nil
// store is refused before any database is opened.
func TestNewDriverRequiresBlobStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	if _, err := NewDriver("sqlite", path, nil); !errors.Is(err, sqlite.ErrNoBlobStore) {
		t.Errorf("sqlite without blob store: err = %v, want ErrNoBlobStore", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("sqlite without blob store created %s (stat err %v)", path, err)
	}
	// postgres.New refuses before sql.Open; no server is contacted.
	if _, err := NewDriver("postgres", "postgres://127.0.0.1:1/none", nil); !errors.Is(err, postgres.ErrNoBlobStore) {
		t.Errorf("postgres without blob store: err = %v, want ErrNoBlobStore", err)
	}
}

// Open wires the store storage.blob configures into the driver.
func TestOpenWiresConfiguredBlobStore(t *testing.T) {
	ctx := context.Background()
	blobDir := filepath.Join(t.TempDir(), "blobs")
	driver, err := Open("sqlite", filepath.Join(t.TempDir(), "test.db"), config.BlobConfig{
		Backend: "local",
		Local:   config.BlobLocalConfig{Path: blobDir},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(ctx) })

	if _, ok := driver.Blobs().(*local.Store); !ok {
		t.Fatalf("Blobs() = %T, want *local.Store", driver.Blobs())
	}
	if err := driver.Blobs().Put(ctx, "abcdef", strings.NewReader("kept"), storage.BlobMeta{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := os.Stat(filepath.Join(blobDir, "ab", "cd", "abcdef")); err != nil {
		t.Fatalf("blob not under storage.blob.local.path: %v", err)
	}
}

// An unusable storage.blob is a construction error, never a store that
// drops writes.
func TestOpenRejectsUnusableBlobConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for name, bc := range map[string]config.BlobConfig{
		"stub backend":     {Backend: "stub"},
		"empty local path": {Backend: "local"},
		"unset":            {},
	} {
		if _, err := Open("sqlite", path, bc); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

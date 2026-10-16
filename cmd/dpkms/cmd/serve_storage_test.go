package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

// loadSandboxedServeConfig loads serve's config with every default path
// (data dir, blob dir) rooted under a temp dir.
func loadSandboxedServeConfig(t *testing.T, body string) (*config.Config, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	t.Setenv(config.EnvDataDir, filepath.Join(root, "data"))
	return loadServeConfig(t, body, nil), root
}

// Content above storage.blob.threshold survives serve's storage
// construction: it lands in the configured blob store and reads back
// byte-identical after the driver is reopened.
func TestServeStorage_PersistsBlobsAboveThreshold(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "dpkms.db")
	c, root := loadSandboxedServeConfig(t, "storage:\n  type: sqlite\n  path: "+dbPath+"\n")

	blobDir := c.Storage.Blob.Local.Path
	if c.Storage.Blob.Backend != "local" {
		t.Fatalf("storage.blob.backend default = %q, want local", c.Storage.Blob.Backend)
	}
	if !strings.HasPrefix(blobDir, root+string(filepath.Separator)) {
		t.Fatalf("default storage.blob.local.path %q is outside the sandboxed data dir %q", blobDir, root)
	}

	driver, err := openServeStorage(ctx, c)
	if err != nil {
		t.Fatalf("open serve storage: %v", err)
	}

	threshold := c.Storage.Blob.Threshold
	if threshold <= 0 {
		t.Fatalf("storage.blob.threshold default = %d, want > 0", threshold)
	}
	content := strings.Repeat("0123456789abcdef", int(threshold/16)+64)
	now := time.Now().UTC().Truncate(time.Second)
	draft := &storage.KnowledgeObject{
		ID:          "obj-large",
		Type:        "text",
		Source:      "test://large",
		ContentType: "text/plain",
		RawContent:  content,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	draft, err = steps.NewExternalizer(driver.Blobs(), threshold).Run(ctx, draft)
	if err != nil {
		t.Fatalf("externalize: %v", err)
	}
	if !blob.IsBlobRef(draft.RawContent) {
		t.Fatalf("content of %d bytes not externalized at threshold %d", len(content), threshold)
	}
	if err := driver.Objects().Create(ctx, draft); err != nil {
		t.Fatalf("create object: %v", err)
	}
	if err := driver.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := openServeStorage(ctx, c)
	if err != nil {
		t.Fatalf("reopen serve storage: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close(ctx) })

	got, err := reopened.Objects().Get(ctx, "obj-large")
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	rc, err := blob.Resolve(ctx, reopened.Blobs(), got)
	if err != nil {
		t.Fatalf("resolve %s: %v", got.RawContent, err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if !bytes.Equal(body, []byte(content)) {
		t.Fatalf("blob content differs: got %d bytes, want %d", len(body), len(content))
	}

	key, _ := blob.BlobKey(got.RawContent)
	if _, err := os.Stat(filepath.Join(blobDir, key[:2], key[2:4], key)); err != nil {
		t.Fatalf("blob not on disk under storage.blob.local.path: %v", err)
	}
}

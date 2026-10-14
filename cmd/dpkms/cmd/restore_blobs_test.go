package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// dpkms restore puts backed-up blobs in the storage.blob store the
// config names, where the restored database's refs resolve.
func TestRestoreWritesBlobsToConfiguredStore(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	srcDB := filepath.Join(src, "db.sqlite")
	srcBlobs := filepath.Join(src, "blobs")
	bs, err := local.New(srcBlobs)
	if err != nil {
		t.Fatal(err)
	}
	if err := bs.Put(ctx, "abcdef0123", strings.NewReader("blob body"), storage.BlobMeta{}); err != nil {
		t.Fatal(err)
	}
	drv, err := storageutil.Open("sqlite", srcDB, config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: srcBlobs}})
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	drv.Close(ctx)
	backup, err := service.Backup(ctx, service.BackupOpts{
		DBPath:       srcDB,
		BlobCfg:      config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: srcBlobs}},
		IncludeBlobs: true,
		OutputDir:    filepath.Join(src, "out"),
	})
	if err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	dstBlobs := filepath.Join(t.TempDir(), "configured-blobs")
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{Storage: config.StorageConfig{
		Type: "sqlite",
		Path: filepath.Join(dst, "db.sqlite"),
		Blob: config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: dstBlobs}},
	}}
	if err := restoreCmd.Flags().Set("skip-configs", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoreCmd.Flags().Set("skip-configs", "false") })

	if err := runRestore(restoreCmd, []string{backup.Path}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	restored, err := local.New(dstBlobs)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := restored.Exists(ctx, "abcdef0123"); err != nil || !ok {
		t.Fatalf("blob not in the configured store %s (exists=%v, err=%v)", dstBlobs, ok, err)
	}
}

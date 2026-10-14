package service_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	psteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// seedExternalizedStore builds a data dir whose one object's content
// lives in the local blob store at blobDir, and returns that content.
func seedExternalizedStore(t *testing.T, dbPath, blobDir string) string {
	t.Helper()
	ctx := context.Background()
	bs, err := local.New(blobDir)
	if err != nil {
		t.Fatal(err)
	}
	drv, err := sqlite.New(dbPath, bs)
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("restored blob body ", 20)
	draft := &storage.KnowledgeObject{
		ID: "obj-blob", Type: "text", RawContent: body, Source: "test://blob",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	out, err := psteps.NewExternalizer(bs, 32).Run(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Objects().Create(ctx, out); err != nil {
		t.Fatal(err)
	}
	if err := drv.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return body
}

// readRestored opens the restored database on blobs and resolves the
// seeded object's content.
func readRestored(t *testing.T, dbPath string, blobs storage.BlobStore) string {
	t.Helper()
	ctx := context.Background()
	drv, err := sqlite.New(dbPath, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if err := drv.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer drv.Close(ctx)
	obj, err := drv.Objects().Get(ctx, "obj-blob")
	if err != nil {
		t.Fatal(err)
	}
	if !blob.IsBlobRef(obj.RawContent) {
		t.Fatalf("restored object is not externalized: %.40q", obj.RawContent)
	}
	got, err := blob.Content(ctx, blobs, obj.RawContent)
	if err != nil {
		t.Fatalf("restored blob does not resolve: %v", err)
	}
	return got
}

// Backup, then restore into a fresh data dir whose blob store is
// storage.blob.local.path (not <data dir>/blobs): the object's blob
// resolves through that store.
func TestBackupRestoreBlobRoundTrip(t *testing.T) {
	src := t.TempDir()
	srcDB := filepath.Join(src, "db.sqlite")
	srcBlobs := filepath.Join(src, "blobs")
	body := seedExternalizedStore(t, srcDB, srcBlobs)

	bResult, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:       srcDB,
		BlobCfg:      config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: srcBlobs}},
		IncludeBlobs: true,
		OutputDir:    filepath.Join(src, "backup"),
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if bResult.BlobCount != 1 {
		t.Fatalf("backup blob count = %d, want 1", bResult.BlobCount)
	}

	for _, tc := range []struct {
		name   string
		source func(t *testing.T) string
	}{
		{"archive", func(*testing.T) string { return bResult.Path }},
		{"directory", func(t *testing.T) string { return extractArchive(t, bResult.Path) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := t.TempDir()
			dstDB := filepath.Join(dst, "db.sqlite")
			dstBlobs, err := local.New(filepath.Join(t.TempDir(), "elsewhere"))
			if err != nil {
				t.Fatal(err)
			}
			rResult, err := service.Restore(context.Background(), service.RestoreOpts{
				Source:      tc.source(t),
				DBPath:      dstDB,
				Blobs:       dstBlobs,
				SkipConfigs: true,
			})
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if rResult.BlobCount != 1 {
				t.Errorf("restore blob count = %d, want 1", rResult.BlobCount)
			}
			if got := readRestored(t, dstDB, dstBlobs); got != body {
				t.Errorf("restored content = %.40q, want the backed-up content", got)
			}
			if _, err := os.Stat(filepath.Join(dst, "blobs")); !os.IsNotExist(err) {
				t.Errorf("restore wrote under <db dir>/blobs instead of the configured store (stat err %v)", err)
			}
		})
	}
}

// A backup holding blobs needs somewhere to put them.
func TestRestoreBlobsWithoutStoreFails(t *testing.T) {
	src := t.TempDir()
	srcDB := filepath.Join(src, "db.sqlite")
	srcBlobs := filepath.Join(src, "blobs")
	seedExternalizedStore(t, srcDB, srcBlobs)
	bResult, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:       srcDB,
		BlobCfg:      config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: srcBlobs}},
		IncludeBlobs: true,
		OutputDir:    filepath.Join(src, "backup"),
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if _, err := service.Restore(context.Background(), service.RestoreOpts{
		Source: bResult.Path, DBPath: filepath.Join(t.TempDir(), "db.sqlite"), SkipConfigs: true,
	}); err == nil {
		t.Fatal("restore of blobs with no blob store: want error")
	}
}

// A blob entry naming a path outside the store is refused.
func TestRestoreRejectsBlobPathEscape(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "extracted")
	if err := os.MkdirAll(filepath.Join(src, "blobs", "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "blobs", "nested", "key"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	bs, err := local.New(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Restore(context.Background(), service.RestoreOpts{
		Source: src, DBPath: filepath.Join(dir, "db.sqlite"), Blobs: bs, SkipConfigs: true,
	}); err == nil {
		t.Fatal("blob key with a path separator: want error")
	}
}

// extractArchive unpacks a backup archive and returns its ctxt-backup
// directory, the layout a directory restore reads.
func extractArchive(t *testing.T, archive string) string {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, filepath.FromSlash(hdr.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "ctxt-backup")
}

// An unset storage.blob.backend is the local store (blob.New), so its
// blobs are backed up too.
func TestBackupEmptyBlobBackendIsLocal(t *testing.T) {
	src := t.TempDir()
	srcDB := filepath.Join(src, "db.sqlite")
	srcBlobs := filepath.Join(src, "blobs")
	seedExternalizedStore(t, srcDB, srcBlobs)
	res, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:       srcDB,
		BlobCfg:      config.BlobConfig{Local: config.BlobLocalConfig{Path: srcBlobs}},
		IncludeBlobs: true,
		OutputDir:    filepath.Join(src, "backup"),
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if res.BlobCount != 1 {
		t.Errorf("blob count = %d, want 1", res.BlobCount)
	}
}

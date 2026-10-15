package service_test

import (
	"archive/tar"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

// A blob's metadata survives backup and restore, in both forms.
func TestBackupRestoreKeepsBlobMeta(t *testing.T) {
	ctx := ownerCtx()
	src := t.TempDir()
	srcDB := filepath.Join(src, "db.sqlite")
	srcBlobs := filepath.Join(src, "blobs")
	seedExternalizedStore(t, srcDB, srcBlobs)
	bs, err := local.New(srcBlobs)
	if err != nil {
		t.Fatal(err)
	}
	const key = "a1b2c3d4meta"
	body := "%PDF-1.7 not really"
	want := storage.BlobMeta{
		ContentType: "application/pdf",
		Size:        int64(len(body)),
		ContentHash: "sha256:feedface",
		Filename:    "report.pdf",
		Properties:  map[string]string{"source": "upload"},
	}
	if err := bs.Put(ctx, key, strings.NewReader(body), want); err != nil {
		t.Fatal(err)
	}

	bResult, err := service.Backup(ctx, service.BackupOpts{
		DBPath:       srcDB,
		BlobCfg:      config.BlobConfig{Backend: "local", Local: config.BlobLocalConfig{Path: srcBlobs}},
		IncludeBlobs: true,
		OutputDir:    filepath.Join(src, "backup"),
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	for _, tc := range []struct {
		name   string
		source func(t *testing.T) string
	}{
		{"archive", func(*testing.T) string { return bResult.Path }},
		{"directory", func(t *testing.T) string { return extractArchive(t, bResult.Path) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst, err := local.New(filepath.Join(t.TempDir(), "store"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Restore(ctx, service.RestoreOpts{
				Source:      tc.source(t),
				DBPath:      filepath.Join(t.TempDir(), "db.sqlite"),
				Blobs:       dst,
				SkipConfigs: true,
			}); err != nil {
				t.Fatalf("Restore: %v", err)
			}
			rc, got, err := dst.Get(ctx, key)
			if err != nil {
				t.Fatalf("restored blob: %v", err)
			}
			rc.Close()
			if !reflect.DeepEqual(got, want) {
				t.Errorf("restored metadata = %+v, want %+v", got, want)
			}
		})
	}
}

// A blob metadata sidecar that is not <bare key>.json, or not a
// regular file, or not valid JSON, refuses the restore.
func TestRestoreRefusesBadBlobMeta(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry tarEntry
	}{
		{"path in key", tarEntry{name: "ctxt-backup/blobmeta/../evil.json", body: "{}"}},
		{"no json suffix", tarEntry{name: "ctxt-backup/blobmeta/abcdef", body: "{}"}},
		{"symlink", tarEntry{name: "ctxt-backup/blobmeta/abcdef.json", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"}},
		{"corrupt", tarEntry{name: "ctxt-backup/blobmeta/abcdef.json", body: "{not json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newRestoreLayout(t)
			archive := filepath.Join(l.base, "a.tar.gz")
			writeTarGz(t, archive, []tarEntry{
				{name: "ctxt-backup/ctxt.db", body: "db bytes"},
				tc.entry,
				{name: "ctxt-backup/blobs/abcdef", body: "blob"},
			})
			if _, err := service.Restore(ownerCtx(), l.opts(t, archive)); err == nil {
				t.Error("want error")
			}
			l.assertNothingWritten(t)
			if _, err := os.Stat(filepath.Join(l.base, "evil.json")); !os.IsNotExist(err) {
				t.Errorf("wrote outside the store (stat err %v)", err)
			}
		})
	}
}

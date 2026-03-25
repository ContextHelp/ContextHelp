package service_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

func TestBackupCreatesArchive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a minimal SQLite DB.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatalf("create table: %v", err)
	}
	db.Close()

	opts := service.BackupOpts{
		DBPath:       dbPath,
		IncludeBlobs: false,
		OutputDir:    outDir,
	}

	result, err := service.Backup(context.Background(), opts)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if !strings.HasSuffix(result.Path, ".tar.gz") {
		t.Fatalf("expected .tar.gz, got %q", result.Path)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("archive not created: %v", err)
	}
	if result.DBSize <= 0 {
		t.Fatalf("expected positive db size, got %d", result.DBSize)
	}

	// Verify archive contents include ctxt.db and manifest.json.
	assertArchiveContains(t, result.Path, "ctxt-backup/ctxt.db", "ctxt-backup/manifest.json")
}

func TestBackupFailsOnUnwritableOutputDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create a minimal SQLite DB.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	db.Close()

	// Use a non-existent output dir — archive creation will fail.
	opts := service.BackupOpts{
		DBPath:    dbPath,
		OutputDir: filepath.Join(dir, "no", "such", "dir"),
	}

	_, err = service.Backup(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error for unwritable output dir")
	}
}

// assertArchiveContains verifies that a .tar.gz archive contains all the listed paths.
func assertArchiveContains(t *testing.T, archivePath string, paths ...string) {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	found := map[string]bool{}
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		found[hdr.Name] = true
	}

	for _, p := range paths {
		if !found[p] {
			t.Errorf("archive missing %q; found: %v", p, found)
		}
	}
}

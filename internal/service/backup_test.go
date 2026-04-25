package service_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

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
	db, err := sql.Open("sqlite3", dbPath)
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

func TestBackupFailsOnNonEmptyOutputDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	db.Close()

	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "stale.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	opts := service.BackupOpts{
		DBPath:    dbPath,
		OutputDir: outDir,
	}

	_, err = service.Backup(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error for non-empty output dir")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("expected 'not empty' error, got: %v", err)
	}
}

func TestBackupIncludesConfigFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	cfgPath := filepath.Join(dir, "config.yaml")
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create minimal SQLite DB.
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatalf("create table: %v", err)
	}
	db.Close()

	// Write a config file.
	if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0644); err != nil {
		t.Fatal(err)
	}

	opts := service.BackupOpts{
		DBPath:     dbPath,
		OutputDir:  outDir,
		ConfigPath: cfgPath,
	}

	result, err := service.Backup(context.Background(), opts)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	assertArchiveContains(t, result.Path,
		"ctxt-backup/ctxt.db",
		"ctxt-backup/config.yaml",
		"ctxt-backup/manifest.json",
	)
}

func TestBackupNoConfigPath(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create minimal SQLite DB.
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatalf("create table: %v", err)
	}
	db.Close()

	// No ConfigPath set — should succeed without config.yaml in archive.
	opts := service.BackupOpts{
		DBPath:    dbPath,
		OutputDir: outDir,
	}

	result, err := service.Backup(context.Background(), opts)
	if err != nil {
		t.Fatalf("Backup without config: %v", err)
	}

	assertArchiveContains(t, result.Path, "ctxt-backup/ctxt.db", "ctxt-backup/manifest.json")
	assertArchiveNotContains(t, result.Path, "ctxt-backup/config.yaml")
}

func TestBackupMissingConfigFileIsNonFatal(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create minimal SQLite DB.
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatalf("create table: %v", err)
	}
	db.Close()

	// Point to a config path that doesn't exist — should warn but not fail.
	opts := service.BackupOpts{
		DBPath:     dbPath,
		OutputDir:  outDir,
		ConfigPath: filepath.Join(dir, "nonexistent.yaml"),
	}

	result, err := service.Backup(context.Background(), opts)
	if err != nil {
		t.Fatalf("Backup with missing config should be non-fatal: %v", err)
	}

	assertArchiveContains(t, result.Path, "ctxt-backup/ctxt.db", "ctxt-backup/manifest.json")
	assertArchiveNotContains(t, result.Path, "ctxt-backup/config.yaml")
}

func TestBackupAutoCreatesOutputDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	outDir := filepath.Join(dir, "deep", "nested", "out")
	opts := service.BackupOpts{
		DBPath:    dbPath,
		OutputDir: outDir,
	}

	result, err := service.Backup(context.Background(), opts)
	if err != nil {
		t.Fatalf("Backup should auto-create dir: %v", err)
	}
	if _, serr := os.Stat(result.Path); serr != nil {
		t.Fatalf("archive not created: %v", serr)
	}
}

func TestBackupIncludesConfigDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	outDir := filepath.Join(dir, "out")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	// Create config dir with files.
	cfgDir := filepath.Join(dir, "configdir")
	os.MkdirAll(filepath.Join(cfgDir, "keys"), 0755)
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("port: 8080\n"), 0644)
	os.WriteFile(filepath.Join(cfgDir, "keys", "test.pub"), []byte("ssh-ed25519 AAAA"), 0644)

	opts := service.BackupOpts{
		DBPath:         dbPath,
		OutputDir:      outDir,
		IncludeConfigs: true,
		ConfigDir:      cfgDir,
	}

	result, err := service.Backup(context.Background(), opts)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	assertArchiveContains(t, result.Path,
		"ctxt-backup/config/config.yaml",
		"ctxt-backup/config/keys/test.pub",
	)
}

func TestBackupManifestV2HasEmbeddingInfo(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	outDir := filepath.Join(dir, "out")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	opts := service.BackupOpts{
		DBPath:    dbPath,
		OutputDir: outDir,
		EmbeddingInfo: service.EmbeddingInfo{
			Backend:   "ollama",
			Model:     "snowflake-arctic-embed2",
			Dimension: 1024,
		},
	}

	result, err := service.Backup(context.Background(), opts)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	m := readManifest(t, result.Path)
	if v, ok := m["schema_version"].(float64); !ok || int(v) != 2 {
		t.Fatalf("expected schema_version 2, got %v", m["schema_version"])
	}
	emb, ok := m["embedding"].(map[string]any)
	if !ok {
		t.Fatalf("missing embedding in manifest")
	}
	if emb["backend"] != "ollama" {
		t.Errorf("embedding backend: got %v, want ollama", emb["backend"])
	}
	if emb["model"] != "snowflake-arctic-embed2" {
		t.Errorf("embedding model: got %v, want snowflake-arctic-embed2", emb["model"])
	}
	if dim, ok := emb["dimension"].(float64); !ok || int(dim) != 1024 {
		t.Errorf("embedding dimension: got %v, want 1024", emb["dimension"])
	}
}

func TestRestoreFromArchive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	backupDir := filepath.Join(dir, "backup")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO items (name) VALUES ('hello')"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	// Create config dir.
	cfgDir := filepath.Join(dir, "cfgdir")
	os.MkdirAll(cfgDir, 0755)
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("port: 8080\n"), 0644)

	bResult, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:         dbPath,
		OutputDir:      backupDir,
		IncludeConfigs: true,
		ConfigDir:      cfgDir,
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Restore.
	restoreDir := filepath.Join(dir, "restored")
	restoreDB := filepath.Join(restoreDir, "ctxt.db")
	restoreCfg := filepath.Join(restoreDir, "config")

	rResult, err := service.Restore(context.Background(), service.RestoreOpts{
		Source:    bResult.Path,
		DBPath:    restoreDB,
		ConfigDir: restoreCfg,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if rResult.DBSize <= 0 {
		t.Error("expected positive DB size")
	}
	if rResult.ConfigCount == 0 {
		t.Error("expected config count > 0")
	}
}

func TestRestoreDryRun(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	backupDir := filepath.Join(dir, "backup")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	bResult, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:    dbPath,
		OutputDir: backupDir,
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	restoreDir := filepath.Join(dir, "restored")
	restoreDB := filepath.Join(restoreDir, "ctxt.db")

	rResult, err := service.Restore(context.Background(), service.RestoreOpts{
		Source: bResult.Path,
		DBPath: restoreDB,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Restore dry-run: %v", err)
	}
	if rResult.DBSize <= 0 {
		t.Error("expected positive DB size in dry run")
	}
	if _, serr := os.Stat(restoreDB); !os.IsNotExist(serr) {
		t.Error("dry run should not create files")
	}
}

func TestRestoreFromDir(t *testing.T) {
	dir := t.TempDir()

	// Create an extracted backup directory.
	backupDir := filepath.Join(dir, "extracted")
	os.MkdirAll(filepath.Join(backupDir, "config"), 0755)

	// Create a DB.
	dbSrc := filepath.Join(backupDir, "ctxt.db")
	db, err := sql.Open("sqlite3", dbSrc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	os.WriteFile(filepath.Join(backupDir, "config", "test.yaml"), []byte("x: 1\n"), 0644)

	restoreDir := filepath.Join(dir, "restored")
	restoreDB := filepath.Join(restoreDir, "ctxt.db")
	restoreCfg := filepath.Join(restoreDir, "config")

	rResult, err := service.Restore(context.Background(), service.RestoreOpts{
		Source:    backupDir,
		DBPath:    restoreDB,
		ConfigDir: restoreCfg,
	})
	if err != nil {
		t.Fatalf("Restore from dir: %v", err)
	}
	if rResult.DBSize <= 0 {
		t.Error("expected positive DB size")
	}
	if rResult.ConfigCount == 0 {
		t.Error("expected config count > 0")
	}
}

func TestRestoreSkipConfigs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	backupDir := filepath.Join(dir, "backup")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	cfgDir := filepath.Join(dir, "cfgdir")
	os.MkdirAll(cfgDir, 0755)
	os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("x: 1\n"), 0644)

	bResult, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:         dbPath,
		OutputDir:      backupDir,
		IncludeConfigs: true,
		ConfigDir:      cfgDir,
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	restoreDir := filepath.Join(dir, "restored")
	restoreDB := filepath.Join(restoreDir, "ctxt.db")
	restoreCfg := filepath.Join(restoreDir, "config")

	rResult, err := service.Restore(context.Background(), service.RestoreOpts{
		Source:      bResult.Path,
		DBPath:      restoreDB,
		ConfigDir:   restoreCfg,
		SkipConfigs: true,
	})
	if err != nil {
		t.Fatalf("Restore skip configs: %v", err)
	}
	if rResult.ConfigCount != 0 {
		t.Errorf("expected ConfigCount=0 with SkipConfigs, got %d", rResult.ConfigCount)
	}
}

func readManifest(t *testing.T, archivePath string) map[string]any {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, _ := gzip.NewReader(f)
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			t.Fatal("manifest not found in archive")
		}
		if hdr.Name == "ctxt-backup/manifest.json" {
			data, _ := io.ReadAll(tr)
			var m map[string]any
			json.Unmarshal(data, &m)
			return m
		}
	}
}

// assertArchiveNotContains verifies a .tar.gz archive does NOT contain any of the listed paths.
func assertArchiveNotContains(t *testing.T, archivePath string, paths ...string) {
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
		if found[p] {
			t.Errorf("archive should not contain %q", p)
		}
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

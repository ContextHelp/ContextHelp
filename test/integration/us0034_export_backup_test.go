package integration

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// readArchiveEntries returns all entry names found inside a .tar.gz archive.
func readArchiveEntries(t *testing.T, archivePath string) map[string]bool {
	t.Helper()
	f, err := os.Open(archivePath)
	require.NoError(t, err, "open archive")
	defer f.Close()

	gr, err := gzip.NewReader(f)
	require.NoError(t, err, "gzip reader")
	defer gr.Close()

	entries := map[string]bool{}
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		entries[hdr.Name] = true
	}
	return entries
}

// readManifest reads and decodes manifest.json from a .tar.gz archive.
func readManifest(t *testing.T, archivePath string) map[string]any {
	t.Helper()
	f, err := os.Open(archivePath)
	require.NoError(t, err)
	defer f.Close()

	gr, err := gzip.NewReader(f)
	require.NoError(t, err)
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Name == "ctxt-backup/manifest.json" {
			var m map[string]any
			require.NoError(t, json.NewDecoder(tr).Decode(&m))
			return m
		}
	}
	t.Fatal("manifest.json not found in archive")
	return nil
}

// createMinimalSQLiteDB creates a minimal SQLite database at path for use as
// a backup source in tests that bypass the full service stack.
func createMinimalSQLiteDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// US-0034 Tests
// ---------------------------------------------------------------------------

// TestUS0034_BackupArchiveCreated verifies that Backup() creates a .tar.gz
// archive in the specified output directory.
func TestUS0034_BackupArchiveCreated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ctxt.db")
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0755))

	createMinimalSQLiteDB(t, dbPath)

	result, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:       dbPath,
		IncludeBlobs: false,
		OutputDir:    outDir,
	})
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(result.Path, ".tar.gz"), "archive should end in .tar.gz")
	_, statErr := os.Stat(result.Path)
	assert.NoError(t, statErr, "archive file must exist")
	assert.Greater(t, result.DBSize, int64(0), "reported db size must be positive")
}

// TestUS0034_ArchiveContainsDBAndManifest verifies that the archive contains
// ctxt-backup/ctxt.db and ctxt-backup/manifest.json.
func TestUS0034_ArchiveContainsDBAndManifest(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ctxt.db")
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0755))

	createMinimalSQLiteDB(t, dbPath)

	result, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:       dbPath,
		IncludeBlobs: false,
		OutputDir:    outDir,
	})
	require.NoError(t, err)

	entries := readArchiveEntries(t, result.Path)
	assert.True(t, entries["ctxt-backup/ctxt.db"], "archive must contain ctxt-backup/ctxt.db")
	assert.True(t, entries["ctxt-backup/manifest.json"], "archive must contain ctxt-backup/manifest.json")
}

// TestUS0034_ManifestSchemaVersionIsCurrent verifies the manifest.json
// schema_version pins to the current value emitted by service.Backup. Bump
// this constant in lockstep with internal/service/backup.go when the manifest
// shape changes (and document the migration in docs/stories/operations/US-0034).
func TestUS0034_ManifestSchemaVersionIsCurrent(t *testing.T) {
	const wantSchemaVersion = 2
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ctxt.db")
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0755))

	createMinimalSQLiteDB(t, dbPath)

	result, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:    dbPath,
		OutputDir: outDir,
	})
	require.NoError(t, err)

	manifest := readManifest(t, result.Path)
	require.NotNil(t, manifest)

	sv, ok := manifest["schema_version"]
	require.True(t, ok, "manifest must contain schema_version")
	assert.Equal(t, float64(wantSchemaVersion), sv,
		"schema_version drift — bump test constant in lockstep with backup.go")
}

// TestUS0034_BackupContainsObjectsAfterIngestion verifies that after ingesting
// N objects and running a backup, the archive's DB snapshot is non-trivial.
func TestUS0034_BackupContainsObjectsAfterIngestion(t *testing.T) {
	const nObjects = 5

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ctxt.db")
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0755))

	// Seed a SQLite DB with the schema and some objects via the full driver.
	driver, err := newSQLiteDriver(t, dbPath)
	require.NoError(t, err)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for i := 0; i < nObjects; i++ {
		obj := &storage.KnowledgeObject{
			ID:        storageObjID(i),
			Type:      "text",
			RawContent: storageObjContent(i),
			CreatedAt: now,
			UpdatedAt: now,
		}
		require.NoError(t, driver.Objects().Create(ctx, obj))
	}
	require.NoError(t, driver.Close(ctx))

	result, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:       dbPath,
		IncludeBlobs: false,
		OutputDir:    outDir,
	})
	require.NoError(t, err)

	entries := readArchiveEntries(t, result.Path)
	assert.True(t, entries["ctxt-backup/ctxt.db"])
	assert.True(t, entries["ctxt-backup/manifest.json"])
	assert.Greater(t, result.DBSize, int64(0))
}

// TestUS0034_BackupWithEdgesIncluded verifies that after ingesting objects with
// edges the backup archive is created successfully (edges are in the DB snapshot).
func TestUS0034_BackupWithEdgesIncluded(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ctxt.db")
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0755))

	driver, err := newSQLiteDriver(t, dbPath)
	require.NoError(t, err)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID: "export-obj-1", Type: "text", RawContent: "content", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, driver.Objects().Create(ctx, obj))

	edge := &storage.Edge{
		ID: "export-edge-1", FromType: "object", FromID: "export-obj-1",
		ToType: "entity", ToID: "@test.entity", EdgeType: "mentions",
		Weight: 1.0, CreatedAt: now,
	}
	require.NoError(t, driver.Edges().Create(ctx, edge))
	require.NoError(t, driver.Close(ctx))

	result, err := service.Backup(context.Background(), service.BackupOpts{
		DBPath:    dbPath,
		OutputDir: outDir,
	})
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(result.Path, ".tar.gz"))

	entries := readArchiveEntries(t, result.Path)
	assert.True(t, entries["ctxt-backup/ctxt.db"])
	assert.True(t, entries["ctxt-backup/manifest.json"])
}

// ---------------------------------------------------------------------------
// internal helpers for US-0034
// ---------------------------------------------------------------------------

// newSQLiteDriver creates and initialises a SQLite driver at the given path.
// The caller is responsible for closing it.
func newSQLiteDriver(t *testing.T, path string) (*sqlite.Driver, error) {
	t.Helper()
	driver, err := sqlite.New(path)
	if err != nil {
		return nil, err
	}
	if err := driver.Init(context.Background()); err != nil {
		driver.Close(context.Background()) //nolint:errcheck
		return nil, err
	}
	return driver, nil
}

func storageObjID(i int) string {
	return fmt.Sprintf("export-obj-%04d", i)
}

func storageObjContent(i int) string {
	return fmt.Sprintf("export content for object %d", i)
}

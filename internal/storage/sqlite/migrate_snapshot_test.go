package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// seedEntity writes a marker row the snapshot must carry. entities has
// kept these columns since migration 001.
func seedEntity(t *testing.T, d *Driver, slug string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.db.Exec(`INSERT INTO entities (slug, created_at, updated_at) VALUES (?, ?, ?)`,
		slug, now, now); err != nil {
		t.Fatalf("seed entity: %v", err)
	}
}

// snapshotFiles lists the pre-migration snapshot names in dir.
func snapshotFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if snapshotNameRE.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// schemaVersion reads MAX(version) from the ledger of db.
func schemaVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	return v
}

// openSnapshot opens a snapshot file read-only through the same driver
// build (sqlite-vec registered), so virtual tables in it resolve.
func openSnapshot(t *testing.T, path string) *sql.DB {
	t.Helper()
	d, err := New(path, testBlobStore(t))
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return d.db
}

func TestMigrate_PendingMigrationWritesSnapshot(t *testing.T) {
	latest := latestMigrationVersion()
	for _, from := range []int{previousMigrationVersion(), 20} { // 20: vec0 virtual table present
		t.Run("from_v"+strconv.Itoa(from), func(t *testing.T) {
			dir := t.TempDir()
			dbPath := filepath.Join(dir, "db.sqlite")
			old := openAtVersion(t, dbPath, from)
			seedEntity(t, old, "marker")
			if err := old.Close(context.Background()); err != nil {
				t.Fatal(err)
			}

			d, err := New(dbPath, testBlobStore(t))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close(context.Background())
			if err := d.Init(context.Background()); err != nil {
				t.Fatalf("init: %v", err)
			}
			if got := schemaVersion(t, d.db); got != latest {
				t.Fatalf("live schema version = %d, want %d", got, latest)
			}

			backups := filepath.Join(dir, "backups")
			snaps := snapshotFiles(t, backups)
			if len(snaps) != 1 {
				t.Fatalf("snapshots = %v, want exactly one", snaps)
			}
			wantName := regexp.MustCompile(`^pre-migrate-v` + strconv.Itoa(from) + `-v` + strconv.Itoa(latest) + `-\d{8}T\d{6}Z\.sqlite$`)
			if !wantName.MatchString(snaps[0]) {
				t.Fatalf("snapshot name %q does not match %s", snaps[0], wantName)
			}
			if leftovers, _ := filepath.Glob(filepath.Join(backups, "*.partial")); len(leftovers) != 0 {
				t.Fatalf("temporary files left behind: %v", leftovers)
			}
			if runtime.GOOS != goosWindows {
				fi, err := os.Stat(filepath.Join(backups, snaps[0]))
				if err != nil {
					t.Fatal(err)
				}
				if perm := fi.Mode().Perm(); perm&0o077 != 0 {
					t.Fatalf("snapshot mode = %v, want owner-only", perm)
				}
			}

			snap := openSnapshot(t, filepath.Join(backups, snaps[0]))
			if got := schemaVersion(t, snap); got != from {
				t.Fatalf("snapshot schema version = %d, want pre-migration %d", got, from)
			}
			var slug string
			if err := snap.QueryRow(`SELECT slug FROM entities WHERE slug = 'marker'`).Scan(&slug); err != nil {
				t.Fatalf("snapshot lost the pre-migration data: %v", err)
			}
			var integrity string
			if err := snap.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
				t.Fatalf("snapshot integrity_check = %q, %v", integrity, err)
			}
		})
	}
}

func TestMigrate_NoPendingMigrationWritesNoSnapshot(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.sqlite")
	d := openAtVersion(t, dbPath, latestMigrationVersion())
	seedEntity(t, d, "marker")
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	again, err := New(dbPath, testBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close(context.Background())
	if err := again.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("backups dir exists after an open with nothing to migrate (stat err %v)", err)
	}
}

func TestMigrate_FreshDatabaseWritesNoSnapshot(t *testing.T) {
	dir := t.TempDir()
	d, err := New(filepath.Join(dir, "db.sqlite"), testBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	if err := d.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("backups dir exists after creating a new database (stat err %v)", err)
	}
}

func TestMigrate_SnapshotOptOut(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.sqlite")
	old := openAtVersion(t, dbPath, previousMigrationVersion())
	seedEntity(t, old, "marker")
	if err := old.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	d, err := New(dbPath, testBlobStore(t), WithMigrateSnapshot(false))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	if err := d.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := schemaVersion(t, d.db); got != latestMigrationVersion() {
		t.Fatalf("schema version = %d, want %d", got, latestMigrationVersion())
	}
	if snaps := snapshotFiles(t, filepath.Join(dir, "backups")); len(snaps) != 0 {
		t.Fatalf("snapshots written with storage.migrate_snapshot=false: %v", snaps)
	}
}

func TestMigrate_SnapshotFailureAbortsMigration(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("directory permission bits do not block writes on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.sqlite")
	from := previousMigrationVersion()
	old := openAtVersion(t, dbPath, from)
	seedEntity(t, old, "marker")
	if err := old.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	if err := os.Mkdir(backups, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(backups, 0o700) })

	d, err := New(dbPath, testBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	err = d.Init(context.Background())
	if err == nil {
		t.Fatal("init succeeded although the snapshot could not be written")
	}
	for _, want := range []string{"snapshot", backups, "nothing migrated"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if got := schemaVersion(t, d.db); got != from {
		t.Fatalf("schema version = %d after failed snapshot, want unmigrated %d", got, from)
	}
}

func TestMigrate_SnapshotRetentionKeepsNewestThree(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.sqlite")
	from := previousMigrationVersion()
	old := openAtVersion(t, dbPath, from)
	if err := old.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	backups := filepath.Join(dir, "backups")
	if err := os.Mkdir(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	// Three older snapshots (one a same-second collision) and files
	// retention must never touch.
	older := []string{
		"pre-migrate-v1-v2-20200101T000000Z.sqlite",
		"pre-migrate-v2-v3-20210101T000000Z.sqlite",
		"pre-migrate-v2-v3-20210101T000000Z-1.sqlite",
	}
	unrelated := []string{"ctxt-backup-2020-01-01T00-00-00Z.tar.gz", "notes.txt", "pre-migrate-v1-v2-junk.sqlite"}
	for _, n := range append(append([]string{}, older...), unrelated...) {
		if err := os.WriteFile(filepath.Join(backups, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	d, err := New(dbPath, testBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	if err := d.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	snaps := snapshotFiles(t, backups)
	if len(snaps) != snapshotKeep {
		t.Fatalf("snapshots after retention = %v, want %d", snaps, snapshotKeep)
	}
	for _, gone := range older[:1] {
		if _, err := os.Stat(filepath.Join(backups, gone)); !os.IsNotExist(err) {
			t.Errorf("oldest snapshot %s kept", gone)
		}
	}
	for _, kept := range older[1:] {
		if _, err := os.Stat(filepath.Join(backups, kept)); err != nil {
			t.Errorf("snapshot %s deleted, want kept: %v", kept, err)
		}
	}
	newPrefix := "pre-migrate-v" + strconv.Itoa(from) + "-v" + strconv.Itoa(latestMigrationVersion()) + "-"
	found := false
	for _, s := range snaps {
		if strings.HasPrefix(s, newPrefix) {
			found = true
		}
	}
	if !found {
		t.Errorf("new snapshot %s* missing from %v", newPrefix, snaps)
	}
	for _, n := range unrelated {
		if _, err := os.Stat(filepath.Join(backups, n)); err != nil {
			t.Errorf("unrelated file %s touched by retention: %v", n, err)
		}
	}
}

func TestPruneMigrateSnapshots_OrdersByTimestampNotVersion(t *testing.T) {
	dir := t.TempDir()
	// Lexical order by name would rank v9 above v10; age must decide.
	names := []string{
		"pre-migrate-v9-v10-20260101T000000Z.sqlite",
		"pre-migrate-v10-v11-20260201T000000Z.sqlite",
		"pre-migrate-v11-v12-20260301T000000Z.sqlite",
		"pre-migrate-v12-v13-20260401T000000Z.sqlite",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneMigrateSnapshots(dir, 3); err != nil {
		t.Fatal(err)
	}
	got := snapshotFiles(t, dir)
	want := append([]string{}, names[1:]...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("kept %v, want %v", got, want)
	}
}

func TestMigrate_SnapshotIncludesUncheckpointedWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.sqlite")
	from := previousMigrationVersion()
	old := openAtVersion(t, dbPath, from)
	defer old.Close(context.Background())
	// Pin one connection with checkpointing off so the marker lives only
	// in the WAL while the migrating handle snapshots the database.
	conn, err := old.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `PRAGMA wal_autocheckpoint = 0`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := conn.ExecContext(context.Background(),
		`INSERT INTO entities (slug, created_at, updated_at) VALUES ('wal-only', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dbPath + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("expected a non-empty WAL holding the marker (err %v)", err)
	}

	d, err := New(dbPath, testBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(context.Background())
	if err := d.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	snaps := snapshotFiles(t, filepath.Join(dir, "backups"))
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %v, want one", snaps)
	}
	snap := openSnapshot(t, filepath.Join(dir, "backups", snaps[0]))
	var n int
	if err := snap.QueryRow(`SELECT COUNT(*) FROM entities WHERE slug = 'wal-only'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("snapshot missing the WAL-only row: n=%d err=%v", n, err)
	}
}

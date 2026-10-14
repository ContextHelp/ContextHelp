package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fileState is what CheckSchema must leave untouched: the database
// file's bytes and the directory's entries. The -wal and -shm files are
// left out: a read-only connection to a WAL-mode database creates them
// (SQLite needs the shared-memory index to read), and it cannot remove
// them on close; the next read-write open owns them as usual.
type fileState struct {
	db      []byte
	entries []string
}

func readFileState(t *testing.T, dbPath string) fileState {
	t.Helper()
	b, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	es, err := os.ReadDir(filepath.Dir(dbPath))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		if n := e.Name(); n != filepath.Base(dbPath)+"-wal" && n != filepath.Base(dbPath)+"-shm" {
			names = append(names, n)
		}
	}
	return fileState{db: b, entries: names}
}

func assertFileStateUnchanged(t *testing.T, dbPath string, before fileState) {
	t.Helper()
	after := readFileState(t, dbPath)
	if string(after.db) != string(before.db) {
		t.Error("CheckSchema changed the database file")
	}
	if len(after.entries) != len(before.entries) {
		t.Errorf("directory entries changed: before %v, after %v", before.entries, after.entries)
	}
}

func TestCheckSchema_PendingReadsVersionWithoutMigrating(t *testing.T) {
	latest := LatestSchemaVersion()
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	old := openAtVersion(t, dbPath, latest-1)
	seedEntity(t, old, "marker")
	if err := old.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := readFileState(t, dbPath)

	st, err := CheckSchema(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if st.Current != latest-1 || st.Latest != latest || st.Target != latest || st.New || !st.Pending() {
		t.Errorf("status = %+v, want current %d, latest/target %d, pending, not new", st, latest-1, latest)
	}
	assertFileStateUnchanged(t, dbPath, before)
	if _, err := os.Stat(SnapshotDir(dbPath)); !os.IsNotExist(err) {
		t.Errorf("CheckSchema created %s (stat err %v)", SnapshotDir(dbPath), err)
	}
}

func TestCheckSchema_Current(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	d := openAtVersion(t, dbPath, LatestSchemaVersion())
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := readFileState(t, dbPath)
	st, err := CheckSchema(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if st.Current != LatestSchemaVersion() || st.Pending() || st.New || st.Ahead() {
		t.Errorf("status = %+v, want current at latest, nothing pending", st)
	}
	assertFileStateUnchanged(t, dbPath, before)
}

// A store held open by another connection (dpkms running) is in WAL
// mode with live -wal/-shm files; the read-only check still reads it.
func TestCheckSchema_WhileOpenElsewhere(t *testing.T) {
	latest := LatestSchemaVersion()
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	d := openAtVersion(t, dbPath, latest-1)
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	seedEntity(t, d, "in-wal")

	st, err := CheckSchema(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if st.Current != latest-1 || st.Target != latest {
		t.Errorf("status = %+v, want v%d -> v%d", st, latest-1, latest)
	}
	if v := schemaVersion(t, d.db); v != latest-1 {
		t.Errorf("ledger at v%d after CheckSchema, want v%d", v, latest-1)
	}
}

func TestCheckSchema_MissingFileIsNewAndNotCreated(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "absent.sqlite")
	st, err := CheckSchema(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if !st.New || st.Current != 0 || st.Target != LatestSchemaVersion() {
		t.Errorf("status = %+v, want new database pending v0 -> latest", st)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Errorf("CheckSchema created %s (stat err %v)", dbPath, err)
	}
}

func TestCheckSchema_EmptyFileIsNew(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty.sqlite")
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := CheckSchema(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if !st.New {
		t.Errorf("status = %+v, want new", st)
	}
}

// Version 0 with tables of its own is data at stake: the next open
// snapshots it, so it is not reported as new.
func TestCheckSchema_TablesWithoutLedgerAreNotNew(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.sqlite")
	raw, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE entities (slug TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := CheckSchema(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if st.New || st.Current != 0 || !st.Pending() {
		t.Errorf("status = %+v, want pending, not new", st)
	}
	if n := countTables(t, dbPath, "schema_version"); n != 0 {
		t.Error("CheckSchema created the schema_version ledger")
	}
}

func TestCheckSchema_AheadOfBinary(t *testing.T) {
	latest := LatestSchemaVersion()
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	d := openAtVersion(t, dbPath, latest)
	if _, err := d.db.Exec(`INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`,
		latest+1, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err := CheckSchema(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if !st.Ahead() || st.Pending() || st.Current != latest+1 {
		t.Errorf("status = %+v, want ahead of v%d, nothing pending", st, latest)
	}
}

// countTables counts tables named name in the database at path.
func countTables(t *testing.T, path, name string) int {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A database holding only an empty ledger has nothing to lose: the
// next open takes no snapshot, so it is reported as new.
func TestCheckSchema_EmptyLedgerIsNew(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ledger-only.sqlite")
	raw, err := sql.Open("sqlite3", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := CheckSchema(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if !st.New || st.Current != 0 || !st.Pending() {
		t.Errorf("status = %+v, want new database pending", st)
	}
}

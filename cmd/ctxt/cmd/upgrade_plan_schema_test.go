package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// sqlDB is the test database's handle.
func sqlDB(t *testing.T, db *testDB) *sql.DB {
	t.Helper()
	d, ok := db.Driver.(*sqlite.Driver)
	if !ok {
		t.Fatalf("test driver is %T, want *sqlite.Driver", db.Driver)
	}
	return d.DB()
}

// ledgerVersion reads MAX(version) from the test database's ledger.
func ledgerVersion(t *testing.T, db *testDB) int {
	t.Helper()
	var v int
	if err := sqlDB(t, db).QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	return v
}

// rollBackLedger makes the test database look one migration behind:
// the newest ledger row goes, so the next normal open re-applies that
// (idempotent) migration and records it again.
func rollBackLedger(t *testing.T, db *testDB) int {
	t.Helper()
	latest := sqlite.LatestSchemaVersion()
	if _, err := sqlDB(t, db).Exec(`DELETE FROM schema_version WHERE version = ?`, latest); err != nil {
		t.Fatalf("roll back ledger: %v", err)
	}
	return latest
}

// testDBPath is the database file setupTestDB's config points at.
func testDBPath(db *testDB) string {
	return filepath.Join(filepath.Dir(db.ConfigPath), "test.db")
}

func TestUpgradePlan_PendingSchemaReportedWithoutMigrating(t *testing.T) {
	db := setupTestDB(t)
	latest := rollBackLedger(t, db)
	backups := sqlite.SnapshotDir(testDBPath(db))

	out, err := db.exec("upgrade", "plan")
	if err != nil {
		t.Fatalf("upgrade plan: %v\n%s", err, out)
	}
	want := fmt.Sprintf("v%d → v%d pending; next open will snapshot to %s", latest-1, latest, backups)
	if !strings.Contains(out, want) {
		t.Errorf("plan output missing %q\nfull output:\n%s", want, out)
	}
	if !strings.Contains(out, "not computed until the schema migration runs") {
		t.Errorf("plan output should say the buckets wait for the migration\nfull output:\n%s", out)
	}
	if v := ledgerVersion(t, db); v != latest-1 {
		t.Errorf("schema at v%d after plan, want v%d: plan migrated the store", v, latest-1)
	}
	if _, err := os.Stat(backups); !os.IsNotExist(err) {
		t.Errorf("plan created %s (stat err %v): it snapshotted the store", backups, err)
	}
}

func TestUpgradePlan_PendingSchemaSnapshotDisabled(t *testing.T) {
	db := setupTestDB(t)
	latest := rollBackLedger(t, db)
	f, err := os.OpenFile(db.ConfigPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("  migrate_snapshot: false\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	out, err := db.exec("upgrade", "plan")
	if err != nil {
		t.Fatalf("upgrade plan: %v\n%s", err, out)
	}
	want := fmt.Sprintf("v%d → v%d pending; next open migrates without a snapshot (storage.migrate_snapshot=false)", latest-1, latest)
	if !strings.Contains(out, want) {
		t.Errorf("plan output missing %q\nfull output:\n%s", want, out)
	}
	if v := ledgerVersion(t, db); v != latest-1 {
		t.Errorf("schema at v%d after plan, want v%d", v, latest-1)
	}
}

func TestUpgradePlan_PendingSchemaJSON(t *testing.T) {
	db := setupTestDB(t)
	latest := rollBackLedger(t, db)

	out, err := db.exec("upgrade", "plan", "--format", "json")
	if err != nil {
		t.Fatalf("upgrade plan --format json: %v\n%s", err, out)
	}
	var got struct {
		Schema struct {
			Backend     string `json:"backend"`
			Current     int    `json:"current"`
			Latest      int    `json:"latest"`
			Target      int    `json:"target"`
			Pending     bool   `json:"pending"`
			SnapshotDir string `json:"snapshot_dir"`
		} `json:"schema"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	s := got.Schema
	if s.Backend != "sqlite" || s.Current != latest-1 || s.Latest != latest || s.Target != latest ||
		!s.Pending || s.SnapshotDir != sqlite.SnapshotDir(testDBPath(db)) {
		t.Errorf("schema = %+v, want sqlite v%d -> v%d pending into %s", s, latest-1, latest, sqlite.SnapshotDir(testDBPath(db)))
	}
	if v := ledgerVersion(t, db); v != latest-1 {
		t.Errorf("schema at v%d after plan, want v%d", v, latest-1)
	}
}

func TestUpgradePlan_SchemaCurrent(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("upgrade", "plan")
	if err != nil {
		t.Fatalf("upgrade plan: %v\n%s", err, out)
	}
	want := fmt.Sprintf("v%d current", sqlite.LatestSchemaVersion())
	for _, frag := range []string{want, "reingest_selective:", "Total objects pending:"} {
		if !strings.Contains(out, frag) {
			t.Errorf("plan output missing %q\nfull output:\n%s", frag, out)
		}
	}
}

// No database yet: plan reports the schema the next open creates and
// does not create the file itself.
func TestUpgradePlan_NoDatabaseYet(t *testing.T) {
	db := setupTestDB(t)
	absent := filepath.Join(t.TempDir(), "absent.db")
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n  path: "+absent+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db.ConfigPath = cfgPath

	out, err := db.exec("upgrade", "plan")
	if err != nil {
		t.Fatalf("upgrade plan: %v\n%s", err, out)
	}
	want := fmt.Sprintf("new database; next open creates schema v%d", sqlite.LatestSchemaVersion())
	if !strings.Contains(out, want) {
		t.Errorf("plan output missing %q\nfull output:\n%s", want, out)
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Errorf("plan created %s (stat err %v)", absent, err)
	}
}

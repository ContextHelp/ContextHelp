//go:build integration

package postgres_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// syncBuffer is a bytes.Buffer safe for the slog handler and the test
// to share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureSlogJSON routes the default slog logger to a JSON buffer for
// the test.
func captureSlogJSON(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// migrationWarnings returns the decoded pre-migration warnings in out.
func migrationWarnings(t *testing.T, out string) []map[string]any {
	t.Helper()
	var found []map[string]any
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("decode log line %q: %v", sc.Text(), err)
		}
		if msg, _ := rec["msg"].(string); rec["level"] == "WARN" && strings.Contains(msg, "pg_dump") {
			found = append(found, rec)
		}
	}
	return found
}

// TestPgMigrate_PendingMigrationWarnsWithVersions opens an existing
// database one migration behind and asserts the open warns, naming both
// versions, before migrating it; a second open (schema current) stays
// silent.
func TestPgMigrate_PendingMigrationWarnsWithVersions(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	ctx := context.Background()
	latest := pgdrv.LatestSchemaVersion()

	old, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	if err := old.MigrateThroughForTest(ctx, latest-1); err != nil {
		t.Fatalf("migrate through %d: %v", latest-1, err)
	}
	if err := old.Close(ctx); err != nil {
		t.Fatal(err)
	}

	// The read-only check sees the pending migration and leaves it pending.
	st, err := pgdrv.CheckSchema(ctx, dsn)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if st.Current != latest-1 || st.Target != latest || st.New {
		t.Errorf("CheckSchema = %+v, want v%d -> v%d, not new", st, latest-1, latest)
	}

	buf := captureSlogJSON(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(ctx) })
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	warns := migrationWarnings(t, buf.String())
	if len(warns) != 1 {
		t.Fatalf("got %d pre-migration warnings, want 1; log:\n%s", len(warns), buf.String())
	}
	w := warns[0]
	if from, _ := w["from"].(float64); int(from) != latest-1 {
		t.Errorf("warning from = %v, want %d", w["from"], latest-1)
	}
	if to, _ := w["to"].(float64); int(to) != latest {
		t.Errorf("warning to = %v, want %d", w["to"], latest)
	}

	var v int
	if err := drv.DB().QueryRowContext(ctx, `SELECT MAX(version) FROM schema_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != latest {
		t.Errorf("schema at v%d after open, want v%d", v, latest)
	}

	// Reopen at the current schema: nothing pending, no warning.
	buf2 := captureSlogJSON(t)
	again, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { again.Close(ctx) })
	if err := again.Init(ctx); err != nil {
		t.Fatalf("Init (current): %v", err)
	}
	if warns := migrationWarnings(t, buf2.String()); len(warns) != 0 {
		t.Errorf("current schema warned: %v", warns)
	}
}

// TestPgCheckSchema_CurrentAndEmpty pins the read-only check at both
// ends: a migrated database reports current with nothing pending, and an
// empty database (no tables) reports new.
func TestPgCheckSchema_CurrentAndEmpty(t *testing.T) {
	ctx := context.Background()
	latest := pgdrv.LatestSchemaVersion()

	migrated, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(migrated, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(ctx) })
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	st, err := pgdrv.CheckSchema(ctx, migrated)
	if err != nil {
		t.Fatalf("CheckSchema(migrated): %v", err)
	}
	if st.Current != latest || st.Pending() || st.New {
		t.Errorf("CheckSchema(migrated) = %+v, want v%d current, nothing pending, not new", st, latest)
	}

	empty, _ := freshDatabaseDSN(t)
	st, err = pgdrv.CheckSchema(ctx, empty)
	if err != nil {
		t.Fatalf("CheckSchema(empty): %v", err)
	}
	if st.Current != 0 || st.Target != latest || !st.New {
		t.Errorf("CheckSchema(empty) = %+v, want v0 -> v%d, new", st, latest)
	}
}

// TestPgMigrate_TablesWithoutLedgerIsNotNew opens a database holding
// ctxt tables but no schema_version ledger (initialized before the
// ledger existed): it has data at stake, so the read-only check must not
// call it new and the open must warn before migrating it, as SQLite
// snapshots such a database.
func TestPgMigrate_TablesWithoutLedgerIsNotNew(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	ctx := context.Background()
	latest := pgdrv.LatestSchemaVersion()

	old, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	// Through 14: the newest schema a pre-ledger database can carry and
	// still replay the chain (see AdoptsPreLedgerDatabase).
	if err := old.MigrateThroughForTest(ctx, 14); err != nil {
		t.Fatalf("migrate through 14: %v", err)
	}
	if _, err := old.DB().Exec(`DROP TABLE schema_version`); err != nil {
		t.Fatalf("drop schema_version: %v", err)
	}
	if err := old.Close(ctx); err != nil {
		t.Fatal(err)
	}

	st, err := pgdrv.CheckSchema(ctx, dsn)
	if err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	if st.Current != 0 || st.Target != latest || st.New {
		t.Errorf("CheckSchema = %+v, want v0 -> v%d, not new", st, latest)
	}

	buf := captureSlogJSON(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(ctx) })
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	warns := migrationWarnings(t, buf.String())
	if len(warns) != 1 {
		t.Fatalf("got %d pre-migration warnings, want 1; log:\n%s", len(warns), buf.String())
	}
	if from, _ := warns[0]["from"].(float64); int(from) != 0 {
		t.Errorf("warning from = %v, want 0", warns[0]["from"])
	}
	if to, _ := warns[0]["to"].(float64); int(to) != latest {
		t.Errorf("warning to = %v, want %d", warns[0]["to"], latest)
	}
}

// TestPgMigrate_EmptyDatabaseIsSilent pins the other side: a database
// with no tables has nothing to lose and opens without a warning.
func TestPgMigrate_EmptyDatabaseIsSilent(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	ctx := context.Background()
	buf := captureSlogJSON(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(ctx) })
	if err := drv.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if warns := migrationWarnings(t, buf.String()); len(warns) != 0 {
		t.Errorf("empty database warned: %v", warns)
	}
}

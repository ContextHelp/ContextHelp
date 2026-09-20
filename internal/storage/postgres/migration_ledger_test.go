//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"testing"
	"time"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// freshIntegrationDriver provisions a brand-new database on the integration
// Postgres server, migrates it, and returns the driver. Unlike
// newIntegrationDriver (shared, pre-populated database), this reproduces the
// fresh-install path — the exact surface where unversioned, ad-hoc schema
// bootstrapping historically broke (objects.source_key referenced by every
// query but created by no migration).
func freshIntegrationDriver(t *testing.T) (*pgdrv.Driver, string) {
	t.Helper()
	dsn, name := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New(fresh): %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate(fresh): %v", err)
	}
	return drv, name
}

// freshDatabaseDSN provisions a brand-new database on the integration server
// and returns a DSN pointing at it, without opening a driver — tests that
// need pre-Init configuration (SetVectorDimension) construct their own.
func freshDatabaseDSN(t *testing.T) (dsn, name string) {
	t.Helper()
	baseDSN := integrationDSN()
	if baseDSN == "" {
		t.Skip("POSTGRES_DSN (or POSTGRES_HOST + POSTGRES_USER) not set; skipping Postgres integration test")
	}

	admin, err := sql.Open("postgres", baseDSN)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	t.Cleanup(func() { admin.Close() })

	name = fmt.Sprintf("ctxt_mig_%d_%04d", time.Now().UnixNano(), rand.Intn(10000))
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create fresh database: %v", err)
	}
	t.Cleanup(func() {
		// Terminate stragglers, then drop. Best-effort; the test DB is
		// disposable either way.
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name)
	})

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse base DSN: %v", err)
	}
	u.Path = "/" + name
	return u.String(), name
}

func pgColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	if err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	return exists
}

// TestPostgres_MigrationLedger_FreshDatabase pins the versioned ledger:
// a fresh database migrates cleanly, records every entry in schema_version,
// and carries the columns that the unversioned bootstrap historically lost
// (source_key, profile_id).
func TestPostgres_MigrationLedger_FreshDatabase(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	db := drv.DB()

	var maxVersion, rows int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0), COUNT(*) FROM schema_version`).Scan(&maxVersion, &rows); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if maxVersion == 0 {
		t.Fatal("schema_version ledger is empty after Migrate")
	}
	if maxVersion != rows {
		t.Errorf("ledger holes: max version %d but %d rows", maxVersion, rows)
	}

	for _, col := range []string{"source_key", "profile_id"} {
		if !pgColumnExists(t, db, "objects", col) {
			t.Errorf("objects.%s missing after fresh migration", col)
		}
	}
}

// TestPostgres_MigrationLedger_Reentrant asserts a second Migrate is a no-op:
// no error, no version growth, no re-application.
func TestPostgres_MigrationLedger_Reentrant(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	db := drv.DB()

	var before int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&before); err != nil {
		t.Fatalf("read version before: %v", err)
	}
	if err := drv.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var after int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&after); err != nil {
		t.Fatalf("read version after: %v", err)
	}
	if before != after {
		t.Errorf("re-run grew the ledger: %d -> %d", before, after)
	}
}

// TestPostgres_MigrationLedger_AdoptsPreLedgerDatabase simulates a database
// initialized before the ledger existed (schema present, no schema_version):
// Migrate must replay the idempotent entries without error and adopt the
// database into the ledger.
func TestPostgres_MigrationLedger_AdoptsPreLedgerDatabase(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	db := drv.DB()

	if _, err := db.Exec(`DROP TABLE schema_version`); err != nil {
		t.Fatalf("drop schema_version: %v", err)
	}
	if err := drv.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate over pre-ledger database: %v", err)
	}
	var maxVersion int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&maxVersion); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if maxVersion == 0 {
		t.Error("pre-ledger database not adopted into the ledger")
	}
}

// TestPostgres_FreshDatabase_ObjectRoundtrip proves the original fresh-DB
// failure is gone: Create + Get + GetBySourceKey work on a database that has
// only ever seen the versioned migration chain.
func TestPostgres_FreshDatabase_ObjectRoundtrip(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	ko := &pluginapi.KnowledgeObject{
		ID:        "fresh-rt-1",
		Type:      "note",
		Status:    "active",
		SourceKey: "slack-ts-1234.5678",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := drv.Objects().Create(ctx, ko); err != nil {
		t.Fatalf("Create on fresh database: %v", err)
	}
	got, err := drv.Objects().Get(ctx, ko.ID)
	if err != nil {
		t.Fatalf("Get on fresh database: %v", err)
	}
	if got.SourceKey != ko.SourceKey {
		t.Errorf("source_key round-trip: got %q want %q", got.SourceKey, ko.SourceKey)
	}
	bySK, err := drv.Objects().GetBySourceKey(ctx, ko.SourceKey)
	if err != nil {
		t.Fatalf("GetBySourceKey: %v", err)
	}
	if bySK == nil || bySK.ID != ko.ID {
		t.Errorf("GetBySourceKey: got %+v, want id %q", bySK, ko.ID)
	}

	// The source-key dedup index must reject duplicates of a non-empty key.
	dup := &pluginapi.KnowledgeObject{
		ID: "fresh-rt-2", Type: "note", Status: "active",
		SourceKey: ko.SourceKey,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := drv.Objects().Create(ctx, dup); err == nil {
		t.Error("duplicate non-empty source_key accepted; unique index missing")
	} else if !strings.Contains(err.Error(), "idx_objects_source_key") &&
		!strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Errorf("unexpected duplicate source_key error: %v", err)
	}
}

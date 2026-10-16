//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/lib/pq"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

const contentHashProfileIndex = "idx_objects_content_hash_profile"

// pgContentHashIndexDef returns the definition of the per-profile
// content-hash index, "" when it is absent.
func pgContentHashIndexDef(t *testing.T, db *sql.DB) string {
	t.Helper()
	var def string
	err := db.QueryRow(`SELECT indexdef FROM pg_indexes WHERE indexname = $1`, contentHashProfileIndex).Scan(&def)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("read %s: %v", contentHashProfileIndex, err)
	}
	return def
}

// assertContentHashIndex fails unless the index is unique on
// (content_hash, profile_id) and partial over non-empty hashes.
func assertContentHashIndex(t *testing.T, db *sql.DB) {
	t.Helper()
	def := pgContentHashIndexDef(t, db)
	for _, want := range []string{"UNIQUE INDEX", "(content_hash, profile_id)", "WHERE (content_hash <> ''::text)"} {
		if !strings.Contains(def, want) {
			t.Errorf("%s = %q, want it to contain %q", contentHashProfileIndex, def, want)
		}
	}
}

// seedPgObject inserts an object row with SQL, the columns every
// schema since the baseline has, so a historic database is seeded
// without the current store.
func seedPgObject(t *testing.T, db *sql.DB, id, hash, profile string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO objects (id, type, content_hash, profile_id, created_at, updated_at)
		VALUES ($1, 'note', $2, $3, NOW(), NOW())`, id, hash, profile); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func pgSchemaVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	return v
}

// pgAt33 is a database migrated through 33, with its driver.
func pgAt33(t *testing.T) (*pgdrv.Driver, string) {
	t.Helper()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.MigrateThroughForTest(context.Background(), 33); err != nil {
		t.Fatalf("migrate through 33: %v", err)
	}
	return drv, dsn
}

// TestPgMigration034_ContentHashPerProfile mirrors SQLite 052: a fresh
// install and an upgrade from 33 get the unique (content_hash,
// profile_id) index over non-empty hashes; a replay is a no-op; a
// database already holding same-profile duplicates is refused, named
// and left at 33 untouched until they are resolved.
func TestPgMigration034_ContentHashPerProfile(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh install", func(t *testing.T) {
		drv, _ := freshIntegrationDriver(t)
		assertContentHashIndex(t, drv.DB())
		if v := pgSchemaVersion(t, drv.DB()); v < 34 {
			t.Errorf("schema version = %d, want at least 34", v)
		}
	})

	t.Run("upgrade from 33", func(t *testing.T) {
		drv, _ := pgAt33(t)
		db := drv.DB()
		if def := pgContentHashIndexDef(t, db); def != "" {
			t.Fatalf("index present at 33: %s", def)
		}
		seedPgObject(t, db, "m34-p1", "sha256:shared", "p1")
		seedPgObject(t, db, "m34-p2", "sha256:shared", "p2")
		seedPgObject(t, db, "m34-nohash-1", "", "p1")
		seedPgObject(t, db, "m34-nohash-2", "", "p1")
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		assertContentHashIndex(t, db)

		_, err := db.Exec(`INSERT INTO objects (id, type, content_hash, profile_id, created_at, updated_at)
			VALUES ('m34-p1-again', 'note', 'sha256:shared', 'p1', NOW(), NOW())`)
		var pe *pq.Error
		if !errors.As(err, &pe) || pe.Code != "23505" {
			t.Errorf("same hash, same profile after 34 = %v, want a unique violation (23505)", err)
		}
		seedPgObject(t, db, "m34-nohash-3", "", "p1")

		// Every ledger entry must stay idempotent: a replay of 34 on a
		// database that has the index changes nothing.
		if err := drv.ReplayContentHashPerProfileForTest(ctx); err != nil {
			t.Fatalf("replay 34: %v", err)
		}
		assertContentHashIndex(t, db)
	})

	t.Run("upgrade from 33 with same-profile duplicates", func(t *testing.T) {
		drv, _ := pgAt33(t)
		db := drv.DB()
		seedPgObject(t, db, "m34-dup-a", "sha256:dup", "p1")
		seedPgObject(t, db, "m34-dup-b", "sha256:dup", "p1")
		seedPgObject(t, db, "m34-dup-c", "sha256:dup-unprofiled", "")
		seedPgObject(t, db, "m34-dup-d", "sha256:dup-unprofiled", "")
		seedPgObject(t, db, "m34-other-profile", "sha256:dup", "p2")

		err := drv.Migrate(ctx)
		if err == nil {
			t.Fatal("Migrate over same-profile duplicates succeeded")
		}
		msg := err.Error()
		for _, want := range []string{
			"apply migration 34", "2 content hashes",
			`profile "p1" hash "sha256:dup": m34-dup-a, m34-dup-b`,
			`profile "" hash "sha256:dup-unprofiled": m34-dup-c, m34-dup-d`,
			"keep one object per (content_hash, profile_id)",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("Migrate error = %q, want it to contain %q", msg, want)
			}
		}
		if strings.Contains(msg, "m34-other-profile") {
			t.Errorf("Migrate error names an object of another profile: %q", msg)
		}
		if v := pgSchemaVersion(t, db); v != 33 {
			t.Errorf("schema version after refusal = %d, want 33", v)
		}
		if def := pgContentHashIndexDef(t, db); def != "" {
			t.Errorf("index created despite the refusal: %s", def)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM objects WHERE id LIKE 'm34-%'`).Scan(&n); err != nil || n != 5 {
			t.Errorf("objects after refusal = %d, %v, want all 5 kept", n, err)
		}

		// The operator resolves them as the error says; the next start
		// applies 34.
		if _, err := db.Exec(`DELETE FROM objects WHERE id = 'm34-dup-b'`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE objects SET content_hash = '' WHERE id = 'm34-dup-d'`); err != nil {
			t.Fatal(err)
		}
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("Migrate after resolving the duplicates: %v", err)
		}
		assertContentHashIndex(t, db)
		if v := pgSchemaVersion(t, db); v < 34 {
			t.Errorf("schema version = %d, want at least 34", v)
		}
	})
}

//go:build integration

package postgres_test

import (
	"context"
	"sync"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
)

// TestPostgres_Migrate_ConcurrentInitializersSerialize pins the advisory
// lock around Migrate: several initializers pointed at the same fresh
// database must serialize behind pg_advisory_lock instead of racing the
// schema_version inserts (duplicate-key on the version PK) and the catalog
// DDL (concurrent CREATE TABLE IF NOT EXISTS can still collide on
// pg_type's unique index).
func TestPostgres_Migrate_ConcurrentInitializersSerialize(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)

	const n = 6
	drivers := make([]*pgdrv.Driver, n)
	for i := range drivers {
		drv, err := pgdrv.New(dsn)
		if err != nil {
			t.Fatalf("postgres.New(%d): %v", i, err)
		}
		t.Cleanup(func() { drv.Close(context.Background()) })
		drivers[i] = drv
	}

	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range drivers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = drivers[i].Migrate(context.Background())
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("initializer %d: %v", i, err)
		}
	}

	// The ledger must look exactly like a single sequential Migrate wrote
	// it: contiguous versions, no duplicates.
	db := drivers[0].DB()
	var maxVersion, rows, distinct int
	if err := db.QueryRow(`
		SELECT COALESCE(MAX(version), 0), COUNT(*), COUNT(DISTINCT version)
		  FROM schema_version`).Scan(&maxVersion, &rows, &distinct); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if maxVersion == 0 {
		t.Fatal("schema_version ledger empty after concurrent Migrate")
	}
	if rows != distinct {
		t.Errorf("duplicate ledger rows: %d rows, %d distinct versions", rows, distinct)
	}
	if maxVersion != rows {
		t.Errorf("ledger holes: max version %d but %d rows", maxVersion, rows)
	}
}

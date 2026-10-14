//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestPgMigration22_BackfillsChangeSeq migrates a schema-21 database
// holding objects and a federation watermark: existing rows get
// change_seq in (updated_at, id COLLATE "C") order, the counter resumes
// after the largest backfilled value, the trigger stamps later writes,
// and the watermark restarts from 0 with the old cursor columns gone.
func TestPgMigration22_BackfillsChangeSeq(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	ctx := context.Background()
	if err := drv.MigrateThroughForTest(ctx, 21); err != nil {
		t.Fatalf("migrate through 21: %v", err)
	}
	db := drv.DB()
	if pgColumnExists(t, db, "objects", "change_seq") {
		t.Fatal("schema 21 already has objects.change_seq")
	}

	base := seedSchema21ChangeOrder(t, db)

	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	wantOrder := []string{"z", "B", "a", "b"}
	for i, id := range wantOrder {
		obj, err := drv.Objects().Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if obj.ChangeSeq != int64(i+1) {
			t.Errorf("%s: change_seq = %d, want %d", id, obj.ChangeSeq, i+1)
		}
	}
	after := int64(0)
	listed, _, err := drv.Objects().List(ctx, storage.ObjectFilter{ChangedAfter: &after, Limit: 10})
	if err != nil {
		t.Fatalf("List(ChangedAfter 0): %v", err)
	}
	var got []string
	for _, o := range listed {
		got = append(got, o.ID)
	}
	if !slices.Equal(got, wantOrder) {
		t.Errorf("change order = %v, want %v", got, wantOrder)
	}

	assertChangeCounterResumes(t, drv, base)

	seq, err := drv.Watermarks().GetWatermark(ctx, "peer")
	if err != nil {
		t.Fatalf("GetWatermark: %v", err)
	}
	if seq != 0 {
		t.Errorf("watermark after migration = %d, want 0 (targets restart)", seq)
	}
	for _, col := range []string{"last_object_id", "last_synced_at"} {
		if pgColumnExists(t, db, "federation_watermarks", col) {
			t.Errorf("federation_watermarks.%s survived migration 22", col)
		}
	}
	if n := pgScalar(t, drv, `SELECT MAX(version) FROM schema_version`); n != pgdrv.LatestSchemaVersionForTest() {
		t.Errorf("ledger at %d, want %d", n, pgdrv.LatestSchemaVersionForTest())
	}
}

// seedSchema21ChangeOrder writes objects and a watermark the v21 way and
// returns the base timestamp they hang off.
func seedSchema21ChangeOrder(t *testing.T, db *sql.DB) time.Time {
	t.Helper()
	// Inserted out of change order. "B" and "a" share updated_at: byte
	// order ("C") puts "B" first, a linguistic collation would not.
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	seeds := []struct {
		id string
		at time.Time
	}{
		{"b", base.Add(2 * time.Hour)},
		{"a", base.Add(time.Hour)},
		{"B", base.Add(time.Hour)},
		{"z", base},
	}
	for _, s := range seeds {
		if _, err := db.Exec(`
			INSERT INTO objects (id, type, raw_content, created_at, updated_at)
			VALUES ($1, 'note', $1, $2, $2)`, s.id, s.at); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO federation_watermarks (federation_name, last_synced_at, last_object_id)
		VALUES ('peer', $1, 'b')`, base); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}
	return base
}

// assertChangeCounterResumes checks the counter continues after the four
// backfilled rows: a new row and an update take the next values.
func assertChangeCounterResumes(t *testing.T, drv *pgdrv.Driver, base time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "new", Type: "note", RawContent: "new", Status: "inbox", CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if n := pgScalar(t, drv, `SELECT change_seq FROM objects WHERE id = 'new'`); n != 5 {
		t.Errorf("new object change_seq = %d, want 5", n)
	}
	if _, err := drv.DB().Exec(`UPDATE objects SET raw_content = 'z2' WHERE id = 'z'`); err != nil {
		t.Fatalf("update z: %v", err)
	}
	if n := pgScalar(t, drv, `SELECT change_seq FROM objects WHERE id = 'z'`); n != 6 {
		t.Errorf("updated object change_seq = %d, want 6", n)
	}
}

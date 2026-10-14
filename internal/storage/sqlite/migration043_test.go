package sqlite

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestMigration043_BackfillsChangeSeqInChangeOrder pins the upgrade of a
// version-42 database: existing objects get change_seq in (updated_at as
// an instant, id) order, the next write continues after the largest, and
// watermarks restart from 0 on the new column.
func TestMigration043_BackfillsChangeSeqInChangeOrder(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v42.db"), 42)
	t.Cleanup(func() { d.Close(ctx) })

	edt := time.FixedZone("EDT", -4*3600)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	// Inserted out of change order; "late" is stamped in EDT so a text
	// compare would put it first.
	for _, o := range []struct {
		id string
		at time.Time
	}{
		{"m-late", base.Add(time.Hour).In(edt)},
		{"m-b", base.Add(time.Second)},
		{"m-first", base},
		{"m-a", base.Add(time.Second)},
	} {
		if err := d.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: o.id, Type: "note", RawContent: o.id, Status: "active",
			CreatedAt: o.at, UpdatedAt: o.at,
		}); err != nil {
			t.Fatalf("create %s at v42: %v", o.id, err)
		}
	}
	if _, err := d.db.ExecContext(ctx,
		`INSERT INTO federation_watermarks (federation_name, last_synced_at, last_object_id) VALUES ('old', ?, 'm-b')`,
		base.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed v42 watermark: %v", err)
	}

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var zero int64
	got, _, err := d.Objects().List(ctx, storage.ObjectFilter{Status: "all", ChangedAfter: &zero})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var ids []string
	var seqs []int64
	for _, o := range got {
		ids = append(ids, o.ID)
		seqs = append(seqs, o.ChangeSeq)
	}
	if want := []string{"m-first", "m-a", "m-b", "m-late"}; !slices.Equal(ids, want) {
		t.Errorf("backfilled order %v, want %v", ids, want)
	}
	if want := []int64{1, 2, 3, 4}; !slices.Equal(seqs, want) {
		t.Errorf("backfilled change_seq %v, want %v", seqs, want)
	}

	if err := d.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "m-new", Type: "note", RawContent: "new", Status: "active",
		CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatalf("create after migrate: %v", err)
	}
	if o, err := d.Objects().Get(ctx, "m-new"); err != nil || o.ChangeSeq != 5 {
		t.Errorf("next write ChangeSeq = %v (err %v), want 5", o.ChangeSeq, err)
	}

	if wm, err := d.Watermarks().GetWatermark(ctx, "old"); err != nil || wm != 0 {
		t.Errorf("old watermark = %d (err %v), want 0", wm, err)
	}
	for _, col := range []string{"last_synced_at", "last_object_id"} {
		if n := scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info('federation_watermarks') WHERE name = ?`, col); n != 0 {
			t.Errorf("federation_watermarks.%s still present", col)
		}
	}
}

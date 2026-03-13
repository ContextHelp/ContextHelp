package sqlite

import (
	"context"
	"testing"
)

func TestMigration008_TablesExist(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	for _, tbl := range []string{"watches", "watch_file_records"} {
		var name string
		err := d.db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q not found after migration 008: %v", tbl, err)
		}
	}
}

func TestMigration008_Idempotent(t *testing.T) {
	d := newTestDriver(t)
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("second Init: %v", err)
	}
}

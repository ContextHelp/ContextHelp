package sqlite

import (
	"context"
	"strings"
	"testing"
)

// corruptColumn rewrites one JSON column of an existing object row to a
// payload that is not valid JSON, simulating corrupt stored data.
func corruptColumn(t *testing.T, d *Driver, id, column, payload string) {
	t.Helper()
	if _, err := d.db.ExecContext(context.Background(),
		"UPDATE objects SET "+column+" = ? WHERE id = ?", payload, id); err != nil {
		t.Fatalf("corrupt %s: %v", column, err)
	}
}

// TestGet_CorruptJSONColumn_SurfacesError confirms a scan path reports a
// malformed stored column instead of silently yielding an empty field.
//
// Before the fix the decode error was dropped, so Get returned a valid
// object whose Tags were empty - indistinguishable from an object that
// genuinely had no tags.
func TestGet_CorruptJSONColumn_SurfacesError(t *testing.T) {
	for _, column := range []string{"tags", "sections", "mentions", "metadata"} {
		t.Run(column, func(t *testing.T) {
			d := newTestDriver(t)
			ctx := context.Background()

			obj := makeObject("obj-corrupt-"+column, "note")
			if err := d.Objects().Create(ctx, obj); err != nil {
				t.Fatalf("create: %v", err)
			}
			corruptColumn(t, d, obj.ID, column, "{not json")

			got, err := d.Objects().Get(ctx, obj.ID)
			if err == nil {
				t.Fatalf("Get returned no error for corrupt %s column; got object %+v", column, got)
			}
			if got != nil {
				t.Errorf("Get returned a non-nil object alongside the error: %+v", got)
			}
			if !strings.Contains(err.Error(), column) {
				t.Errorf("error should name the corrupt column %q, got: %v", column, err)
			}
		})
	}
}

// TestGet_EmptyJSONColumn_Tolerated confirms an absent column is still
// read as a zero value rather than an error. The JSON columns carry a
// DEFAULT but no NOT NULL, so NULL and legacy-empty rows must keep
// loading exactly as they did before.
func TestGet_EmptyJSONColumn_Tolerated(t *testing.T) {
	for _, column := range []string{"tags", "sections", "mentions", "metadata"} {
		t.Run(column, func(t *testing.T) {
			d := newTestDriver(t)
			ctx := context.Background()

			obj := makeObject("obj-empty-"+column, "note")
			if err := d.Objects().Create(ctx, obj); err != nil {
				t.Fatalf("create: %v", err)
			}
			corruptColumn(t, d, obj.ID, column, "")

			got, err := d.Objects().Get(ctx, obj.ID)
			if err != nil {
				t.Fatalf("Get errored on a legitimately empty %s column: %v", column, err)
			}
			if got.ID != obj.ID {
				t.Errorf("ID: got %q want %q", got.ID, obj.ID)
			}
		})
	}
}

// TestReinforce_CorruptTags_DoesNotOverwrite confirms Reinforce aborts on
// corrupt stored tags rather than merging against an empty slice and
// writing the result back, which would compound the corruption by
// destroying the tags it failed to read.
func TestReinforce_CorruptTags_DoesNotOverwrite(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	obj := makeObject("obj-reinforce-corrupt", "note")
	// Reinforce looks the row up by content hash and rejects an empty
	// one outright, so this must be set or the call never reaches the
	// decode under test.
	obj.ContentHash = "hash-reinforce-corrupt"
	if err := d.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Sanity check: with intact tags the same call succeeds, so the
	// error asserted below can only come from the corrupt payload.
	merge := makeObject("ignored", "note")
	if _, err := d.Objects().Reinforce(ctx, obj.ContentHash, merge); err != nil {
		t.Fatalf("Reinforce on intact tags: %v", err)
	}

	corruptColumn(t, d, obj.ID, "tags", "{not json")

	if _, err := d.Objects().Reinforce(ctx, obj.ContentHash, merge); err == nil {
		t.Fatal("Reinforce returned no error for corrupt stored tags")
	}

	// The corrupt payload must still be on disk: aborting is what keeps
	// the bad row recoverable instead of silently replacing it.
	var stored string
	if err := d.db.QueryRowContext(ctx,
		"SELECT tags FROM objects WHERE id = ?", obj.ID).Scan(&stored); err != nil {
		t.Fatalf("read back tags: %v", err)
	}
	if stored != "{not json" {
		t.Errorf("Reinforce overwrote the corrupt tags column: got %q", stored)
	}
}

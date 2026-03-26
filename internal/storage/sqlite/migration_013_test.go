package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestMigration012_ColumnsExist(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	for _, col := range []string{"remind_at", "reminded_at"} {
		var name string
		err := d.db.QueryRowContext(ctx,
			"SELECT name FROM pragma_table_info('objects') WHERE name=?", col,
		).Scan(&name)
		if err != nil {
			t.Fatalf("column %q not found after migration 012: %v", col, err)
		}
	}
}

func TestMigration012_SetAndClearReminder(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	// Create a minimal object.
	obj := makeObject("remind-test-id", "note")
	if err := d.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create object: %v", err)
	}

	// Set reminder.
	at := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	if err := d.Objects().SetReminder(ctx, obj.ID, at); err != nil {
		t.Fatalf("set reminder: %v", err)
	}

	// Retrieve and verify.
	got, err := d.Objects().Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	if got.RemindAt == nil {
		t.Fatal("RemindAt is nil after SetReminder")
	}
	if !got.RemindAt.Equal(at) {
		t.Errorf("RemindAt = %v, want %v", got.RemindAt, at)
	}
	if got.RemindedAt != nil {
		t.Error("RemindedAt should be nil after SetReminder")
	}

	// ListPendingReminders should include this object.
	pending, err := d.Objects().ListPendingReminders(ctx)
	if err != nil {
		t.Fatalf("list pending reminders: %v", err)
	}
	found := false
	for _, o := range pending {
		if o.ID == obj.ID {
			found = true
		}
	}
	if !found {
		t.Error("object not found in pending reminders")
	}

	// ListDueReminders with now far in the future should return it.
	due, err := d.Objects().ListDueReminders(ctx, at.Add(time.Minute))
	if err != nil {
		t.Fatalf("list due reminders: %v", err)
	}
	found = false
	for _, o := range due {
		if o.ID == obj.ID {
			found = true
		}
	}
	if !found {
		t.Error("object not in due reminders")
	}

	// MarkReminded.
	now := time.Now().Truncate(time.Second)
	if err := d.Objects().MarkReminded(ctx, obj.ID, now); err != nil {
		t.Fatalf("mark reminded: %v", err)
	}

	// Due reminders should no longer include this object.
	due2, err := d.Objects().ListDueReminders(ctx, at.Add(time.Minute))
	if err != nil {
		t.Fatalf("list due reminders after mark: %v", err)
	}
	for _, o := range due2 {
		if o.ID == obj.ID {
			t.Error("object still in due reminders after MarkReminded")
		}
	}

	// ClearReminder.
	if err := d.Objects().ClearReminder(ctx, obj.ID); err != nil {
		t.Fatalf("clear reminder: %v", err)
	}
	cleared, err := d.Objects().Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if cleared.RemindAt != nil {
		t.Error("RemindAt should be nil after ClearReminder")
	}
}

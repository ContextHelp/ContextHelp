package directopen

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func overrideEntries(t *testing.T, h *Handle) []*storage.AuditEntry {
	t.Helper()
	entries, _, err := h.Driver.AuditLog().List(context.Background(),
		storage.AuditFilter{EventType: EventMaintenanceOverride})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// The override opens a protected instance for maintenance, under the
// lock, without lowering its class, and leaves an audit entry naming
// the action.
func TestOverride_ProtectedAudited(t *testing.T) {
	db := migrated(t)
	if err := accessmarker.Write(db, accessmarker.Marker{Access: config.AccessProtected, Instance: "home"}); err != nil {
		t.Fatal(err)
	}
	h, err := Override(context.Background(), target(t, db), "housekeeping vacuum")
	if err != nil {
		t.Fatalf("Override: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	if err := dblock.Check(db); !errors.Is(err, dblock.ErrHeld) {
		t.Errorf("override does not hold the lock: %v", err)
	}
	entries := overrideEntries(t, h)
	if len(entries) != 1 {
		t.Fatalf("override audit entries = %d; want 1", len(entries))
	}
	p := entries[0].Payload
	if p["action"] != "housekeeping vacuum" || p["access"] != config.AccessProtected || p["instance"] != "home" {
		t.Errorf("audit payload %v", p)
	}
	if m, _, _ := accessmarker.Read(db); m.Access != config.AccessProtected {
		t.Errorf("override changed the access class to %q", m.Access)
	}
}

// The override never runs beside a live daemon: the lock decides.
func TestOverride_DaemonRunning(t *testing.T) {
	db := migrated(t)
	if err := accessmarker.Write(db, accessmarker.Marker{Access: config.AccessPublic, Instance: "home"}); err != nil {
		t.Fatal(err)
	}
	startHolder(t, db)
	_, err := Override(context.Background(), target(t, db), "backup")
	var ce *ContentionError
	if !errors.As(err, &ce) {
		t.Fatalf("Override with the daemon up = %v; want *ContentionError", err)
	}
}

// A private instance needs no override and gets no audit entry.
func TestOverride_PrivateNoEntry(t *testing.T) {
	h, err := Override(context.Background(), target(t, migrated(t)), "backup")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	if n := len(overrideEntries(t, h)); n != 0 {
		t.Errorf("private override wrote %d audit entries", n)
	}
}

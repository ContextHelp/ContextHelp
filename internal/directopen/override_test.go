package directopen

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/user"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
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

// The override is audited before anything else touches the database:
// a migration that fails still leaves the entry. The entry names the
// operator by the account os/user reports (a SID on Windows).
func TestOverride_AuditedBeforeMigration(t *testing.T) {
	db := migrated(t)
	if err := accessmarker.Write(db, accessmarker.Marker{Access: config.AccessProtected, Instance: "home"}); err != nil {
		t.Fatal(err)
	}
	// One migration pending, and its pre-migration snapshot cannot be
	// written: Init fails before migrating.
	raw, err := sql.Open("sqlite3", db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DELETE FROM schema_version WHERE version = (SELECT MAX(version) FROM schema_version)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	if err := os.WriteFile(sqlite.SnapshotDir(db), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tg := target(t, db)
	tg.MigrateSnapshot = true
	if _, err := Override(context.Background(), tg, "housekeeping vacuum"); err == nil {
		t.Fatal("override with a failing migration succeeded")
	}

	raw, err = sql.Open("sqlite3", db)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var actor string
	if err := raw.QueryRow(`SELECT actor FROM audit_log WHERE event_type = ?`, EventMaintenanceOverride).Scan(&actor); err != nil {
		t.Fatalf("no override audit entry after the failed migration: %v", err)
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(actor, u.Username) || !strings.Contains(actor, u.Uid) {
		t.Errorf("actor %q does not name %s (%s)", actor, u.Username, u.Uid)
	}
}

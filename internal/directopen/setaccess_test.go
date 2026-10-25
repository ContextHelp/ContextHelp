package directopen

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
)

func accessEntries(t *testing.T, db string) int {
	t.Helper()
	raw, err := sql.Open("sqlite3", db)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var n int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE event_type = ?`, EventAccessSet).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Changing the class is audited before the marker changes, and keeps
// the instance name.
func TestSetAccess_Audited(t *testing.T) {
	db := migrated(t)
	if err := accessmarker.Write(db, accessmarker.Marker{Access: config.AccessPublic, Instance: "home"}); err != nil {
		t.Fatal(err)
	}
	prev, err := SetAccess(context.Background(), target(t, db), config.AccessPrivate, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if prev.Access != config.AccessPublic {
		t.Errorf("previous class %q; want public", prev.Access)
	}
	m, _, _ := accessmarker.Read(db)
	if m.Access != config.AccessPrivate || m.Instance != "home" {
		t.Errorf("marker after SetAccess = %+v", m)
	}
	if n := accessEntries(t, db); n != 1 {
		t.Errorf("access audit entries = %d; want 1", n)
	}
}

// No audit, no change: an audit log that cannot be written leaves the
// class as it was.
func TestSetAccess_AuditFailureKeepsClass(t *testing.T) {
	db := migrated(t)
	if err := accessmarker.Write(db, accessmarker.Marker{Access: config.AccessProtected}); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite3", db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DROP TABLE audit_log`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	if _, err := SetAccess(context.Background(), target(t, db), config.AccessPrivate, "x"); err == nil {
		t.Fatal("SetAccess with no audit log succeeded")
	}
	if m, _, _ := accessmarker.Read(db); m.Access != config.AccessProtected {
		t.Errorf("class changed to %q without an audit entry", m.Access)
	}
}

// The daemon must be stopped, and the database must exist (its owner
// is who may change the class).
func TestSetAccess_Refusals(t *testing.T) {
	db := migrated(t)
	held, err := dblock.Acquire(db, dblock.Info{Role: dblock.RoleDaemon})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetAccess(context.Background(), target(t, db), config.AccessPrivate, "x"); !errors.Is(err, dblock.ErrHeld) {
		t.Errorf("SetAccess beside the daemon = %v; want contention", err)
	}
	_ = held.Release()

	missing := db + ".absent"
	if _, err := SetAccess(context.Background(), target(t, missing), config.AccessPrivate, "x"); err == nil {
		t.Error("SetAccess on a missing database succeeded")
	}
	if _, err := os.Stat(accessmarker.Path(missing)); !os.IsNotExist(err) {
		t.Errorf("refused SetAccess wrote a marker: %v", err)
	}
}

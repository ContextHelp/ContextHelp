package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestMigration041_UpgradeFrom40 migrates a schema-40 database, which
// has no session tables yet, to the latest version: 041 creates them
// with token_hash and 049 swaps it for identity_id.
func TestMigration041_UpgradeFrom40(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v40.db"), 40)
	t.Cleanup(func() { d.Close(ctx) })
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'ui_sessions'`); n != 0 {
		t.Fatal("schema 40 already has ui_sessions: nothing for 041 to create")
	}
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertSessionsBoundToIdentities(t, d)
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != latestMigrationVersion() {
		t.Errorf("schema_version after migrate = %d, want %d", n, latestMigrationVersion())
	}
}

// TestMigration041_ReplayAfterIdentityBinding replays the chain from 041
// on a fully migrated database, whose session tables already carry
// identity_id and no token_hash: the replay succeeds, keeps the tables'
// shape and keeps a session stored before it.
func TestMigration041_ReplayAfterIdentityBinding(t *testing.T) {
	ctx := context.Background()
	d := newTestDriver(t)
	idn := &storage.Identity{PrincipalID: storage.OwnerPrincipal, Method: storage.IdentityMethodAPIKey, Subject: "k1"}
	if err := d.Auth().Identities().Create(ctx, idn); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	at := time.Now().UTC()
	if err := d.UISessions().Create(ctx, &storage.UISession{
		ID: "uis_kept", SecretHash: "s-kept", PrincipalID: storage.OwnerPrincipal, IdentityID: idn.ID, Scope: "ui",
		CreatedAt: at, LastSeenAt: at, IdleExpiresAt: at.Add(time.Hour), ExpiresAt: at.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := d.db.ExecContext(ctx, `DELETE FROM schema_version WHERE version >= 41`); err != nil {
		t.Fatalf("rewind ledger: %v", err)
	}

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("replay from 041: %v", err)
	}
	assertSessionsBoundToIdentities(t, d)
	if _, err := d.UISessions().Get(ctx, "uis_kept"); err != nil {
		t.Errorf("session lost on replay: %v", err)
	}
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != latestMigrationVersion() {
		t.Errorf("schema_version after replay = %d, want %d", n, latestMigrationVersion())
	}
}

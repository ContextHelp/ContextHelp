package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// hasColumn reports whether table has a column named column.
func hasColumn(t *testing.T, d *Driver, table, column string) bool {
	t.Helper()
	return scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column) != 0
}

// TestMigration049_SessionsBoundToIdentities migrates a schema-48
// database holding static-token sessions: both tables lose token_hash
// for identity_id referencing principal_identities, the old rows are
// gone, a session of an unknown identity is refused, a re-run is a
// no-op and the ledger ends at the latest version.
func TestMigration049_SessionsBoundToIdentities(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v48.db"), 48)
	t.Cleanup(func() { d.Close(ctx) })

	now := time.Now().UnixMilli()
	if _, err := d.db.ExecContext(ctx, `INSERT INTO ui_sessions
		(id, secret_hash, principal_id, token_hash, scope, created_at, last_seen_at, idle_expires_at, expires_at)
		VALUES ('uis_old', 'sh', 'ops', 'th', 'ui', ?, ?, ?, ?)`, now, now, now+3600000, now+7200000); err != nil {
		t.Fatalf("seed schema-48 session: %v", err)
	}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO ui_login_codes
		(code_hash, principal_id, token_hash, scope, created_at, expires_at)
		VALUES ('ch', 'ops', 'th', 'ui', ?, ?)`, now, now+60000); err != nil {
		t.Fatalf("seed schema-48 login code: %v", err)
	}
	for _, table := range []string{"ui_sessions", "ui_login_codes"} {
		if !hasColumn(t, d, table, "token_hash") || hasColumn(t, d, table, "identity_id") {
			t.Fatalf("schema 48 %s: want token_hash and no identity_id", table)
		}
	}

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertSessionsBoundToIdentities(t, d)
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM ui_sessions`) + scalarInt(t, d, `SELECT COUNT(*) FROM ui_login_codes`); n != 0 {
		t.Errorf("static-token sessions and codes survived the rebuild: %d rows", n)
	}

	// A re-run keeps the rebuilt tables and whatever they hold.
	idn := &storage.Identity{PrincipalID: storage.OwnerPrincipal, Method: storage.IdentityMethodAPIKey, Subject: "k1"}
	if err := d.Auth().Identities().Create(ctx, idn); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	at := time.Now().UTC()
	if err := d.UISessions().Create(ctx, &storage.UISession{
		ID: "uis_new", SecretHash: "s-new", PrincipalID: storage.OwnerPrincipal, IdentityID: idn.ID, Scope: "ui",
		CreatedAt: at, LastSeenAt: at, IdleExpiresAt: at.Add(time.Hour), ExpiresAt: at.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := migrate049SessionsBoundToIdentities(ctx, d); err != nil {
		t.Fatalf("re-run 049: %v", err)
	}
	if _, err := d.UISessions().Get(ctx, "uis_new"); err != nil {
		t.Errorf("session lost on re-run: %v", err)
	}
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != latestMigrationVersion() {
		t.Errorf("schema_version after migrate = %d, want %d", n, latestMigrationVersion())
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM schema_version WHERE version = 49`); n != 1 {
		t.Errorf("ledger rows for 049: %d, want 1", n)
	}
}

// TestMigration049_FreshInstall pins that a fresh database binds
// sessions to identities.
func TestMigration049_FreshInstall(t *testing.T) {
	assertSessionsBoundToIdentities(t, newTestDriver(t))
}

// assertSessionsBoundToIdentities checks the column shape and that the
// foreign key is enforced: a session or code naming an identity that
// does not exist is refused.
func assertSessionsBoundToIdentities(t *testing.T, d *Driver) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"ui_sessions", "ui_login_codes"} {
		if hasColumn(t, d, table, "token_hash") || !hasColumn(t, d, table, "identity_id") {
			t.Errorf("%s: want identity_id and no token_hash", table)
		}
	}
	at := time.Now().UTC()
	if err := d.UISessions().Create(ctx, &storage.UISession{
		ID: "uis_orphan", SecretHash: "s-orphan", PrincipalID: storage.OwnerPrincipal, IdentityID: "idn_missing", Scope: "ui",
		CreatedAt: at, LastSeenAt: at, IdleExpiresAt: at.Add(time.Hour), ExpiresAt: at.Add(2 * time.Hour),
	}); err == nil {
		t.Error("a session of an unknown identity was stored")
	}
	if err := d.UISessions().CreateLoginCode(ctx, &storage.UILoginCode{
		CodeHash: "c-orphan", PrincipalID: storage.OwnerPrincipal, IdentityID: "idn_missing", Scope: "ui",
		CreatedAt: at, ExpiresAt: at.Add(time.Minute),
	}); err == nil {
		t.Error("a login code of an unknown identity was stored")
	}
}

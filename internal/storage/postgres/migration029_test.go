//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// assertSessionsBoundToIdentities checks the column shape and that the
// foreign key is enforced: a session or code naming an identity that
// does not exist is refused.
func assertSessionsBoundToIdentities(t *testing.T, drv *pgdrv.Driver) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"ui_sessions", "ui_login_codes"} {
		if pgColumnExists(t, drv.DB(), table, "token_hash") || !pgColumnExists(t, drv.DB(), table, "identity_id") {
			t.Errorf("%s: want identity_id and no token_hash", table)
		}
	}
	at := time.Now().UTC()
	if err := drv.UISessions().Create(ctx, &storage.UISession{
		ID: "uis_orphan", SecretHash: "s-orphan", PrincipalID: storage.OwnerPrincipal, IdentityID: "idn_missing", Scope: "ui",
		CreatedAt: at, LastSeenAt: at, IdleExpiresAt: at.Add(time.Hour), ExpiresAt: at.Add(2 * time.Hour),
	}); err == nil {
		t.Error("a session of an unknown identity was stored")
	}
	if err := drv.UISessions().CreateLoginCode(ctx, &storage.UILoginCode{
		CodeHash: "c-orphan", PrincipalID: storage.OwnerPrincipal, IdentityID: "idn_missing", Scope: "ui",
		CreatedAt: at, ExpiresAt: at.Add(time.Minute),
	}); err == nil {
		t.Error("a login code of an unknown identity was stored")
	}
}

// TestPgMigration029_SessionsBoundToIdentities pins that a fresh database
// and one migrated from schema 28 holding static-token sessions both end
// with identity_id referencing principal_identities, the old rows gone,
// and that replaying the chain keeps a session minted afterwards.
func TestPgMigration029_SessionsBoundToIdentities(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh install", func(t *testing.T) {
		drv, _ := freshIntegrationDriver(t)
		assertSessionsBoundToIdentities(t, drv)
	})

	t.Run("upgrade from 28", func(t *testing.T) {
		dsn, _ := freshDatabaseDSN(t)
		old, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		if err := old.MigrateThroughForTest(ctx, 28); err != nil {
			t.Fatalf("migrate through 28: %v", err)
		}
		now := time.Now().UTC()
		if _, err := old.DB().Exec(`INSERT INTO ui_sessions
			(id, secret_hash, principal_id, token_hash, scope, created_at, last_seen_at, idle_expires_at, expires_at)
			VALUES ('uis_old', 'sh', 'ops', 'th', 'ui', $1, $1, $2, $3)`, now, now.Add(time.Hour), now.Add(2*time.Hour)); err != nil {
			t.Fatalf("seed schema-28 session: %v", err)
		}
		if _, err := old.DB().Exec(`INSERT INTO ui_login_codes (code_hash, principal_id, token_hash, scope, created_at, expires_at)
			VALUES ('ch', 'ops', 'th', 'ui', $1, $2)`, now, now.Add(time.Minute)); err != nil {
			t.Fatalf("seed schema-28 login code: %v", err)
		}
		if !pgColumnExists(t, old.DB(), "ui_sessions", "token_hash") {
			t.Fatal("schema 28 ui_sessions lacks token_hash: nothing for 29 to swap")
		}
		if err := old.Close(ctx); err != nil {
			t.Fatal(err)
		}

		drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		t.Cleanup(func() { drv.Close(ctx) })
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		assertSessionsBoundToIdentities(t, drv)
		var rows int
		if err := drv.DB().QueryRow(`SELECT (SELECT COUNT(*) FROM ui_sessions) + (SELECT COUNT(*) FROM ui_login_codes)`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Errorf("static-token sessions and codes survived the swap: %d rows", rows)
		}

		idn := &storage.Identity{PrincipalID: storage.OwnerPrincipal, Method: storage.IdentityMethodAPIKey, Subject: "k1"}
		if err := drv.Auth().Identities().Create(ctx, idn); err != nil {
			t.Fatalf("create identity: %v", err)
		}
		at := time.Now().UTC()
		if err := drv.UISessions().Create(ctx, &storage.UISession{
			ID: "uis_new", SecretHash: "s-new", PrincipalID: storage.OwnerPrincipal, IdentityID: idn.ID, Scope: "ui",
			CreatedAt: at, LastSeenAt: at, IdleExpiresAt: at.Add(time.Hour), ExpiresAt: at.Add(2 * time.Hour),
		}); err != nil {
			t.Fatalf("create session: %v", err)
		}
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("replay Migrate: %v", err)
		}
		if _, err := drv.UISessions().Get(ctx, "uis_new"); err != nil {
			t.Errorf("session lost on replay: %v", err)
		}
		var version sql.NullInt64
		if err := drv.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
			t.Fatalf("read ledger: %v", err)
		}
		if int(version.Int64) != pgdrv.LatestSchemaVersionForTest() {
			t.Errorf("schema_version after migrate = %d, want %d", version.Int64, pgdrv.LatestSchemaVersionForTest())
		}
	})
}

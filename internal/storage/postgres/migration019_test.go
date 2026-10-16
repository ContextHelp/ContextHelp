//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestPgMigration019_UpgradeFrom18 migrates a schema-18 database, which
// has no session tables yet, to the latest version: 19 creates them with
// token_hash and 29 swaps it for identity_id. A second Migrate is a no-op.
func TestPgMigration019_UpgradeFrom18(t *testing.T) {
	ctx := context.Background()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(ctx) })
	if err := drv.MigrateThroughForTest(ctx, 18); err != nil {
		t.Fatalf("migrate through 18: %v", err)
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename = 'ui_sessions'`); n != 0 {
		t.Fatal("schema 18 already has ui_sessions: nothing for 19 to create")
	}
	for i := 0; i < 2; i++ {
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("Migrate #%d: %v", i+1, err)
		}
	}
	assertSessionsBoundToIdentities(t, drv)
	if n := pgScalar(t, drv, `SELECT MAX(version) FROM schema_version`); n != pgdrv.LatestSchemaVersionForTest() {
		t.Errorf("ledger at %d, want %d", n, pgdrv.LatestSchemaVersionForTest())
	}
}

// TestPgMigration019_ReplayAfterIdentityBinding replays the chain from 19
// on a fully migrated database, whose session tables already carry
// identity_id and no token_hash: the replay succeeds, keeps the tables'
// shape and keeps a session stored before it.
func TestPgMigration019_ReplayAfterIdentityBinding(t *testing.T) {
	ctx := context.Background()
	drv, _ := freshIntegrationDriver(t)
	idn := &storage.Identity{PrincipalID: storage.OwnerPrincipal, Method: storage.IdentityMethodAPIKey, Subject: "k1"}
	if err := drv.Auth().Identities().Create(ctx, idn); err != nil {
		t.Fatalf("create identity: %v", err)
	}
	at := time.Now().UTC()
	if err := drv.UISessions().Create(ctx, &storage.UISession{
		ID: "uis_kept", SecretHash: "s-kept", PrincipalID: storage.OwnerPrincipal, IdentityID: idn.ID, Scope: "ui",
		CreatedAt: at, LastSeenAt: at, IdleExpiresAt: at.Add(time.Hour), ExpiresAt: at.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := drv.DB().ExecContext(ctx, `DELETE FROM schema_version WHERE version >= 19`); err != nil {
		t.Fatalf("rewind ledger: %v", err)
	}

	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("replay from 19: %v", err)
	}
	assertSessionsBoundToIdentities(t, drv)
	if _, err := drv.UISessions().Get(ctx, "uis_kept"); err != nil {
		t.Errorf("session lost on replay: %v", err)
	}
	if n := pgScalar(t, drv, `SELECT MAX(version) FROM schema_version`); n != pgdrv.LatestSchemaVersionForTest() {
		t.Errorf("ledger at %d, want %d", n, pgdrv.LatestSchemaVersionForTest())
	}
}

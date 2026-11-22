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

// TestPgMigration045_UISessionElevation mirrors SQLite 064: a browser
// session stored before the elevation columns reads back unelevated, an
// elevation is stored and read back, and a replay is a no-op that keeps
// it.
func TestPgMigration045_UISessionElevation(t *testing.T) {
	ctx := context.Background()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.MigrateThroughForTest(ctx, 44); err != nil {
		t.Fatalf("migrate through 44: %v", err)
	}
	for _, col := range []string{"steward_grant_id", "steward_active_at"} {
		if pgColumnExists(t, drv.DB(), "ui_sessions", col) {
			t.Fatalf("schema 44 already has ui_sessions.%s: nothing for 45 to add", col)
		}
	}
	t0 := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	p := &storage.Principal{ID: "prn_mig045", Kind: storage.PrincipalKindPerson, DisplayName: "Steward", Roles: []string{"reader"}, CreatedAt: t0}
	if err := drv.Auth().Principals().Create(ctx, p); err != nil {
		t.Fatalf("seed principal: %v", err)
	}
	idn := &storage.Identity{PrincipalID: p.ID, Method: storage.IdentityMethodLink, Subject: "mig045", CreatedAt: t0}
	if err := drv.Auth().Identities().Create(ctx, idn); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	sess := &storage.UISession{
		ID: "uis_mig045", SecretHash: "uis_mig045" + "-hash", PrincipalID: p.ID, IdentityID: idn.ID, Scope: "ui",
		CreatedAt: t0, LastSeenAt: t0, IdleExpiresAt: t0.Add(time.Hour), ExpiresAt: t0.Add(24 * time.Hour),
	}
	if err := drv.UISessions().Create(ctx, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	got, err := drv.UISessions().Get(ctx, sess.ID)
	if err != nil || got.StewardGrantID != "" || got.StewardActiveAt != nil {
		t.Fatalf("session after 45 = %+v, %v; want it unelevated", got, err)
	}
	at := t0.Add(time.Minute)
	if err := drv.UISessions().Elevate(ctx, sess.ID, "stg_mig045", at); err != nil {
		t.Fatalf("Elevate after 45: %v", err)
	}
	check := func(stage string) {
		t.Helper()
		got, err := drv.UISessions().Get(ctx, sess.ID)
		if err != nil || got.StewardGrantID != "stg_mig045" || got.StewardActiveAt == nil || !got.StewardActiveAt.Equal(at) {
			t.Errorf("%s: session = %+v, %v; want elevated into stg_mig045 at %s", stage, got, err, at)
		}
	}
	check("after 45")
	if err := drv.ReplayStatementsForTest(ctx, 45); err != nil {
		t.Fatalf("replay 45: %v", err)
	}
	check("after replay")
}

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestMigration064_UISessionElevation migrates a schema-63 database
// holding a browser session: the elevation columns appear empty on it,
// an elevation is stored and read back, and a replay is a no-op that
// keeps it.
func TestMigration064_UISessionElevation(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v63.db"), 63)
	t.Cleanup(func() { d.Close(ctx) })
	for _, col := range []string{"steward_grant_id", "steward_active_at"} {
		if has, err := tableHasColumn(ctx, d, "ui_sessions", col); err != nil || has {
			t.Fatalf("schema 63 ui_sessions.%s present=%v err=%v: nothing for 064 to add", col, has, err)
		}
	}
	t0 := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	p := &storage.Principal{ID: "prn_mig064", Kind: storage.PrincipalKindPerson, DisplayName: "Steward", Roles: []string{"reader"}, CreatedAt: t0}
	if err := d.Auth().Principals().Create(ctx, p); err != nil {
		t.Fatalf("seed principal: %v", err)
	}
	idn := &storage.Identity{PrincipalID: p.ID, Method: storage.IdentityMethodLink, Subject: "mig064", CreatedAt: t0}
	if err := d.Auth().Identities().Create(ctx, idn); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	sess := &storage.UISession{
		ID: "uis_mig064", SecretHash: "uis_mig064" + "-hash", PrincipalID: p.ID, IdentityID: idn.ID, Scope: "ui",
		CreatedAt: t0, LastSeenAt: t0, IdleExpiresAt: t0.Add(time.Hour), ExpiresAt: t0.Add(24 * time.Hour),
	}
	if err := d.UISessions().Create(ctx, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := d.UISessions().Get(ctx, sess.ID)
	if err != nil || got.StewardGrantID != "" || got.StewardActiveAt != nil {
		t.Fatalf("session after 064 = %+v, %v; want it unelevated", got, err)
	}
	at := t0.Add(time.Minute)
	if err := d.UISessions().Elevate(ctx, sess.ID, "stg_mig064", at); err != nil {
		t.Fatalf("Elevate after 064: %v", err)
	}
	check := func(stage string) {
		t.Helper()
		got, err := d.UISessions().Get(ctx, sess.ID)
		if err != nil || got.StewardGrantID != "stg_mig064" || got.StewardActiveAt == nil || !got.StewardActiveAt.Equal(at) {
			t.Errorf("%s: session = %+v, %v; want elevated into stg_mig064 at %s", stage, got, err, at)
		}
	}
	check("after 064")
	if err := migrate064UISessionElevation(ctx, d); err != nil {
		t.Fatalf("re-run 064: %v", err)
	}
	check("after replay")
}

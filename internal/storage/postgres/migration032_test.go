//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// pgEntitlementKinds returns "kind:name" for every registry_entitlements
// row, sorted.
func pgEntitlementKinds(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT kind, registry_name FROM registry_entitlements`)
	if err != nil {
		t.Fatalf("select kinds: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kind, name string
		if err := rows.Scan(&kind, &name); err != nil {
			t.Fatal(err)
		}
		out = append(out, kind+":"+name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestPgMigration032_EntitlementKind mirrors SQLite 051: principal-shaped
// names become inbound only, every other name outbound plus an inbound
// copy; a fresh install keys on (kind, registry_name); a replay of 32
// leaves the rows alone.
func TestPgMigration032_EntitlementKind(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh install", func(t *testing.T) {
		drv, _ := freshIntegrationDriver(t)
		if _, err := drv.DB().Exec(`INSERT INTO registry_entitlements (kind, registry_name, plan, namespaces)
			VALUES ('sideways', 'x', '', '[]')`); err == nil {
			t.Error("schema accepted kind 'sideways'")
		}
		storagetestKindsSeparate(t, drv)
	})

	t.Run("upgrade from 30", func(t *testing.T) {
		dsn, _ := freshDatabaseDSN(t)
		old, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		if err := old.MigrateThroughForTest(ctx, 30); err != nil {
			t.Fatalf("migrate through 30: %v", err)
		}
		for _, r := range []struct{ name, plan, ns string }{
			{"owner", "inbound", `["ai.*"]`},
			{"prn_01JABCDEFGHJKMNPQRSTVWXYZ0", "inbound", `["ops.*"]`},
			{"phone", "inbound", `["public.*"]`},
			{"acme-registry", "pro", `["ai.*","devops.*"]`},
			{"PRN_upper", "basic", `["*"]`},
		} {
			if _, err := old.DB().ExecContext(ctx, `INSERT INTO registry_entitlements
				(registry_name, plan, namespaces, expires_at) VALUES ($1, $2, $3, '2031-06-01T00:00:00Z')`,
				r.name, r.plan, r.ns); err != nil {
				t.Fatalf("seed %s: %v", r.name, err)
			}
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

		want := []string{
			"inbound:PRN_upper", "inbound:acme-registry", "inbound:owner",
			"inbound:phone", "inbound:prn_01JABCDEFGHJKMNPQRSTVWXYZ0",
			"outbound:PRN_upper", "outbound:acme-registry", "outbound:phone",
		}
		if got := pgEntitlementKinds(t, drv.DB()); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("rows after 32:\n got  %v\n want %v", got, want)
		}
		acme, err := drv.Entitlements().Get(ctx, storage.EntitlementOutbound, "acme-registry")
		if err != nil {
			t.Fatalf("Get outbound acme: %v", err)
		}
		if acme.Plan != "pro" || len(acme.Namespaces) != 2 || acme.ExpiresAt.Year() != 2031 {
			t.Errorf("acme after 32 = %+v, fields not carried", acme)
		}

		// Every ledger entry must stay idempotent: a replay of 32 on a
		// table that already has kind changes nothing.
		if err := drv.ReplayEntitlementKindForTest(ctx); err != nil {
			t.Fatalf("replay 32: %v", err)
		}
		if got := pgEntitlementKinds(t, drv.DB()); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("rows after replay:\n got  %v\n want %v", got, want)
		}
		storagetestKindsSeparate(t, drv)
	})
}

// storagetestKindsSeparate pins that an outbound upsert does not touch
// an inbound row of the same name.
func storagetestKindsSeparate(t *testing.T, drv *pgdrv.Driver) {
	t.Helper()
	ctx := context.Background()
	ents := drv.Entitlements()
	if err := ents.Upsert(ctx, &storage.RegistryEntitlement{
		Kind: storage.EntitlementInbound, RegistryName: "same-name", Namespaces: []string{"in.only"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ents.Upsert(ctx, &storage.RegistryEntitlement{
		Kind: storage.EntitlementOutbound, RegistryName: "same-name", Namespaces: []string{"*"},
	}); err != nil {
		t.Fatal(err)
	}
	in, err := ents.Get(ctx, storage.EntitlementInbound, "same-name")
	if err != nil || len(in.Namespaces) != 1 || in.Namespaces[0] != "in.only" {
		t.Errorf("inbound after outbound upsert = %+v, %v", in, err)
	}
}

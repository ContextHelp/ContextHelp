package sqlite

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// entitlementKinds returns "kind:name" for every registry_entitlements
// row, sorted.
func entitlementKinds(t *testing.T, d *Driver) []string {
	t.Helper()
	rows, err := d.db.Query(`SELECT kind, registry_name FROM registry_entitlements`)
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

// seedV50Entitlements writes pre-051 rows: principal-shaped names, a
// static-token principal name, a registry name, and an upper-case PRN_
// name that is not principal-shaped.
func seedV50Entitlements(t *testing.T, d *Driver) {
	t.Helper()
	for _, r := range []struct{ name, plan, ns, expires string }{
		{"owner", "inbound", `["ai.*"]`, ""},
		{"prn_01JABCDEFGHJKMNPQRSTVWXYZ0", "inbound", `["ops.*"]`, "2030-01-01T00:00:00Z"},
		{"phone", "inbound", `["public.*"]`, ""},
		{"acme-registry", "pro", `["ai.*","devops.*"]`, "2031-06-01T00:00:00Z"},
		{"PRN_upper", "basic", `["*"]`, ""},
	} {
		var expires any
		if r.expires != "" {
			expires = r.expires
		}
		if _, err := d.db.Exec(`INSERT INTO registry_entitlements
			(registry_name, plan, namespaces, expires_at, fetched_at) VALUES (?, ?, ?, ?, '2026-10-01T00:00:00Z')`,
			r.name, r.plan, r.ns, expires); err != nil {
			t.Fatalf("seed %s: %v", r.name, err)
		}
	}
}

// migration051Rows is the backfill seedV50Entitlements must produce.
var migration051Rows = []string{
	"inbound:PRN_upper", "inbound:acme-registry", "inbound:owner",
	"inbound:phone", "inbound:prn_01JABCDEFGHJKMNPQRSTVWXYZ0",
	"outbound:PRN_upper", "outbound:acme-registry", "outbound:phone",
}

// assertMigration051Fields checks plan, namespaces and timestamps carry
// over to the re-keyed rows.
func assertMigration051Fields(t *testing.T, d *Driver) {
	t.Helper()
	ctx := context.Background()
	acme, err := d.Entitlements().Get(ctx, storage.EntitlementOutbound, "acme-registry")
	if err != nil {
		t.Fatalf("Get outbound acme: %v", err)
	}
	if acme.Plan != "pro" || len(acme.Namespaces) != 2 || acme.ExpiresAt.Year() != 2031 || acme.FetchedAt.Year() != 2026 {
		t.Errorf("acme after 051 = %+v, fields not carried", acme)
	}
	prn, err := d.Entitlements().Get(ctx, storage.EntitlementInbound, "prn_01JABCDEFGHJKMNPQRSTVWXYZ0")
	if err != nil {
		t.Fatalf("Get inbound prn: %v", err)
	}
	if len(prn.Namespaces) != 1 || prn.Namespaces[0] != "ops.*" || prn.ExpiresAt.Year() != 2030 {
		t.Errorf("prn after 051 = %+v", prn)
	}
}

// TestMigration051_EntitlementKind migrates a schema-50 database holding
// a mix of rows: principal-shaped names (owner, prn_*) become inbound
// only, any other name (a registry, or a static-token principal such as
// "phone") becomes outbound plus an inbound copy, so neither reader
// loses a row it used to see. Fields survive, a replay is a no-op and
// an outbound sync no longer overwrites a grant of the same name.
func TestMigration051_EntitlementKind(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v50.db"), 50)
	t.Cleanup(func() { d.Close(ctx) })

	if has, err := tableHasColumn(ctx, d, "registry_entitlements", "kind"); err != nil || has {
		t.Fatalf("schema 50 kind column present=%v err=%v: nothing for 051 to add", has, err)
	}
	seedV50Entitlements(t, d)
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	want := strings.Join(migration051Rows, ",")
	if got := strings.Join(entitlementKinds(t, d), ","); got != want {
		t.Errorf("rows after 051:\n got  %v\n want %v", got, want)
	}
	assertMigration051Fields(t, d)

	// Replay: the table already has kind, nothing changes.
	if err := migrate051EntitlementKind(ctx, d); err != nil {
		t.Fatalf("re-run 051: %v", err)
	}
	if got := strings.Join(entitlementKinds(t, d), ","); got != want {
		t.Errorf("rows after replay: %v", got)
	}

	// An outbound sync for "phone" leaves the inbound grant alone.
	if err := d.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
		Kind: storage.EntitlementOutbound, RegistryName: "phone", Plan: "remote", Namespaces: []string{"*"},
	}); err != nil {
		t.Fatalf("outbound upsert: %v", err)
	}
	phone, err := d.Entitlements().Get(ctx, storage.EntitlementInbound, "phone")
	if err != nil || len(phone.Namespaces) != 1 || phone.Namespaces[0] != "public.*" {
		t.Errorf("inbound phone after outbound upsert = %+v, %v", phone, err)
	}
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != latestMigrationVersion() {
		t.Errorf("schema_version = %d, want %d", n, latestMigrationVersion())
	}
}

// TestMigration051_FreshInstall pins that a fresh database keys
// registry_entitlements on (kind, registry_name) and refuses an unknown
// kind at the schema level.
func TestMigration051_FreshInstall(t *testing.T) {
	d := newTestDriver(t)
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info('registry_entitlements') WHERE pk > 0`); n != 2 {
		t.Errorf("primary key columns = %d, want 2 (kind, registry_name)", n)
	}
	if _, err := d.db.Exec(`INSERT INTO registry_entitlements (kind, registry_name, plan, namespaces)
		VALUES ('sideways', 'x', '', '[]')`); err == nil {
		t.Error("schema accepted kind 'sideways'")
	}
}

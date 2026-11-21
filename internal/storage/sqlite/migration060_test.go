package sqlite

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestMigration060_OwnerApprovalRoles migrates a schema-59 database,
// whose owner holds admin alone: the owner gains the approval roles,
// no other principal changes, and a replay keeps both.
func TestMigration060_OwnerApprovalRoles(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v59.db"), 59)
	t.Cleanup(func() { d.Close(ctx) })

	owner, err := d.Auth().Principals().Get(ctx, storage.OwnerPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(owner.Roles, []string{"admin"}) {
		t.Fatalf("schema 59 owner roles = %v: nothing for 060 to change", owner.Roles)
	}
	if err := d.Auth().Principals().Create(ctx, &storage.Principal{ID: "prn_admin", Kind: storage.PrincipalKindPerson, DisplayName: "Admin", Roles: []string{"admin"}}); err != nil {
		t.Fatal(err)
	}

	want := []string{"admin", "steward-requester", "steward-approver", "share-approver"}
	check := func(stage string) {
		t.Helper()
		owner, err := d.Auth().Principals().Get(ctx, storage.OwnerPrincipal)
		if err != nil || !slices.Equal(owner.Roles, want) {
			t.Errorf("%s: owner = %+v, %v; want roles %v", stage, owner, err, want)
		}
		admin, err := d.Auth().Principals().Get(ctx, "prn_admin")
		if err != nil || !slices.Equal(admin.Roles, []string{"admin"}) {
			t.Errorf("%s: a non-owner admin = %+v, %v; want its roles untouched", stage, admin, err)
		}
	}
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	check("migrated")
	if err := migrate060OwnerApprovalRoles(ctx, d); err != nil {
		t.Fatalf("re-run 060: %v", err)
	}
	check("replayed")
}

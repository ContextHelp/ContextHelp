//go:build integration

package postgres_test

import (
	"context"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// pgAt40 is a database migrated through 40, with its driver.
func pgAt40(t *testing.T) *pgdrv.Driver {
	t.Helper()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.MigrateThroughForTest(context.Background(), 40); err != nil {
		t.Fatalf("migrate through 40: %v", err)
	}
	return drv
}

// TestPgMigration041_OwnerApprovalRoles mirrors SQLite 060: the owner,
// which holds admin alone at 40, gains the approval roles; no other
// principal changes; a replay keeps both.
func TestPgMigration041_OwnerApprovalRoles(t *testing.T) {
	ctx := context.Background()
	drv := pgAt40(t)
	owner, err := drv.Auth().Principals().Get(ctx, storage.OwnerPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(owner.Roles, []string{"admin"}) {
		t.Fatalf("schema 40 owner roles = %v: nothing for 41 to change", owner.Roles)
	}
	if err := drv.Auth().Principals().Create(ctx, &storage.Principal{ID: "prn_admin", Kind: storage.PrincipalKindPerson, DisplayName: "Admin", Roles: []string{"admin"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"admin", "steward-requester", "steward-approver", "share-approver"}
	check := func(stage string) {
		t.Helper()
		owner, err := drv.Auth().Principals().Get(ctx, storage.OwnerPrincipal)
		if err != nil || !slices.Equal(owner.Roles, want) {
			t.Errorf("%s: owner = %+v, %v; want roles %v", stage, owner, err, want)
		}
		admin, err := drv.Auth().Principals().Get(ctx, "prn_admin")
		if err != nil || !slices.Equal(admin.Roles, []string{"admin"}) {
			t.Errorf("%s: a non-owner admin = %+v, %v; want its roles untouched", stage, admin, err)
		}
	}
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	check("after 41")
	if err := drv.ReplayStatementsForTest(ctx, 41); err != nil {
		t.Fatalf("replay 41: %v", err)
	}
	check("after replay")
}

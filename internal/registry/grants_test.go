package registry_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func newGrants(t *testing.T) (*registry.Grants, *registry.InboundGate, storage.StorageDriver) {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	return registry.NewGrants(driver.Entitlements()),
		registry.NewInboundGate(driver.Entitlements(), driver.Metering()), driver
}

func inboundRow(t *testing.T, driver storage.StorageDriver, principal string) *storage.RegistryEntitlement {
	t.Helper()
	rows, err := driver.Entitlements().List(context.Background(), storage.EntitlementInbound)
	if err != nil {
		t.Fatalf("list inbound: %v", err)
	}
	for _, r := range rows {
		if r.RegistryName == principal {
			return r
		}
	}
	return nil
}

// Grant creates the row on first use and unions namespaces on later
// ones; re-granting a namespace already held changes nothing.
func TestGrantsGrantIsAdditiveAndIdempotent(t *testing.T) {
	g, gate, driver := newGrants(t)
	ctx := context.Background()

	res, err := g.Grant(ctx, "partner", []string{"ai.*"}, registry.GrantOptions{}, false)
	if err != nil {
		t.Fatalf("first grant: %v", err)
	}
	if !res.Created || !res.Changed || strings.Join(res.Added, ",") != "ai.*" {
		t.Errorf("first grant = %+v, want created with ai.* added", res)
	}

	res, err = g.Grant(ctx, "partner", []string{"ai.*", "ops.k8s"}, registry.GrantOptions{}, false)
	if err != nil {
		t.Fatalf("second grant: %v", err)
	}
	if res.Created || !res.Changed || strings.Join(res.Added, ",") != "ops.k8s" || strings.Join(res.Unchanged, ",") != "ai.*" {
		t.Errorf("second grant = %+v, want ops.k8s added, ai.* unchanged", res)
	}
	if row := inboundRow(t, driver, "partner"); row == nil || strings.Join(row.Namespaces, ",") != "ai.*,ops.k8s" {
		t.Errorf("row = %+v, want [ai.* ops.k8s]", row)
	}

	res, err = g.Grant(ctx, "partner", []string{"ops.k8s"}, registry.GrantOptions{}, false)
	if err != nil {
		t.Fatalf("re-grant: %v", err)
	}
	if res.Changed || len(res.Added) != 0 {
		t.Errorf("re-grant = %+v, want no change", res)
	}

	if err := gate.Check(ctx, "partner", "ops.k8s"); err != nil {
		t.Errorf("granted namespace denied: %v", err)
	}
	if err := gate.Check(ctx, "partner", "med.records"); !registry.IsEntitlementRequired(err) {
		t.Errorf("ungranted namespace: %v, want ErrEntitlementRequired", err)
	}
}

// Grant writes inbound rows only: an outbound row of the same name is
// untouched, and the grant does not show up as outbound.
func TestGrantsWriteInboundOnly(t *testing.T) {
	g, _, driver := newGrants(t)
	ctx := context.Background()
	if err := driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
		Kind: storage.EntitlementOutbound, RegistryName: "partner", Plan: "remote", Namespaces: []string{"*"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Grant(ctx, "partner", []string{"ai.*"}, registry.GrantOptions{}, false); err != nil {
		t.Fatal(err)
	}
	out, err := driver.Entitlements().Get(ctx, storage.EntitlementOutbound, "partner")
	if err != nil || out.Plan != "remote" || strings.Join(out.Namespaces, ",") != "*" {
		t.Errorf("outbound row after grant = %+v, %v", out, err)
	}
	list, err := g.List(ctx)
	if err != nil || len(list) != 1 || list[0].Kind != storage.EntitlementInbound {
		t.Errorf("List = %+v, %v; want one inbound row", list, err)
	}
}

// --expires sets the expiry; a later grant without it keeps it; an
// expired grant denies everything.
func TestGrantsExpiry(t *testing.T) {
	g, gate, driver := newGrants(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)

	res, err := g.Grant(ctx, "partner", []string{"ai.*"}, registry.GrantOptions{ExpiresAt: past, SetExpiry: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Entitlement.ExpiresAt.Equal(past) {
		t.Errorf("expires = %v, want %v", res.Entitlement.ExpiresAt, past)
	}
	if _, err := g.Grant(ctx, "partner", []string{"ops.*"}, registry.GrantOptions{}, false); err != nil {
		t.Fatal(err)
	}
	if row := inboundRow(t, driver, "partner"); row == nil || !row.ExpiresAt.Equal(past) {
		t.Errorf("expiry after grant without --expires = %+v, want kept", row)
	}
	if err := gate.Check(ctx, "partner", "ai.models"); !registry.IsEntitlementRequired(err) {
		t.Errorf("expired grant: %v, want ErrEntitlementRequired", err)
	}

	// Clearing the expiry with the zero time: only the expiry changes.
	res, err = g.Grant(ctx, "partner", []string{"ai.*"}, registry.GrantOptions{SetExpiry: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || !res.Entitlement.ExpiresAt.IsZero() {
		t.Errorf("clear expiry = %+v, want changed with no expiry", res)
	}
	if err := gate.Check(ctx, "partner", "ai.models"); err != nil {
		t.Errorf("grant after clearing expiry: %v", err)
	}
}

// Revoke removes namespaces; removing the last keeps an empty row,
// which denies every gated read. Revoking what is not granted (or a
// principal with no grant) succeeds without a change.
func TestGrantsRevoke(t *testing.T) {
	g, gate, driver := newGrants(t)
	ctx := context.Background()

	res, err := g.Revoke(ctx, "nobody", []string{"ai.*"}, false)
	if err != nil {
		t.Fatalf("revoke without grant: %v", err)
	}
	if res.Changed || res.Entitlement != nil || strings.Join(res.Unchanged, ",") != "ai.*" {
		t.Errorf("revoke without grant = %+v, want no-op", res)
	}
	if inboundRow(t, driver, "nobody") != nil {
		t.Error("revoke without grant created a row")
	}

	if _, err := g.Grant(ctx, "partner", []string{"ai.*", "ops.*"}, registry.GrantOptions{}, false); err != nil {
		t.Fatal(err)
	}
	res, err = g.Revoke(ctx, "partner", []string{"ops.*", "med.*"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || strings.Join(res.Removed, ",") != "ops.*" || strings.Join(res.Unchanged, ",") != "med.*" {
		t.Errorf("revoke = %+v, want ops.* removed, med.* not granted", res)
	}

	res, err = g.Revoke(ctx, "partner", []string{"ai.*"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Entitlement == nil || len(res.Entitlement.Namespaces) != 0 {
		t.Errorf("revoke last = %+v, want an empty row kept", res)
	}
	row := inboundRow(t, driver, "partner")
	if row == nil || len(row.Namespaces) != 0 {
		t.Fatalf("row after revoking last = %+v, want empty row", row)
	}
	if err := gate.Check(ctx, "partner", "ai.models"); !registry.IsEntitlementRequired(err) {
		t.Errorf("empty grant: %v, want deny-all", err)
	}
}

// Ungate deletes the row: the principal is unrestricted again. Ungating
// a principal with no grant succeeds without a change.
func TestGrantsUngate(t *testing.T) {
	g, gate, driver := newGrants(t)
	ctx := context.Background()

	res, err := g.Ungate(ctx, "nobody", false)
	if err != nil || res.Changed {
		t.Errorf("ungate without grant = %+v, %v; want no-op", res, err)
	}

	if _, err := g.Grant(ctx, "partner", []string{"ai.*"}, registry.GrantOptions{}, false); err != nil {
		t.Fatal(err)
	}
	if err := driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
		Kind: storage.EntitlementOutbound, RegistryName: "partner", Namespaces: []string{"x"},
	}); err != nil {
		t.Fatal(err)
	}
	res, err = g.Ungate(ctx, "partner", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || strings.Join(res.Removed, ",") != "ai.*" {
		t.Errorf("ungate = %+v, want changed with ai.* dropped", res)
	}
	if inboundRow(t, driver, "partner") != nil {
		t.Error("inbound row still present after ungate")
	}
	if _, err := driver.Entitlements().Get(ctx, storage.EntitlementOutbound, "partner"); err != nil {
		t.Errorf("outbound row of the same name gone after ungate: %v", err)
	}
	if err := gate.Check(ctx, "partner", "med.records"); err != nil {
		t.Errorf("ungated principal denied: %v", err)
	}
}

// Dry run reports the change without writing it.
func TestGrantsDryRun(t *testing.T) {
	g, _, driver := newGrants(t)
	ctx := context.Background()

	res, err := g.Grant(ctx, "partner", []string{"ai.*"}, registry.GrantOptions{}, true)
	if err != nil || !res.Changed || !res.Created {
		t.Fatalf("dry-run grant = %+v, %v", res, err)
	}
	if inboundRow(t, driver, "partner") != nil {
		t.Fatal("dry-run grant wrote a row")
	}

	if _, err := g.Grant(ctx, "partner", []string{"ai.*"}, registry.GrantOptions{}, false); err != nil {
		t.Fatal(err)
	}
	if res, err := g.Revoke(ctx, "partner", []string{"ai.*"}, true); err != nil || !res.Changed {
		t.Fatalf("dry-run revoke = %+v, %v", res, err)
	}
	if res, err := g.Ungate(ctx, "partner", true); err != nil || !res.Changed {
		t.Fatalf("dry-run ungate = %+v, %v", res, err)
	}
	if row := inboundRow(t, driver, "partner"); row == nil || strings.Join(row.Namespaces, ",") != "ai.*" {
		t.Errorf("row after dry runs = %+v, want untouched", row)
	}
}

// Input is validated before anything is read or written.
func TestGrantsValidation(t *testing.T) {
	g, _, _ := newGrants(t)
	ctx := context.Background()
	for _, c := range []struct {
		principal string
		ns        []string
	}{
		{"", []string{"ai.*"}},
		{"  ", []string{"ai.*"}},
		{"partner", nil},
		{"partner", []string{""}},
		{"partner", []string{"ai.["}},
	} {
		if _, err := g.Grant(ctx, c.principal, c.ns, registry.GrantOptions{}, false); err == nil {
			t.Errorf("Grant(%q, %q) accepted", c.principal, c.ns)
		}
		if _, err := g.Revoke(ctx, c.principal, c.ns, false); err == nil {
			t.Errorf("Revoke(%q, %q) accepted", c.principal, c.ns)
		}
	}
	if _, err := g.Ungate(ctx, " ", false); err == nil {
		t.Error("Ungate of a blank principal accepted")
	}
}

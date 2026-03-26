package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

func newTestDriver(t *testing.T) *sqlite.Driver {
	t.Helper()
	drv, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("init driver: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	return drv
}

func TestEntitlementStore_UpsertAndGet(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	expiry := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	ent := &storage.RegistryEntitlement{
		RegistryName: "test-reg",
		Plan:         "pro",
		Namespaces:   []string{"ai.*", "devops.*"},
		ExpiresAt:    expiry,
		FetchedAt:    time.Now().UTC(),
	}

	if err := drv.Entitlements().Upsert(ctx, ent); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := drv.Entitlements().Get(ctx, "test-reg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.RegistryName != "test-reg" {
		t.Errorf("RegistryName: got %q, want %q", got.RegistryName, "test-reg")
	}
	if got.Plan != "pro" {
		t.Errorf("Plan: got %q, want %q", got.Plan, "pro")
	}
	if len(got.Namespaces) != 2 {
		t.Errorf("Namespaces count: got %d, want 2", len(got.Namespaces))
	}
	if got.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should not be zero")
	}
}

func TestEntitlementStore_Upsert_Overwrite(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	first := &storage.RegistryEntitlement{
		RegistryName: "reg",
		Plan:         "free",
		Namespaces:   []string{"public.*"},
	}
	if err := drv.Entitlements().Upsert(ctx, first); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}

	second := &storage.RegistryEntitlement{
		RegistryName: "reg",
		Plan:         "pro",
		Namespaces:   []string{"ai.*", "devops.*"},
	}
	if err := drv.Entitlements().Upsert(ctx, second); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	got, err := drv.Entitlements().Get(ctx, "reg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Plan != "pro" {
		t.Errorf("Plan after overwrite: got %q, want %q", got.Plan, "pro")
	}
	if len(got.Namespaces) != 2 {
		t.Errorf("Namespaces count after overwrite: got %d, want 2", len(got.Namespaces))
	}
}

func TestEntitlementStore_List(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		e := &storage.RegistryEntitlement{
			RegistryName: name,
			Plan:         "basic",
			Namespaces:   []string{"*"},
		}
		if err := drv.Entitlements().Upsert(ctx, e); err != nil {
			t.Fatalf("Upsert %s: %v", name, err)
		}
	}

	list, err := drv.Entitlements().List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("List count: got %d, want 3", len(list))
	}
}

func TestEntitlementStore_Get_NoRecord(t *testing.T) {
	drv := newTestDriver(t)
	ctx := context.Background()

	_, err := drv.Entitlements().Get(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent registry, got nil")
	}
}

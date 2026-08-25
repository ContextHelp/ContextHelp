package registry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func newInboundGate(t *testing.T) (*registry.InboundGate, storage.StorageDriver) {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	return registry.NewInboundGate(driver.Entitlements(), driver.Metering()), driver
}

func grantNamespaces(t *testing.T, driver storage.StorageDriver, principal string, namespaces ...string) {
	t.Helper()
	err := driver.Entitlements().Upsert(context.Background(), &storage.RegistryEntitlement{
		RegistryName: principal,
		Plan:         "inbound",
		Namespaces:   namespaces,
		FetchedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("upsert grant: %v", err)
	}
}

func usageTotal(t *testing.T, driver storage.StorageDriver, principal string) int {
	t.Helper()
	aggs, err := driver.Metering().Aggregate(context.Background(),
		storage.MeteringFilter{RegistryName: principal})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	total := 0
	for _, a := range aggs {
		total += a.Total
	}
	return total
}

// A principal with no stored grant is unrestricted — grants opt a
// principal INTO namespace gating. The access is still metered.
func TestInboundGateNoGrantAllowsAndMeters(t *testing.T) {
	gate, driver := newInboundGate(t)
	ctx := context.Background()

	if err := gate.Authorize(ctx, "partner", "ai.models", storage.MeteringEventEntityResolve); err != nil {
		t.Fatalf("Authorize without grant: %v", err)
	}
	if got := usageTotal(t, driver, "partner"); got != 1 {
		t.Errorf("usage = %d, want 1 metered event", got)
	}
}

// A stored grant restricts the principal to its namespace globs.
// Denied accesses are not metered.
func TestInboundGateGrantGatesNamespaces(t *testing.T) {
	gate, driver := newInboundGate(t)
	ctx := context.Background()
	grantNamespaces(t, driver, "partner", "ai.*")

	if err := gate.Authorize(ctx, "partner", "ai.models", storage.MeteringEventEntityResolve); err != nil {
		t.Fatalf("granted namespace: %v", err)
	}

	err := gate.Authorize(ctx, "partner", "med.records", storage.MeteringEventEntityResolve)
	if !registry.IsEntitlementRequired(err) {
		t.Fatalf("ungranted namespace: err = %v, want ErrEntitlementRequired", err)
	}

	if got := usageTotal(t, driver, "partner"); got != 1 {
		t.Errorf("usage = %d, want 1 (denied access must not meter)", got)
	}
}

// Check gates without metering — the read path for index filtering.
func TestInboundGateCheckDoesNotMeter(t *testing.T) {
	gate, driver := newInboundGate(t)
	ctx := context.Background()
	grantNamespaces(t, driver, "partner", "ai.*")

	if err := gate.Check(ctx, "partner", "ai.models"); err != nil {
		t.Fatalf("Check granted: %v", err)
	}
	if err := gate.Check(ctx, "partner", "med.records"); !registry.IsEntitlementRequired(err) {
		t.Fatalf("Check ungranted: err = %v, want ErrEntitlementRequired", err)
	}
	if got := usageTotal(t, driver, "partner"); got != 0 {
		t.Errorf("usage = %d, want 0 (Check never meters)", got)
	}
}

// Quota exhaustion surfaces ErrQuotaExhausted once the per-principal
// hard limit is reached within the billing period.
func TestInboundGateQuotaExhausted(t *testing.T) {
	gate, _ := newInboundGate(t)
	ctx := context.Background()
	gate.SetQuota("partner", storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 2})

	for i := range 2 {
		if err := gate.Authorize(ctx, "partner", "ai.models", storage.MeteringEventEntityResolve); err != nil {
			t.Fatalf("Authorize %d: %v", i+1, err)
		}
	}
	err := gate.Authorize(ctx, "partner", "ai.models", storage.MeteringEventEntityResolve)
	if !errors.Is(err, registry.ErrQuotaExhausted) {
		t.Fatalf("over limit: err = %v, want ErrQuotaExhausted", err)
	}
}

// Quotas and usage are isolated per principal.
func TestInboundGateQuotaPerPrincipal(t *testing.T) {
	gate, _ := newInboundGate(t)
	ctx := context.Background()
	gate.SetQuota("partner-a", storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 1})

	if err := gate.Authorize(ctx, "partner-a", "ai.models", storage.MeteringEventEntityResolve); err != nil {
		t.Fatalf("partner-a first: %v", err)
	}
	if err := gate.Authorize(ctx, "partner-a", "ai.models", storage.MeteringEventEntityResolve); !errors.Is(err, registry.ErrQuotaExhausted) {
		t.Fatalf("partner-a over limit: err = %v, want ErrQuotaExhausted", err)
	}

	// partner-b has no quota and its usage is not polluted by partner-a.
	for i := range 3 {
		if err := gate.Authorize(ctx, "partner-b", "ai.models", storage.MeteringEventEntityResolve); err != nil {
			t.Fatalf("partner-b %d: %v", i+1, err)
		}
	}
}

// A nil gate is a no-op allow — private instances never construct one.
func TestInboundGateNilIsNoop(t *testing.T) {
	var gate *registry.InboundGate
	ctx := context.Background()

	if err := gate.Check(ctx, "anyone", "ai.models"); err != nil {
		t.Fatalf("nil Check: %v", err)
	}
	if err := gate.Authorize(ctx, "anyone", "ai.models", storage.MeteringEventEntityResolve); err != nil {
		t.Fatalf("nil Authorize: %v", err)
	}
}

// The inbound gate meters fail CLOSED: a quota-usage read error denies
// the access instead of serving it unmetered.
func TestInboundGateFailsClosedOnMeteringReadError(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	gate := registry.NewInboundGate(driver.Entitlements(), &erroringMeteringStore{})
	gate.SetQuota("partner", storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 10})

	err := gate.Authorize(context.Background(), "partner", "ai.models",
		storage.MeteringEventEntityResolve)
	if err == nil {
		t.Fatal("expected error, got nil (inbound gate must fail closed on metering read error)")
	}
}

type erroringMeteringStore struct{}

func (e *erroringMeteringStore) Record(_ context.Context, _ *storage.MeteringEvent) error {
	return errors.New("metering store down")
}

func (e *erroringMeteringStore) RecordCapped(
	_ context.Context, _ *storage.MeteringEvent, _ time.Time, _ int,
) (bool, error) {
	return false, errors.New("metering store down")
}

func (e *erroringMeteringStore) Aggregate(
	_ context.Context, _ storage.MeteringFilter,
) ([]*storage.MeteringAggregate, error) {
	return nil, errors.New("metering store down")
}

func (e *erroringMeteringStore) List(
	_ context.Context, _ storage.MeteringFilter,
) ([]*storage.MeteringEvent, error) {
	return nil, errors.New("metering store down")
}

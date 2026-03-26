package registry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestMeterRecordEvent_HappyPath verifies that events are stored and
// aggregated correctly under the quota limit.
func TestMeterRecordEvent_HappyPath(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	meter := registry.NewMeter(driver.Metering())

	ctx := context.Background()
	err := meter.RecordEvent(ctx, "test-registry",
		storage.MeteringEventEntityResolve, "ai", time.Time{})
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	aggs, err := meter.UsageSummary(ctx, "test-registry", time.Time{})
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if len(aggs) != 1 {
		t.Fatalf("expected 1 aggregate, got %d", len(aggs))
	}
	if aggs[0].Total != 1 {
		t.Errorf("expected total=1, got %d", aggs[0].Total)
	}
	if aggs[0].EventType != storage.MeteringEventEntityResolve {
		t.Errorf("expected event_type=entity_resolve, got %s", aggs[0].EventType)
	}
}

// TestMeterRecordEvent_QuotaExhausted verifies ErrQuotaExhausted is returned
// when the hard limit is reached.
func TestMeterRecordEvent_QuotaExhausted(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	meter := registry.NewMeter(driver.Metering())

	meter.SetQuota("paid-reg", storage.MeteringEventContentPull, storage.QuotaConfig{
		Limit: 2,
	})

	ctx := context.Background()

	// Record up to limit.
	for i := 0; i < 2; i++ {
		if err := meter.RecordEvent(ctx, "paid-reg",
			storage.MeteringEventContentPull, "", time.Time{}); err != nil {
			t.Fatalf("RecordEvent %d: %v", i, err)
		}
	}

	// One more should fail.
	err := meter.RecordEvent(ctx, "paid-reg",
		storage.MeteringEventContentPull, "", time.Time{})
	if err == nil {
		t.Fatal("expected ErrQuotaExhausted, got nil")
	}
	if !errors.Is(err, registry.ErrQuotaExhausted) {
		t.Errorf("expected ErrQuotaExhausted, got: %v", err)
	}
}

// TestMeterRecordEvent_NoQuota verifies unlimited operation when no quota set.
func TestMeterRecordEvent_NoQuota(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	meter := registry.NewMeter(driver.Metering())

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := meter.RecordEvent(ctx, "free-reg",
			storage.MeteringEventTaxonomySync, "", time.Time{}); err != nil {
			t.Fatalf("RecordEvent %d: %v", i, err)
		}
	}

	aggs, err := meter.AllUsageSummary(ctx, time.Time{})
	if err != nil {
		t.Fatalf("AllUsageSummary: %v", err)
	}
	found := false
	for _, a := range aggs {
		if a.RegistryName == "free-reg" && a.EventType == storage.MeteringEventTaxonomySync {
			if a.Total != 5 {
				t.Errorf("expected total=5, got %d", a.Total)
			}
			found = true
		}
	}
	if !found {
		t.Error("did not find free-reg/taxonomy_sync in aggregates")
	}
}

// TestSQLiteMeteringStore_RecordAndAggregate verifies the SQLite store directly.
func TestSQLiteMeteringStore_RecordAndAggregate(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ms := driver.Metering()

	ctx := context.Background()
	events := []storage.MeteringEvent{
		{ID: uuid.New().String(), RegistryName: "r1", EventType: storage.MeteringEventEntityResolve, Count: 1, OccurredAt: time.Now()},
		{ID: uuid.New().String(), RegistryName: "r1", EventType: storage.MeteringEventEntityResolve, Count: 1, OccurredAt: time.Now()},
		{ID: uuid.New().String(), RegistryName: "r1", EventType: storage.MeteringEventContentPull, Count: 1, OccurredAt: time.Now()},
		{ID: uuid.New().String(), RegistryName: "r2", EventType: storage.MeteringEventEntityResolve, Count: 1, OccurredAt: time.Now()},
	}
	for _, e := range events {
		ee := e
		if err := ms.Record(ctx, &ee); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	aggs, err := ms.Aggregate(ctx, storage.MeteringFilter{RegistryName: "r1"})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	totals := make(map[storage.MeteringEventType]int)
	for _, a := range aggs {
		totals[a.EventType] = a.Total
	}
	if totals[storage.MeteringEventEntityResolve] != 2 {
		t.Errorf("expected entity_resolve=2, got %d", totals[storage.MeteringEventEntityResolve])
	}
	if totals[storage.MeteringEventContentPull] != 1 {
		t.Errorf("expected content_pull=1, got %d", totals[storage.MeteringEventContentPull])
	}

	// r2 must not appear in r1-filtered results.
	for _, a := range aggs {
		if a.RegistryName == "r2" {
			t.Error("r2 should not appear in r1-filtered aggregates")
		}
	}
}

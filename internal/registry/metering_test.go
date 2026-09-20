package registry_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

// flakyMeteringStore fails the atomic capped write while keeping the
// plain Record path working — models a usage-read failure.
type flakyMeteringStore struct {
	recordCalls int
	cappedErr   error
}

func (f *flakyMeteringStore) Record(_ context.Context, _ *storage.MeteringEvent) error {
	f.recordCalls++
	return nil
}

func (f *flakyMeteringStore) RecordCapped(
	_ context.Context, _ *storage.MeteringEvent, _ time.Time, _ int,
) (bool, error) {
	return false, f.cappedErr
}

func (f *flakyMeteringStore) Aggregate(
	_ context.Context, _ storage.MeteringFilter,
) ([]*storage.MeteringAggregate, error) {
	return nil, nil
}

func (f *flakyMeteringStore) List(
	_ context.Context, _ storage.MeteringFilter,
) ([]*storage.MeteringEvent, error) {
	return nil, nil
}

// TestMeterRecordEvent_ConcurrentNoOvershoot verifies the check-and-
// record is atomic: N concurrent recorders against limit L admit
// exactly L events, never more.
func TestMeterRecordEvent_ConcurrentNoOvershoot(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	meter := registry.NewMeter(driver.Metering())
	meter.SetFailClosed(true)
	meter.SetQuota("paid-reg", storage.MeteringEventContentPull, storage.QuotaConfig{Limit: 5})

	const workers = 20
	var wg sync.WaitGroup
	var allowed atomic.Int32
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := meter.RecordEvent(context.Background(), "paid-reg",
				storage.MeteringEventContentPull, "", time.Time{}); err == nil {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := allowed.Load(); got != 5 {
		t.Errorf("allowed = %d, want exactly 5 (no concurrent overshoot)", got)
	}
	aggs, err := driver.Metering().Aggregate(context.Background(),
		storage.MeteringFilter{RegistryName: "paid-reg"})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	total := 0
	for _, a := range aggs {
		total += a.Total
	}
	if total != 5 {
		t.Errorf("stored usage = %d, want exactly 5", total)
	}
}

// A fail-closed meter denies when the usage check errors — the event is
// neither served nor recorded unmetered.
func TestMeterRecordEvent_FailClosedOnReadError(t *testing.T) {
	store := &flakyMeteringStore{cappedErr: errors.New("disk exploded")}
	meter := registry.NewMeter(store)
	meter.SetFailClosed(true)
	meter.SetQuota("partner", storage.MeteringEventEntityResolve, storage.QuotaConfig{Limit: 10})

	err := meter.RecordEvent(context.Background(), "partner",
		storage.MeteringEventEntityResolve, "ai", time.Time{})
	if err == nil {
		t.Fatal("expected error, got nil (fail-closed meter must deny on read error)")
	}
	if errors.Is(err, registry.ErrQuotaExhausted) {
		t.Errorf("read error must not masquerade as quota exhaustion: %v", err)
	}
	if store.recordCalls != 0 {
		t.Errorf("recordCalls = %d, want 0 (denied event must not be recorded)", store.recordCalls)
	}
}

// The default (advisory, outbound) meter keeps its historical fail-open
// behavior: a usage-read failure logs and records the event unchecked.
func TestMeterRecordEvent_AdvisoryProceedsOnReadError(t *testing.T) {
	store := &flakyMeteringStore{cappedErr: errors.New("disk exploded")}
	meter := registry.NewMeter(store)
	meter.SetQuota("outbound-reg", storage.MeteringEventContentPull, storage.QuotaConfig{Limit: 10})

	err := meter.RecordEvent(context.Background(), "outbound-reg",
		storage.MeteringEventContentPull, "", time.Time{})
	if err != nil {
		t.Fatalf("advisory meter must proceed on read error, got: %v", err)
	}
	if store.recordCalls != 1 {
		t.Errorf("recordCalls = %d, want 1 (advisory event still recorded)", store.recordCalls)
	}
}

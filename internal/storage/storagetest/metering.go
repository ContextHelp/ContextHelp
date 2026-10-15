package storagetest

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// meterT0 is the metering conformance clock origin. Every fixture time is
// a whole second: the SQLite driver stores RFC3339 seconds.
var meterT0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// MeteringConformance pins the MeteringStore contract every driver
// implements: append and list round trip, filter fidelity, aggregation
// per registry and event type, and RecordCapped's atomic cap (summed
// counts within the period, scoped to one registry and event type, never
// overshot by concurrent recorders). The inbound entitlement gate meters
// every non-admin entity read through this store and fails closed, so
// there is no parity-gap skip: a driver without it cannot serve a
// protected instance.
func MeteringConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	if drv.Metering() == nil {
		t.Fatal("Metering() returned nil")
	}
	for _, c := range []struct {
		name string
		fn   func(*testing.T, storage.MeteringStore)
	}{
		{"empty store", meterConfEmpty},
		{"record and list round trip", meterConfRoundTrip},
		{"zero occurred_at is stamped now", meterConfZeroOccurredAt},
		{"duplicate id is rejected", meterConfDuplicateID},
		{"list filters and order", meterConfListFilters},
		{"aggregate sums per registry and event", meterConfAggregate},
		{"record capped under the cap", meterConfCappedUnder},
		{"record capped at the cap", meterConfCappedAtCap},
		{"record capped sums counts", meterConfCappedSumsCounts},
		{"record capped honours the period", meterConfCappedPeriod},
		{"record capped scopes registry and event", meterConfCappedScope},
		{"concurrent record capped never overshoots", meterConfCappedConcurrent},
	} {
		t.Run(c.name, func(t *testing.T) { c.fn(t, drv.Metering()) })
	}
}

func meterEvent(id, registry string, et storage.MeteringEventType, ns string, count int, at time.Time) *storage.MeteringEvent {
	return &storage.MeteringEvent{
		ID: id, RegistryName: registry, EventType: et, Namespace: ns, Count: count, OccurredAt: at,
	}
}

func meterRecord(t *testing.T, st storage.MeteringStore, events ...*storage.MeteringEvent) {
	t.Helper()
	for _, e := range events {
		if err := st.Record(context.Background(), e); err != nil {
			t.Fatalf("Record(%s): %v", e.ID, err)
		}
	}
}

func meterListIDs(t *testing.T, st storage.MeteringStore, f storage.MeteringFilter) []string {
	t.Helper()
	events, err := st.List(context.Background(), f)
	if err != nil {
		t.Fatalf("List(%+v): %v", f, err)
	}
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	return ids
}

// meterTotal is the Aggregate total for one registry and event type,
// 0 when no row matches.
func meterTotal(t *testing.T, st storage.MeteringStore, registry string, et storage.MeteringEventType) int {
	t.Helper()
	aggs, err := st.Aggregate(context.Background(), storage.MeteringFilter{RegistryName: registry, EventType: et})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	total := 0
	for _, a := range aggs {
		if a.RegistryName == registry && a.EventType == et {
			total += a.Total
		}
	}
	return total
}

func meterConfEmpty(t *testing.T, st storage.MeteringStore) {
	ctx := context.Background()
	f := storage.MeteringFilter{RegistryName: "meter-empty"}
	events, err := st.List(ctx, f)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("List on an unused registry = %d events, want 0", len(events))
	}
	aggs, err := st.Aggregate(ctx, f)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(aggs) != 0 {
		t.Errorf("Aggregate on an unused registry = %d rows, want 0", len(aggs))
	}
}

func meterConfRoundTrip(t *testing.T, st storage.MeteringStore) {
	withNS := meterEvent("meter-rt-1", "meter-rt", storage.MeteringEventContentPull, "ai", 3, meterT0)
	noNS := meterEvent("meter-rt-2", "meter-rt", storage.MeteringEventEntityResolve, "", 1, meterT0.Add(time.Minute))
	meterRecord(t, st, withNS, noNS)

	events, err := st.List(context.Background(), storage.MeteringFilter{RegistryName: "meter-rt"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("List = %d events, want 2", len(events))
	}
	byID := map[string]*storage.MeteringEvent{}
	for _, e := range events {
		byID[e.ID] = e
	}
	for _, want := range []*storage.MeteringEvent{withNS, noNS} {
		got := byID[want.ID]
		if got == nil {
			t.Errorf("event %s missing from List", want.ID)
			continue
		}
		if got.RegistryName != want.RegistryName || got.EventType != want.EventType ||
			got.Namespace != want.Namespace || got.Count != want.Count || !got.OccurredAt.Equal(want.OccurredAt) {
			t.Errorf("event %s = %+v, want %+v", want.ID, got, want)
		}
		if got.OccurredAt.Location() != time.UTC {
			t.Errorf("event %s occurred_at location = %v, want UTC", want.ID, got.OccurredAt.Location())
		}
	}
}

func meterConfZeroOccurredAt(t *testing.T, st storage.MeteringStore) {
	before := time.Now().UTC().Add(-2 * time.Second)
	meterRecord(t, st, meterEvent("meter-now-1", "meter-now", storage.MeteringEventEntityResolve, "", 1, time.Time{}))
	after := time.Now().UTC().Add(2 * time.Second)

	events, err := st.List(context.Background(), storage.MeteringFilter{RegistryName: "meter-now"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("List = %d events, want 1", len(events))
	}
	if at := events[0].OccurredAt; at.Before(before) || at.After(after) {
		t.Errorf("occurred_at = %v, want within [%v, %v]", at, before, after)
	}
}

func meterConfDuplicateID(t *testing.T, st storage.MeteringStore) {
	meterRecord(t, st, meterEvent("meter-dup-1", "meter-dup", storage.MeteringEventEntityResolve, "", 1, meterT0))
	err := st.Record(context.Background(), meterEvent("meter-dup-1", "meter-dup", storage.MeteringEventEntityResolve, "", 1, meterT0))
	if err == nil {
		t.Fatal("Record with a duplicate id succeeded, want an error")
	}
	assertIDs(t, "events after duplicate", meterListIDs(t, st, storage.MeteringFilter{RegistryName: "meter-dup"}),
		[]string{"meter-dup-1"})
}

func meterConfListFilters(t *testing.T, st storage.MeteringStore) {
	meterRecord(t, st,
		meterEvent("meter-lf-1", "meter-lf", storage.MeteringEventEntityResolve, "ai", 1, meterT0),
		meterEvent("meter-lf-2", "meter-lf", storage.MeteringEventContentPull, "ai", 1, meterT0.Add(1*time.Hour)),
		meterEvent("meter-lf-3", "meter-lf", storage.MeteringEventEntityResolve, "med", 1, meterT0.Add(2*time.Hour)),
		meterEvent("meter-lf-4", "meter-lf-other", storage.MeteringEventEntityResolve, "ai", 1, meterT0.Add(3*time.Hour)),
	)

	// Newest first.
	assertIDs(t, "registry filter", meterListIDs(t, st, storage.MeteringFilter{RegistryName: "meter-lf"}),
		[]string{"meter-lf-3", "meter-lf-2", "meter-lf-1"})
	assertIDs(t, "event type filter", meterListIDs(t, st, storage.MeteringFilter{
		RegistryName: "meter-lf", EventType: storage.MeteringEventEntityResolve,
	}), []string{"meter-lf-3", "meter-lf-1"})
	// After and Before are inclusive bounds.
	assertIDs(t, "after filter", meterListIDs(t, st, storage.MeteringFilter{
		RegistryName: "meter-lf", After: meterT0.Add(1 * time.Hour),
	}), []string{"meter-lf-3", "meter-lf-2"})
	assertIDs(t, "before filter", meterListIDs(t, st, storage.MeteringFilter{
		RegistryName: "meter-lf", Before: meterT0.Add(1 * time.Hour),
	}), []string{"meter-lf-2", "meter-lf-1"})
	assertIDs(t, "after and before", meterListIDs(t, st, storage.MeteringFilter{
		RegistryName: "meter-lf", After: meterT0.Add(30 * time.Minute), Before: meterT0.Add(90 * time.Minute),
	}), []string{"meter-lf-2"})
}

func meterConfAggregate(t *testing.T, st storage.MeteringStore) {
	ctx := context.Background()
	meterRecord(t, st,
		meterEvent("meter-ag-1", "meter-ag-b", storage.MeteringEventEntityResolve, "", 2, meterT0),
		meterEvent("meter-ag-2", "meter-ag-b", storage.MeteringEventEntityResolve, "", 3, meterT0.Add(time.Hour)),
		meterEvent("meter-ag-3", "meter-ag-b", storage.MeteringEventContentPull, "", 1, meterT0.Add(time.Hour)),
		meterEvent("meter-ag-4", "meter-ag-a", storage.MeteringEventTaxonomySync, "", 4, meterT0.Add(2*time.Hour)),
	)

	render := func(aggs []*storage.MeteringAggregate) []string {
		out := []string{}
		for _, a := range aggs {
			if a.RegistryName == "meter-ag-a" || a.RegistryName == "meter-ag-b" {
				out = append(out, fmt.Sprintf("%s/%s=%d", a.RegistryName, a.EventType, a.Total))
			}
		}
		return out
	}

	// Grouped by registry then event type, ordered by both.
	all, err := st.Aggregate(ctx, storage.MeteringFilter{})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	assertIDs(t, "aggregate", render(all), []string{
		"meter-ag-a/taxonomy_sync=4",
		"meter-ag-b/content_pull=1",
		"meter-ag-b/entity_resolve=5",
	})

	byRegistry, err := st.Aggregate(ctx, storage.MeteringFilter{RegistryName: "meter-ag-b"})
	if err != nil {
		t.Fatalf("Aggregate(registry): %v", err)
	}
	assertIDs(t, "aggregate by registry", render(byRegistry), []string{
		"meter-ag-b/content_pull=1",
		"meter-ag-b/entity_resolve=5",
	})

	inPeriod, err := st.Aggregate(ctx, storage.MeteringFilter{
		RegistryName: "meter-ag-b", EventType: storage.MeteringEventEntityResolve, After: meterT0.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Aggregate(period): %v", err)
	}
	assertIDs(t, "aggregate in period", render(inPeriod), []string{"meter-ag-b/entity_resolve=3"})
}

func meterConfCappedUnder(t *testing.T, st storage.MeteringStore) {
	ok, err := st.RecordCapped(context.Background(),
		meterEvent("meter-cu-1", "meter-cu", storage.MeteringEventEntityResolve, "ai", 1, meterT0), time.Time{}, 2)
	if err != nil {
		t.Fatalf("RecordCapped: %v", err)
	}
	if !ok {
		t.Fatal("RecordCapped under the cap = false, want true")
	}
	events, err := st.List(context.Background(), storage.MeteringFilter{RegistryName: "meter-cu"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 || events[0].ID != "meter-cu-1" || events[0].Namespace != "ai" ||
		events[0].Count != 1 || !events[0].OccurredAt.Equal(meterT0) {
		t.Errorf("recorded events = %+v, want meter-cu-1 as given", events)
	}
}

func meterConfCappedAtCap(t *testing.T, st storage.MeteringStore) {
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		ok, err := st.RecordCapped(ctx, meterEvent(fmt.Sprintf("meter-cc-%d", i), "meter-cc",
			storage.MeteringEventContentPull, "", 1, meterT0.Add(time.Duration(i)*time.Second)), time.Time{}, 2)
		if err != nil {
			t.Fatalf("RecordCapped %d: %v", i, err)
		}
		if want := i <= 2; ok != want {
			t.Errorf("RecordCapped %d = %v, want %v", i, ok, want)
		}
	}
	assertIDs(t, "recorded events", meterListIDs(t, st, storage.MeteringFilter{RegistryName: "meter-cc"}),
		[]string{"meter-cc-2", "meter-cc-1"})
}

func meterConfCappedSumsCounts(t *testing.T, st storage.MeteringStore) {
	ctx := context.Background()
	// One event of count 5 against a cap of 5 leaves no room: the cap is
	// on summed counts, not rows.
	meterRecord(t, st, meterEvent("meter-cs-1", "meter-cs", storage.MeteringEventEntityResolve, "", 5, meterT0))
	ok, err := st.RecordCapped(ctx, meterEvent("meter-cs-2", "meter-cs",
		storage.MeteringEventEntityResolve, "", 1, meterT0.Add(time.Second)), time.Time{}, 5)
	if err != nil {
		t.Fatalf("RecordCapped: %v", err)
	}
	if ok {
		t.Error("RecordCapped with summed count at the cap = true, want false")
	}
	ok, err = st.RecordCapped(ctx, meterEvent("meter-cs-3", "meter-cs",
		storage.MeteringEventEntityResolve, "", 1, meterT0.Add(time.Second)), time.Time{}, 6)
	if err != nil {
		t.Fatalf("RecordCapped: %v", err)
	}
	if !ok {
		t.Error("RecordCapped with summed count below the cap = false, want true")
	}
	if got := meterTotal(t, st, "meter-cs", storage.MeteringEventEntityResolve); got != 6 {
		t.Errorf("total = %d, want 6", got)
	}
}

func meterConfCappedPeriod(t *testing.T, st storage.MeteringStore) {
	ctx := context.Background()
	period := meterT0.Add(24 * time.Hour)
	// Two events before the period and one on its first second.
	meterRecord(t, st,
		meterEvent("meter-cp-1", "meter-cp", storage.MeteringEventEntityResolve, "", 1, meterT0),
		meterEvent("meter-cp-2", "meter-cp", storage.MeteringEventEntityResolve, "", 1, meterT0.Add(time.Hour)),
		meterEvent("meter-cp-3", "meter-cp", storage.MeteringEventEntityResolve, "", 1, period),
	)
	// The period start is inclusive: one event counts, so a cap of 2
	// admits one more and a cap of 1 none.
	ok, err := st.RecordCapped(ctx, meterEvent("meter-cp-4", "meter-cp",
		storage.MeteringEventEntityResolve, "", 1, period.Add(time.Minute)), period, 1)
	if err != nil {
		t.Fatalf("RecordCapped: %v", err)
	}
	if ok {
		t.Error("RecordCapped with the period's event at the cap = true, want false")
	}
	ok, err = st.RecordCapped(ctx, meterEvent("meter-cp-5", "meter-cp",
		storage.MeteringEventEntityResolve, "", 1, period.Add(time.Minute)), period, 2)
	if err != nil {
		t.Fatalf("RecordCapped: %v", err)
	}
	if !ok {
		t.Error("RecordCapped with only the period's events counted = false, want true")
	}
	// A zero period start is all time: four events recorded, cap 4 full.
	ok, err = st.RecordCapped(ctx, meterEvent("meter-cp-6", "meter-cp",
		storage.MeteringEventEntityResolve, "", 1, period.Add(2*time.Minute)), time.Time{}, 4)
	if err != nil {
		t.Fatalf("RecordCapped: %v", err)
	}
	if ok {
		t.Error("RecordCapped all-time at the cap = true, want false")
	}
	if got := meterTotal(t, st, "meter-cp", storage.MeteringEventEntityResolve); got != 4 {
		t.Errorf("total = %d, want 4", got)
	}
}

func meterConfCappedScope(t *testing.T, st storage.MeteringStore) {
	ctx := context.Background()
	// Usage on another registry and on another event type of the same
	// registry does not count toward the cap.
	meterRecord(t, st,
		meterEvent("meter-sc-1", "meter-sc-other", storage.MeteringEventEntityResolve, "", 5, meterT0),
		meterEvent("meter-sc-2", "meter-sc", storage.MeteringEventContentPull, "", 5, meterT0),
	)
	ok, err := st.RecordCapped(ctx, meterEvent("meter-sc-3", "meter-sc",
		storage.MeteringEventEntityResolve, "", 1, meterT0.Add(time.Second)), time.Time{}, 1)
	if err != nil {
		t.Fatalf("RecordCapped: %v", err)
	}
	if !ok {
		t.Error("RecordCapped counted another registry or event type = false, want true")
	}
}

func meterConfCappedConcurrent(t *testing.T, st storage.MeteringStore) {
	const (
		workers = 16
		limit   = 5
	)
	ctx := context.Background()
	var (
		wg       sync.WaitGroup
		recorded atomic.Int64
		errs     = make(chan error, workers)
		start    = make(chan struct{})
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ok, err := st.RecordCapped(ctx, meterEvent(fmt.Sprintf("meter-cn-%02d", i), "meter-cn",
				storage.MeteringEventContentPull, "", 1, meterT0.Add(time.Duration(i)*time.Second)), time.Time{}, limit)
			if err != nil {
				errs <- err
				return
			}
			if ok {
				recorded.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("RecordCapped: %v", err)
	}
	if got := recorded.Load(); got != limit {
		t.Errorf("concurrent RecordCapped admitted %d events, want exactly %d", got, limit)
	}
	if got := meterTotal(t, st, "meter-cn", storage.MeteringEventContentPull); got != limit {
		t.Errorf("stored total = %d, want %d", got, limit)
	}
}

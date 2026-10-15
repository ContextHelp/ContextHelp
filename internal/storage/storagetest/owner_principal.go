package storagetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// guestPrincipal is a non-owner principal id, in the ADR-079 shape.
const guestPrincipal = "prn_guest"

// OwnerPrincipalConformance pins the per-principal contract of the search
// history, saved search and resurfacing queue stores: a row written
// without a principal belongs to the instance owner, a principal given on
// write round-trips, a filter with OwnerPrincipal set keeps that
// principal's rows only, and a filter without it still lists every row.
// A driver whose store is a placeholder (ErrNotImplemented) skips.
func OwnerPrincipalConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	t.Run("search history", func(t *testing.T) { ownerConfSearchHistory(t, drv.SearchHistory()) })
	t.Run("saved searches", func(t *testing.T) { ownerConfSavedSearches(t, drv.SavedSearches()) })
	t.Run("resurfacing queue", func(t *testing.T) { ownerConfResurfacing(t, drv) })
}

// skipUnimplemented skips the test when err marks a driver's placeholder
// store, and fails it on any other error.
func skipUnimplemented(t *testing.T, op string, err error) {
	t.Helper()
	if errors.Is(err, storage.ErrNotImplemented) {
		t.Skipf("PARITY GAP: %v", err)
	}
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
}

// assertIDs checks got against want, in order.
func assertIDs(t *testing.T, label string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func ownerConfSearchHistory(t *testing.T, st storage.SearchHistoryStore) {
	ctx := context.Background()
	t0 := fixtureTime()
	entries := []*storage.SearchHistoryEntry{
		{ID: "own-sh-owner", Query: "q1", ProfileID: "work", SearchedAt: t0},
		{ID: "own-sh-guest", Query: "q2", ProfileID: "work", OwnerPrincipal: guestPrincipal, SearchedAt: t0.Add(time.Minute)},
		{ID: "own-sh-guest-home", Query: "q3", ProfileID: "home", OwnerPrincipal: guestPrincipal, SearchedAt: t0.Add(2 * time.Minute)},
	}
	for i, e := range entries {
		err := st.Append(ctx, e)
		if i == 0 {
			skipUnimplemented(t, "Append", err)
		}
		if err != nil {
			t.Fatalf("Append %s: %v", e.ID, err)
		}
	}
	list := func(f storage.SearchHistoryFilter) ([]string, []string) {
		t.Helper()
		got, err := st.List(ctx, f)
		if err != nil {
			t.Fatalf("List %+v: %v", f, err)
		}
		ids := make([]string, len(got))
		owners := make([]string, len(got))
		for i, e := range got {
			ids[i], owners[i] = e.ID, e.OwnerPrincipal
		}
		return ids, owners
	}

	ids, owners := list(storage.SearchHistoryFilter{})
	assertIDs(t, "List unscoped", ids, []string{"own-sh-guest-home", "own-sh-guest", "own-sh-owner"})
	assertIDs(t, "owners round-trip", owners, []string{guestPrincipal, guestPrincipal, storage.OwnerPrincipal})

	ids, _ = list(storage.SearchHistoryFilter{OwnerPrincipal: storage.OwnerPrincipal})
	assertIDs(t, "List owner", ids, []string{"own-sh-owner"})
	ids, _ = list(storage.SearchHistoryFilter{OwnerPrincipal: guestPrincipal})
	assertIDs(t, "List guest", ids, []string{"own-sh-guest-home", "own-sh-guest"})
	ids, _ = list(storage.SearchHistoryFilter{OwnerPrincipal: guestPrincipal, ProfileID: "work"})
	assertIDs(t, "List guest+work", ids, []string{"own-sh-guest"})
	ids, _ = list(storage.SearchHistoryFilter{OwnerPrincipal: "prn_nobody"})
	assertIDs(t, "List unknown principal", ids, []string{})
}

func ownerConfSavedSearches(t *testing.T, st storage.SavedSearchStore) {
	ctx := context.Background()
	t0 := fixtureTime()
	searches := []*storage.SavedSearch{
		{ID: "own-ss-owner", Name: "own-owner", Query: "q1", ProfileID: "work", CreatedAt: t0, UpdatedAt: t0},
		{ID: "own-ss-guest", Name: "own-guest", Query: "q2", ProfileID: "work", OwnerPrincipal: guestPrincipal,
			CreatedAt: t0.Add(time.Minute), UpdatedAt: t0.Add(time.Minute)},
		{ID: "own-ss-guest-home", Name: "own-guest-home", Query: "q3", ProfileID: "home", OwnerPrincipal: guestPrincipal,
			CreatedAt: t0.Add(2 * time.Minute), UpdatedAt: t0.Add(2 * time.Minute)},
	}
	for i, ss := range searches {
		err := st.Create(ctx, ss)
		if i == 0 {
			skipUnimplemented(t, "Create", err)
		}
		if err != nil {
			t.Fatalf("Create %s: %v", ss.Name, err)
		}
	}
	for name, want := range map[string]string{"own-owner": storage.OwnerPrincipal, "own-guest": guestPrincipal} {
		got, err := st.GetByName(ctx, name)
		if err != nil || got == nil {
			t.Fatalf("GetByName %s = %v, %v", name, got, err)
		}
		if got.OwnerPrincipal != want {
			t.Errorf("GetByName %s owner = %q, want %q", name, got.OwnerPrincipal, want)
		}
	}
	list := func(f storage.SavedSearchFilter) []string {
		t.Helper()
		got, err := st.List(ctx, f)
		if err != nil {
			t.Fatalf("List %+v: %v", f, err)
		}
		ids := make([]string, len(got))
		for i, ss := range got {
			ids[i] = ss.ID
		}
		return ids
	}
	assertIDs(t, "List unscoped", list(storage.SavedSearchFilter{}),
		[]string{"own-ss-guest-home", "own-ss-guest", "own-ss-owner"})
	assertIDs(t, "List owner", list(storage.SavedSearchFilter{OwnerPrincipal: storage.OwnerPrincipal}),
		[]string{"own-ss-owner"})
	assertIDs(t, "List guest", list(storage.SavedSearchFilter{OwnerPrincipal: guestPrincipal}),
		[]string{"own-ss-guest-home", "own-ss-guest"})
	assertIDs(t, "List guest+work", list(storage.SavedSearchFilter{OwnerPrincipal: guestPrincipal, ProfileID: "work"}),
		[]string{"own-ss-guest"})
	assertIDs(t, "List unknown principal", list(storage.SavedSearchFilter{OwnerPrincipal: "prn_nobody"}), []string{})

	// Ownership is fixed at creation: an update carrying another principal
	// changes the query, not the owner.
	upd := *searches[1]
	upd.Query, upd.OwnerPrincipal = "q2 edited", "prn_other"
	if err := st.Update(ctx, &upd); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := st.GetByName(ctx, "own-guest")
	if err != nil || got == nil {
		t.Fatalf("GetByName after Update = %v, %v", got, err)
	}
	if got.Query != "q2 edited" || got.OwnerPrincipal != guestPrincipal {
		t.Errorf("after Update: query %q owner %q, want %q owned by %q", got.Query, got.OwnerPrincipal, "q2 edited", guestPrincipal)
	}
}

func ownerConfResurfacing(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Resurfacing()
	for _, id := range []string{"own-obj-1", "own-obj-2", "own-obj-3"} {
		if err := drv.Objects().Create(ctx, searchFixtureObject(id, "note", "resurfacing fixture "+id)); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	entries := []*storage.ResurfacingEntry{
		{ID: "own-rq-owner", ObjectID: "own-obj-1", ProfileID: "work", Score: 0.9, Reason: "tag_overlap", CreatedAt: fixtureTime()},
		{ID: "own-rq-guest", ObjectID: "own-obj-2", ProfileID: "work", Score: 0.8, Reason: "tag_overlap",
			OwnerPrincipal: guestPrincipal, CreatedAt: fixtureTime()},
		{ID: "own-rq-guest-home", ObjectID: "own-obj-3", ProfileID: "home", Score: 0.7, Reason: "tag_overlap",
			OwnerPrincipal: guestPrincipal, CreatedAt: fixtureTime()},
	}
	for i, e := range entries {
		err := st.Upsert(ctx, e)
		if i == 0 {
			skipUnimplemented(t, "Upsert", err)
		}
		if err != nil {
			t.Fatalf("Upsert %s: %v", e.ID, err)
		}
	}
	list := func(f storage.ResurfacingFilter) ([]string, []string) {
		t.Helper()
		got, err := st.List(ctx, f)
		if err != nil {
			t.Fatalf("List %+v: %v", f, err)
		}
		ids := make([]string, len(got))
		owners := make([]string, len(got))
		for i, e := range got {
			ids[i], owners[i] = e.ID, e.OwnerPrincipal
		}
		return ids, owners
	}

	ids, owners := list(storage.ResurfacingFilter{})
	assertIDs(t, "List unscoped", ids, []string{"own-rq-owner", "own-rq-guest", "own-rq-guest-home"})
	assertIDs(t, "owners round-trip", owners, []string{storage.OwnerPrincipal, guestPrincipal, guestPrincipal})

	ids, _ = list(storage.ResurfacingFilter{OwnerPrincipal: storage.OwnerPrincipal})
	assertIDs(t, "List owner", ids, []string{"own-rq-owner"})
	ids, _ = list(storage.ResurfacingFilter{OwnerPrincipal: guestPrincipal})
	assertIDs(t, "List guest", ids, []string{"own-rq-guest", "own-rq-guest-home"})
	ids, _ = list(storage.ResurfacingFilter{OwnerPrincipal: guestPrincipal, ProfileID: "work", UnseenOnly: true})
	assertIDs(t, "List guest+work+unseen", ids, []string{"own-rq-guest"})
	ids, _ = list(storage.ResurfacingFilter{OwnerPrincipal: "prn_nobody"})
	assertIDs(t, "List unknown principal", ids, []string{})

	// Ownership is fixed at creation: re-queuing the owner's candidate
	// under another principal refreshes the score and keeps the owner.
	again := *entries[0]
	again.Score, again.OwnerPrincipal = 0.95, "prn_other"
	if err := st.Upsert(ctx, &again); err != nil {
		t.Fatalf("Upsert again: %v", err)
	}
	got, err := st.List(ctx, storage.ResurfacingFilter{OwnerPrincipal: storage.OwnerPrincipal})
	if err != nil || len(got) != 1 || got[0].ID != "own-rq-owner" || got[0].Score != 0.95 {
		t.Errorf("owner's queue after re-queue = %+v, %v; want own-rq-owner at 0.95", got, err)
	}
}

package storagetest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// listTotalRow is one seeded object of ListTotalConformance and the
// Go-side model its expectations are computed from.
type listTotalRow struct {
	id, status, typ, profile, tag string
	created, updated              time.Time
}

// ListTotalConformance pins ObjectStore.List's total and page: total is
// the exact number of objects the filter selects whatever the page size
// and offset, under every filter List supports here (status scope, type,
// profile, tag, created_at bounds), and the page is a slice of that same
// set in list order (created_at or updated_at, newest first unless asked
// ascending). Expectations come from the seeded rows, not from the
// store, so a count and a page built from different predicates, or a
// page plan that drops or reorders rows, fail here.
func ListTotalConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	const (
		active    = storage.ObjectStatusActive
		inbox     = "inbox"
		discarded = "discarded"
		note      = "note"
		contact   = "contact"
		p1        = "p1"
		p2        = "p2"
		work      = "work"
		home      = "home"
	)
	base := fixtureTime()
	// Twelve objects created one hour apart; updated_at runs the other
	// way so the two orders differ. Statuses, types, profiles and tags
	// cycle so every filter selects a strict subset.
	seed := []listTotalRow{
		{id: "lt-00", status: active, typ: note, tag: work},
		{id: "lt-01", status: active, typ: contact, profile: p1},
		{id: "lt-02", status: inbox, typ: note, tag: work},
		{id: "lt-03", status: active, typ: note, profile: p2, tag: home},
		{id: "lt-04", status: discarded, typ: note, profile: p1, tag: work},
		{id: "lt-05", status: active, typ: contact, tag: work},
		{id: "lt-06", status: active, typ: note, profile: p1},
		{id: "lt-07", status: inbox, typ: contact, profile: p1, tag: home},
		{id: "lt-08", status: active, typ: note, tag: work},
		{id: "lt-09", status: active, typ: note, profile: p2, tag: work},
		{id: "lt-10", status: discarded, typ: contact},
		{id: "lt-11", status: active, typ: note, profile: p1, tag: home},
	}
	for i := range seed {
		r := &seed[i]
		r.created = base.Add(time.Duration(i) * time.Hour)
		r.updated = base.Add(time.Duration(24-i) * time.Hour)
		obj := &storage.KnowledgeObject{
			ID: r.id, Type: r.typ, RawContent: r.id, Status: r.status, ProfileID: r.profile,
			CreatedAt: r.created, UpdatedAt: r.updated,
		}
		if r.tag != "" {
			obj.Tags = []storage.Tag{{Label: r.tag}}
		}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", r.id, err)
		}
	}
	isActive := func(r listTotalRow) bool { return r.status == active }
	at := func(ts time.Time) *time.Time { return &ts }
	cases := []struct {
		name   string
		filter storage.ObjectFilter
		keep   func(listTotalRow) bool
	}{
		{"default scope", storage.ObjectFilter{}, isActive},
		{"every status", storage.ObjectFilter{Status: storage.ObjectStatusAll}, func(listTotalRow) bool { return true }},
		{"inbox", storage.ObjectFilter{Status: inbox}, func(r listTotalRow) bool { return r.status == inbox }},
		{"discarded", storage.ObjectFilter{Status: discarded}, func(r listTotalRow) bool { return r.status == discarded }},
		{"type", storage.ObjectFilter{Type: note}, func(r listTotalRow) bool { return isActive(r) && r.typ == note }},
		{"profile", storage.ObjectFilter{ProfileID: p1}, func(r listTotalRow) bool { return isActive(r) && r.profile == p1 }},
		{"tag", storage.ObjectFilter{Tag: work}, func(r listTotalRow) bool { return isActive(r) && r.tag == work }},
		{"after", storage.ObjectFilter{After: at(base.Add(5 * time.Hour))}, func(r listTotalRow) bool {
			return isActive(r) && !r.created.Before(base.Add(5*time.Hour))
		}},
		{"before", storage.ObjectFilter{Before: at(base.Add(6 * time.Hour))}, func(r listTotalRow) bool {
			return isActive(r) && !r.created.After(base.Add(6*time.Hour))
		}},
		{"between", storage.ObjectFilter{After: at(base.Add(3 * time.Hour)), Before: at(base.Add(9 * time.Hour))}, func(r listTotalRow) bool {
			return isActive(r) && !r.created.Before(base.Add(3*time.Hour)) && !r.created.After(base.Add(9*time.Hour))
		}},
		{"type and profile, every status", storage.ObjectFilter{Status: storage.ObjectStatusAll, Type: note, ProfileID: p1}, func(r listTotalRow) bool {
			return r.typ == note && r.profile == p1
		}},
		{"by updated_at", storage.ObjectFilter{Sort: "updated_at"}, isActive},
		{"ascending", storage.ObjectFilter{Dir: "asc"}, isActive},
		{"by updated_at ascending", storage.ObjectFilter{Sort: "updated_at", Dir: "asc"}, isActive},
		{"tag by updated_at", storage.ObjectFilter{Tag: work, Sort: "updated_at"}, func(r listTotalRow) bool {
			return isActive(r) && r.tag == work
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := listTotalExpect(seed, tc.filter, tc.keep)
			if len(want) == 0 {
				t.Fatal("the seed selects no row: the case pins nothing")
			}
			assertListPages(t, drv.Objects(), tc.filter, want)
		})
	}
}

// assertListPages lists filter as the whole set, as pages of two from the
// first and from the third row, and as everything but the first row, and
// checks each page against want (the ids in list order) and each total
// against len(want): total never follows the page.
func assertListPages(t *testing.T, objs storage.ObjectStore, filter storage.ObjectFilter, want []string) {
	t.Helper()
	ctx := context.Background()
	for _, page := range []struct{ limit, offset int }{{0, 0}, {2, 0}, {2, 2}, {len(want), 1}} {
		f := filter
		f.Limit, f.Offset = page.limit, page.offset
		listed, total, err := objs.List(ctx, f)
		if err != nil {
			t.Fatalf("List(limit %d, offset %d): %v", page.limit, page.offset, err)
		}
		got := make([]string, 0, len(listed))
		for _, o := range listed {
			got = append(got, o.ID)
		}
		wantPage := want[min(page.offset, len(want)):]
		if page.limit > 0 && len(wantPage) > page.limit {
			wantPage = wantPage[:page.limit]
		}
		if total != len(want) || !slices.Equal(got, wantPage) {
			t.Errorf("List(limit %d, offset %d) = %v (total %d), want %v (total %d)",
				page.limit, page.offset, got, total, wantPage, len(want))
		}
	}
}

// listTotalExpect is the ids of the seeded rows keep selects, in the
// order filter lists them.
func listTotalExpect(seed []listTotalRow, filter storage.ObjectFilter, keep func(listTotalRow) bool) []string {
	sel := make([]listTotalRow, 0, len(seed))
	for _, r := range seed {
		if keep(r) {
			sel = append(sel, r)
		}
	}
	slices.SortFunc(sel, func(a, b listTotalRow) int {
		ta, tb := a.created, b.created
		if filter.Sort == "updated_at" {
			ta, tb = a.updated, b.updated
		}
		if filter.Dir == "asc" {
			return ta.Compare(tb)
		}
		return tb.Compare(ta)
	})
	ids := make([]string, len(sel))
	for i, r := range sel {
		ids[i] = r.id
	}
	return ids
}

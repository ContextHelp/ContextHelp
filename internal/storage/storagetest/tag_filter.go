package storagetest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
)

// tagSeedRow is one object of TagFilterConformance.
type tagSeedRow struct {
	id   string
	tags []string
}

// TagFilterConformance pins the multi-tag selection of ObjectStore.List
// and FTSSearch: any (one of), all (every required tag), results matching
// more of the listed tags first in List, unknown tags selecting nothing, created-date
// bounds combined with tags, and exact totals under pagination. The
// expectations are computed from the seed in Go (tagfilter.Match and
// Matched), never from the store.
func TagFilterConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	base := fixtureTime()
	// Created one hour apart, oldest first, so list order is the reverse
	// of the seed order and a count tier shows as a departure from it.
	seed := []tagSeedRow{
		{"tf-0", []string{"a"}},
		{"tf-1", []string{"a", "b"}},
		{"tf-2", []string{"b"}},
		{"tf-3", []string{"a", "b", "c"}},
		{"tf-4", []string{"c"}},
		{"tf-5", []string{"a", "c"}},
		{"tf-6", nil},
		{"tf-7", []string{"b", "c"}},
	}
	created := map[string]time.Time{}
	for i, r := range seed {
		obj := searchFixtureObject(r.id, "note", "tagword")
		obj.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		obj.UpdatedAt = obj.CreatedAt
		for _, l := range r.tags {
			obj.Tags = append(obj.Tags, storage.Tag{Label: l})
		}
		created[r.id] = obj.CreatedAt
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", r.id, err)
		}
	}
	at := func(h int) *time.Time { ts := base.Add(time.Duration(h) * time.Hour); return &ts }
	tagChangeOrder(t, drv, seed)

	cases := []struct {
		name   string
		tags   string
		after  *time.Time
		before *time.Time
	}{
		{name: "one tag", tags: "a"},
		{name: "any of two", tags: "a,b"},
		{name: "any via repeat", tags: "a,b,c"},
		{name: "unknown tag", tags: "nope"},
		{name: "any with unknown", tags: "nope,c"},
		{name: "all of two", tags: "+a,+b"},
		{name: "all of three", tags: "+a,+b,+c"},
		{name: "all with unknown", tags: "+a,+nope"},
		{name: "all with optional", tags: "+a,b,+c"},
		{name: "all with optional unknown", tags: "+a,nope"},
		{name: "any, count tiers", tags: "a,b"},
		{name: "any all optional", tags: "a,b"},
		{name: "any plus created window", tags: "a,b", after: at(2), before: at(5)},
		{name: "all plus created after", tags: "+a,+c", after: at(4)},
		{name: "count tiers plus created before", tags: "c,b", before: at(5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := tagfilter.Parse([]string{tc.tags})
			if err != nil {
				t.Fatal(err)
			}
			filter := storage.ObjectFilter{Tags: f, After: tc.after, Before: tc.before}
			want := tagExpect(seed, f, created, tc.after, tc.before)

			// List: exact order, exact total under every page.
			if len(want) == 0 {
				listed, total, err := drv.Objects().List(ctx, filter)
				if err != nil || total != 0 || len(listed) != 0 {
					t.Fatalf("List = %d objects, total %d, err %v; want none", len(listed), total, err)
				}
			} else {
				assertListPages(t, drv.Objects(), filter, want)
			}

			// FTS: same membership; the pool is relevance-ordered, never by tags.
			fts := filter
			fts.Limit = 50
			hits, err := drv.Objects().FTSSearch(ctx, "tagword", fts)
			if err != nil {
				t.Fatalf("FTSSearch: %v", err)
			}
			got := make([]string, len(hits))
			for i, h := range hits {
				got[i] = h.ID
			}
			if !sameSet(got, want) {
				t.Fatalf("FTSSearch = %v, want the members of %v", got, want)
			}
		})
	}
}

// tagChangeOrder: a ChangedAfter page composes with the tag selection and
// stays in change order, which here is seed order: the tags-matched count
// never reorders a change-ordered page, so paging by the last
// ChangeSeq cannot skip or repeat an object.
func tagChangeOrder(t *testing.T, drv storage.StorageDriver, seed []tagSeedRow) {
	ctx := context.Background()
	first, err := drv.Objects().Get(ctx, "tf-1")
	if err != nil {
		t.Fatalf("Get tf-1: %v", err)
	}
	for _, spec := range []string{"a,c", "+a,+c", "+a,b,+c", "c,b"} {
		t.Run("changed after with "+spec, func(t *testing.T) {
			f, err := tagfilter.Parse([]string{spec})
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, r := range seed[2:] {
				if f.Match(r.tags) {
					want = append(want, r.id)
				}
			}
			after := first.ChangeSeq
			filter := storage.ObjectFilter{Tags: f, ChangedAfter: &after}
			got, total, err := drv.Objects().List(ctx, filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			ids := make([]string, len(got))
			for i, o := range got {
				ids[i] = o.ID
			}
			if !slices.Equal(ids, want) || total != len(want) {
				t.Errorf("List = %v (total %d), want %v in change order", ids, total, want)
			}
		})
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// tagExpect is the ids of the seed rows f and the created bounds select,
// in List order: tags matched descending, then newest first.
func tagExpect(seed []tagSeedRow, f tagfilter.Filter, created map[string]time.Time, after, before *time.Time) []string {
	sel := make([]tagSeedRow, 0, len(seed))
	for _, r := range seed {
		c := created[r.id]
		if !f.Match(r.tags) || (after != nil && c.Before(*after)) || (before != nil && !c.Before(*before)) {
			continue
		}
		sel = append(sel, r)
	}
	slices.SortStableFunc(sel, func(a, b tagSeedRow) int {
		if ba, bb := f.Matched(a.tags), f.Matched(b.tags); ba != bb {
			return bb - ba
		}
		return created[b.id].Compare(created[a.id])
	})
	ids := make([]string, len(sel))
	for i, r := range sel {
		ids[i] = r.id
	}
	return ids
}

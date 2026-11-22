package storagetest

import (
	"context"
	"reflect"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TagCountsConformance pins ObjectStore.TagCounts: labels counted once
// per active object (inbox and discarded objects and repeated labels do
// not count), ordered by count descending then label (byte order), a case-insensitive
// prefix match with LIKE wildcards taken literally, and the limit.
func TagCountsConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	seed := []struct {
		id, status string
		tags       []string
	}{
		{"tc-0", "active", []string{"go", "golang", "Gopher"}},
		{"tc-1", "active", []string{"go", "rust", "go"}},
		{"tc-2", "active", []string{"go", "100%_sure", "a_b"}},
		{"tc-3", "active", []string{"axb", "rust"}},
		{"tc-4", "inbox", []string{"go", "inbox-only"}},
		{"tc-5", "discarded", []string{"discarded-only"}},
		{"tc-6", "active", nil},
	}
	for _, r := range seed {
		obj := searchFixtureObject(r.id, "note", "tagcounts")
		obj.Status = r.status
		for _, l := range r.tags {
			obj.Tags = append(obj.Tags, storage.Tag{Label: l})
		}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("Create %s: %v", r.id, err)
		}
	}
	all := []storage.TagCount{
		{Label: "go", Count: 3}, {Label: "rust", Count: 2},
		{Label: "100%_sure", Count: 1}, {Label: "Gopher", Count: 1}, {Label: "a_b", Count: 1},
		{Label: "axb", Count: 1}, {Label: "golang", Count: 1},
	}
	cases := []struct {
		name string
		q    storage.TagQuery
		want []storage.TagCount
	}{
		{"all, count then label", storage.TagQuery{}, all},
		{"limit", storage.TagQuery{Limit: 3}, all[:3]},
		{"prefix ignores case", storage.TagQuery{Prefix: "GO"}, []storage.TagCount{{Label: "go", Count: 3}, {Label: "Gopher", Count: 1}, {Label: "golang", Count: 1}}},
		{"prefix and limit", storage.TagQuery{Prefix: "go", Limit: 2}, []storage.TagCount{{Label: "go", Count: 3}, {Label: "Gopher", Count: 1}}},
		{"prefix is anchored", storage.TagQuery{Prefix: "ust"}, []storage.TagCount{}},
		{"underscore is literal", storage.TagQuery{Prefix: "a_"}, []storage.TagCount{{Label: "a_b", Count: 1}}},
		{"percent is literal", storage.TagQuery{Prefix: "100%"}, []storage.TagCount{{Label: "100%_sure", Count: 1}}},
		{"no match", storage.TagQuery{Prefix: "zzz"}, []storage.TagCount{}},
		{"inbox label hidden", storage.TagQuery{Prefix: "inbox"}, []storage.TagCount{}},
		{"discarded label hidden", storage.TagQuery{Prefix: "discarded"}, []storage.TagCount{}},
	}
	for _, c := range cases {
		got, err := drv.Objects().TagCounts(ctx, c.q)
		if err != nil {
			t.Fatalf("%s: TagCounts(%+v): %v", c.name, c.q, err)
		}
		if len(got) == 0 {
			got = []storage.TagCount{}
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: TagCounts(%+v) = %v, want %v", c.name, c.q, got, c.want)
		}
	}
}

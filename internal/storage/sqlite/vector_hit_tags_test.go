package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
)

// TestVectorHitMatchesTagsAndCreated pins the filters VectorSearch applies
// in Go to a KNN hit: tag selection and the created bounds.
func TestVectorHitMatchesTagsAndCreated(t *testing.T) {
	base := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	obj := &storage.KnowledgeObject{
		Status: storage.ObjectStatusActive, CreatedAt: base,
		Tags: []storage.Tag{{Label: "a"}, {Label: "b"}},
	}
	parse := func(s string) tagfilter.Filter {
		f, err := tagfilter.Parse([]string{s})
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	hour := func(h int) *time.Time { ts := base.Add(time.Duration(h) * time.Hour); return &ts }
	cases := []struct {
		name   string
		filter storage.ObjectFilter
		want   bool
	}{
		{"no tag filter", storage.ObjectFilter{}, true},
		{"any hit", storage.ObjectFilter{Tags: parse("a,z")}, true},
		{"any miss", storage.ObjectFilter{Tags: parse("y,z")}, false},
		{"all hit", storage.ObjectFilter{Tags: parse("+a,+b")}, true},
		{"all miss", storage.ObjectFilter{Tags: parse("+a,+z")}, false},
		{"all optional not required", storage.ObjectFilter{Tags: parse("+a,z")}, true},
		{"after inclusive", storage.ObjectFilter{After: &base}, true},
		{"after later", storage.ObjectFilter{After: hour(1)}, false},
		{"before exclusive", storage.ObjectFilter{Before: &base}, false},
		{"before just past", storage.ObjectFilter{Before: hour(1)}, true},
		{"before earlier", storage.ObjectFilter{Before: hour(-1)}, false},
	}
	for _, tc := range cases {
		if got := vectorHitMatches(obj, tc.filter); got != tc.want {
			t.Errorf("%s: vectorHitMatches = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestVectorSearch_RareTagBeyondOverfetch: a tag or date filter is applied
// to hydrated KNN hits, so a rare match ranked below the first
// limit*vectorOverfetch neighbours must still be found: the search widens
// until the page is full or the index is exhausted.
func TestVectorSearch_RareTagBeyondOverfetch(t *testing.T) {
	d := newTestDriver(t)
	s, emb := vectorTestStore(t, d)
	ctx := context.Background()
	for i := range 3 * vectorOverfetch {
		obj := makeObject(fmt.Sprintf("common-%02d", i), "item")
		obj.Tags = []storage.Tag{{Label: "common"}}
		putVector(t, s, emb, obj, []float32{1, float32(i) / 100, 0})
	}
	rare := makeObject("rare", "item")
	rare.Tags = []storage.Tag{{Label: "rare"}, {Label: "machine learning"}}
	putVector(t, s, emb, rare, []float32{0, 1, 0}) // farthest from the query

	for _, spec := range []string{"rare", "+rare,+machine learning", "nope,machine learning"} {
		f, err := tagfilter.Parse([]string{spec})
		require.NoError(t, err)
		got, err := s.VectorSearch(ctx, m3(1, 0, 0), storage.ObjectFilter{Tags: f, Limit: 1})
		require.NoError(t, err, spec)
		require.Len(t, got, 1, "tagged %q: the rare hit lies past the first over-fetch", spec)
		assert.Equal(t, "rare", got[0].ID)
	}

	// A tag nothing carries exhausts the index and returns nothing.
	f, err := tagfilter.Parse([]string{"nobody"})
	require.NoError(t, err)
	got, err := s.VectorSearch(ctx, m3(1, 0, 0), storage.ObjectFilter{Tags: f, Limit: 1})
	require.NoError(t, err)
	assert.Empty(t, got)

	// A date bound widens the same way.
	late := rare.CreatedAt.Add(time.Hour)
	got, err = s.VectorSearch(ctx, m3(1, 0, 0), storage.ObjectFilter{After: &late, Limit: 1})
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestCreatedBoundsSecondPrecision: created_at is stored at second
// precision, so List and the vector leg's hit filter read a sub-second
// bound alike.
func TestCreatedBoundsSecondPrecision(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	base := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	obj := makeObject("sub-1", "item")
	obj.CreatedAt, obj.UpdatedAt = base, base
	require.NoError(t, d.Objects().Create(ctx, obj))
	got, err := d.Objects().Get(ctx, "sub-1")
	require.NoError(t, err)

	half := 500 * time.Millisecond
	for name, f := range map[string]storage.ObjectFilter{
		"after +.5s":   {After: ptrTime(base.Add(half))},
		"after -.5s":   {After: ptrTime(base.Add(-half))},
		"before +.5s":  {Before: ptrTime(base.Add(half))},
		"before -.5s":  {Before: ptrTime(base.Add(-half))},
		"before +1.5s": {Before: ptrTime(base.Add(time.Second + half))},
	} {
		listed, _, err := d.Objects().List(ctx, f)
		require.NoError(t, err, name)
		inList := false
		for _, o := range listed {
			inList = inList || o.ID == "sub-1"
		}
		assert.Equal(t, inList, vectorHitMatches(got, f), "%s: List says %v", name, inList)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

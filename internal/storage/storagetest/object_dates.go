package storagetest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ObjectDateFilterConformance pins ObjectFilter.After and Before as
// instant comparisons: an object created in one UTC offset is selected
// by a bound given in another exactly when its instant is on the right
// side of the bound. The daemon stamps objects in its local zone, while
// the API hands the driver bounds normalized to UTC.
func ObjectDateFilterConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	edt := time.FixedZone("EDT", -4*3600)
	// 10:00:00-04:00 is 14:00:00Z; 10:00:05-04:00 is 14:00:05Z.
	early := time.Date(2026, 9, 27, 10, 0, 0, 0, edt)
	late := early.Add(5 * time.Second)
	for id, at := range map[string]time.Time{"od-early": early, "od-late": late} {
		if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "note", RawContent: id, Status: "active",
			CreatedAt: at, UpdatedAt: at,
		}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	at := func(ts time.Time) *time.Time { return &ts }
	cases := []struct {
		name string
		f    storage.ObjectFilter
		want []string
	}{
		{"after in UTC between the two", storage.ObjectFilter{After: at(early.Add(time.Second).UTC())}, []string{"od-late"}},
		{"after in UTC at the first", storage.ObjectFilter{After: at(early.UTC())}, []string{"od-early", "od-late"}},
		{"after in the stored zone", storage.ObjectFilter{After: at(early.Add(time.Second))}, []string{"od-late"}},
		{"after in a zone ahead of UTC", storage.ObjectFilter{After: at(early.Add(time.Second).In(time.FixedZone("CEST", 2*3600)))}, []string{"od-late"}},
		{"before in UTC between the two", storage.ObjectFilter{Before: at(late.Add(-time.Second).UTC())}, []string{"od-early"}},
		{"before in UTC at the last", storage.ObjectFilter{Before: at(late.UTC())}, []string{"od-early", "od-late"}},
		{"after in UTC past both", storage.ObjectFilter{After: at(late.Add(time.Second).UTC())}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objs, total, err := drv.Objects().List(ctx, tc.f)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			var got []string
			for _, o := range objs {
				got = append(got, o.ID)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) || total != len(tc.want) {
				t.Errorf("List = %v (total %d), want %v", got, total, tc.want)
			}
		})
	}
}

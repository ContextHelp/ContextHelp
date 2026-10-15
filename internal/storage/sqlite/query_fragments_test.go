package sqlite

import (
	"slices"
	"testing"
)

func TestProfilePage(t *testing.T) {
	const base = "SELECT id FROM t WHERE 1=1"
	tests := []struct {
		name          string
		profileID     string
		limit, offset int
		wantQuery     string
		wantArgs      []any
	}{
		{name: "unscoped, unpaged", wantQuery: base + orderNewestFirst},
		{
			name: "profile scope", profileID: "work",
			wantQuery: base + " AND profile_id = ?" + orderNewestFirst,
			wantArgs:  []any{"work"},
		},
		{
			name: "page", limit: 10, offset: 20,
			wantQuery: base + orderNewestFirst + " LIMIT ? OFFSET ?",
			wantArgs:  []any{10, 20},
		},
		{
			name: "offset without limit is ignored", offset: 5,
			wantQuery: base + orderNewestFirst,
		},
		{
			name: "scope and page bind in placeholder order", profileID: "work", limit: 3, offset: 1,
			wantQuery: base + " AND profile_id = ?" + orderNewestFirst + " LIMIT ? OFFSET ?",
			wantArgs:  []any{"work", 3, 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q, args := profilePage(base, orderNewestFirst, tc.profileID, tc.limit, tc.offset)
			if q != tc.wantQuery {
				t.Errorf("query = %q, want %q", q, tc.wantQuery)
			}
			if !slices.Equal(args, tc.wantArgs) {
				t.Errorf("args = %v, want %v", args, tc.wantArgs)
			}
		})
	}
}

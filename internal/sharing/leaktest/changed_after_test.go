package leaktest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

type changePage struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Total int   `json:"total"`
	Next  int64 `json:"next_changed_after"`
}

func changePageOf(t *testing.T, f *Fixture, p Principal, query string) (changePage, Result) {
	t.Helper()
	r := fetch(t, f, p, "/objects?"+query)
	require.Equal(t, http.StatusOK, r.Status, "%s: %s", query, r.Body)
	AssertNoLeak(t, f, p, HTTPKey(http.MethodGet, "/objects"), r)
	var out changePage
	require.NoError(t, json.Unmarshal(r.Body, &out), "%s", r.Body)
	return out, r
}

// A restricted viewer paging by change order sees exactly its visible
// rows, however the hidden rows interleave in the change order, and the
// cursor walks past the hidden rows' sequences without naming them.
func TestChangedAfterListIsViewerScoped(t *testing.T) {
	f := Start(t, storageutil.NewTestDriver(t))
	for _, p := range f.Restricted() {
		if p.Revoked {
			continue
		}
		require.NotEmpty(t, f.Hidden(p), "%s has nothing hidden", p.Name)
		want := f.VisibleObjects(p)

		all, _ := changePageOf(t, f, p, "changed_after=0&status=all&limit=0")
		var got []string
		for _, o := range all.Data {
			got = append(got, o.ID)
		}
		assert.ElementsMatch(t, want, got, "%s: one page", p.Name)
		assert.Equal(t, len(want), all.Total, "%s: total counts the visible rows", p.Name)

		// One row per page: the cursor never lands on, or stops at, a
		// row the viewer does not see.
		var walked []string
		var cursor int64
		for range len(want) + 2 {
			page, _ := changePageOf(t, f, p, fmt.Sprintf("changed_after=%d&status=all&limit=1", cursor))
			if len(page.Data) == 0 {
				assert.Equal(t, cursor, page.Next, "%s: an empty page keeps the cursor", p.Name)
				break
			}
			assert.Greater(t, page.Next, cursor, "%s: the cursor advances", p.Name)
			walked = append(walked, page.Data[0].ID)
			cursor = page.Next
		}
		assert.ElementsMatch(t, want, walked, "%s: paged", p.Name)
	}
}

var nextCursor = regexp.MustCompile(`,"next_changed_after":\d+`)

// Differential: the hidden rows change nothing in a restricted viewer's
// change-order list but the cursor value, which is a position in the
// store's whole change order and so shows how many changes preceded
// the last visible row. That gap is the one thing the list gives away.
func TestChangedAfterListHiddenRowsChangeOnlyTheCursor(t *testing.T) {
	with := Start(t, storageutil.NewTestDriver(t))
	for i, p := range with.Restricted() {
		if p.Revoked {
			continue
		}
		without := Start(t, storageutil.NewTestDriver(t), WithoutHiddenFor(p.Name))
		q := without.Restricted()[i]
		const query = "changed_after=0&status=all&limit=0"
		_, ra := changePageOf(t, with, p, query)
		_, rb := changePageOf(t, without, q, query)
		assert.Equal(t, normalize(nextCursor.ReplaceAll(rb.Body, nil)), normalize(nextCursor.ReplaceAll(ra.Body, nil)), "%s: body apart from the cursor", p.Name)
	}
}

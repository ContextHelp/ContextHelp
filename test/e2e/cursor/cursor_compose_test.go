//go:build cursor_e2e

package cursor

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCursor_ComposesWithMention: cursor + --mention preserves the mention
// flag in the filter and stores it in the query snapshot, while gating by
// time. (Storage-side mention filtering is out of cursor's scope; this test
// asserts the composition contract: cursor passes Mention through unchanged
// AND adds the time gate.)
func TestCursor_ComposesWithMention(t *testing.T) {
	env := newCursorEnv(t)
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	ids := listKOs(t, driver, 3, "ko-mention", "@client.acme")
	require.Len(t, ids, 3)

	// Pull all with ASC sort so objs[0] is earliest.
	objs, _, err := driver.Objects().List(ctx, storage.ObjectFilter{
		Limit: 100, Status: "all", Sort: "created_at", Dir: "asc",
	})
	require.NoError(t, err)
	require.Len(t, objs, 3)

	first := objs[0]
	snap := cursor.QuerySnapshot{Mention: []string{"@client.acme"}}
	_, err = env.mgr.Advance("acme", first.CreatedAt, first.ID, snap)
	require.NoError(t, err)

	// Cursor advances → gate present, Mention passed through.
	c, _, _ := env.mgr.GetOrInit("acme")
	filter := gateByCursor(storage.ObjectFilter{
		Mention: "@client.acme",
		Limit:   100,
		Status:  "all",
	}, c)
	require.NotNil(t, filter.After, "cursor must add time gate")
	assert.Equal(t, "@client.acme", filter.Mention, "Mention must pass through unchanged")

	// Cursor's snapshot persists Mention.
	stored, err := env.mgr.Get("acme")
	require.NoError(t, err)
	assert.Equal(t, []string{"@client.acme"}, stored.Query.Mention)

	// Time gate strictly excludes objs[0]; remaining 2 visible (storage
	// ignores Mention but that's not cursor's contract).
	objs2, _, err := driver.Objects().List(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, objs2, 2, "cursor time gate excludes the advanced-to head")
}

// TestCursor_ComposesWithTag: cursor+--tag.
func TestCursor_ComposesWithTag(t *testing.T) {
	env := newCursorEnv(t)
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i, tag := range []string{"alpha", "beta", "alpha"} {
		ko := storageutil.BuildGraphKO("ko-tag-"+string(rune('a'+i)), "text", "x", tag)
		ko.CreatedAt = base.Add(time.Duration(i) * time.Second)
		ko.UpdatedAt = ko.CreatedAt
		require.NoError(t, driver.Objects().Create(ctx, ko))
	}

	objs, _, err := driver.Objects().List(ctx, storage.ObjectFilter{
		Tag: "alpha", Limit: 100, Status: "all", Sort: "created_at", Dir: "asc",
	})
	require.NoError(t, err)
	require.Len(t, objs, 2)
	first := objs[0]
	_, err = env.mgr.Advance("tagged", first.CreatedAt, first.ID, cursor.QuerySnapshot{Tag: []string{"alpha"}})
	require.NoError(t, err)

	c, _, _ := env.mgr.GetOrInit("tagged")
	filter := gateByCursor(storage.ObjectFilter{Tag: "alpha", Limit: 100, Status: "all"}, c)
	objs2, _, err := driver.Objects().List(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, objs2, 1)
}

// TestCursor_ComposesWithProfile: ProfileID filter compose.
func TestCursor_ComposesWithProfile(t *testing.T) {
	env := newCursorEnv(t)
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i, pid := range []string{"work", "personal", "work"} {
		ko := storageutil.BuildGraphKO("ko-prof-"+string(rune('a'+i)), "text", "x")
		ko.ProfileID = pid
		ko.CreatedAt = base.Add(time.Duration(i) * time.Second)
		ko.UpdatedAt = ko.CreatedAt
		require.NoError(t, driver.Objects().Create(ctx, ko))
	}

	objs, _, err := driver.Objects().List(ctx, storage.ObjectFilter{
		ProfileID: "work", Limit: 100, Status: "all", Sort: "created_at", Dir: "asc",
	})
	require.NoError(t, err)
	require.Len(t, objs, 2)

	first := objs[0]
	_, err = env.mgr.Advance("prof", first.CreatedAt, first.ID, cursor.QuerySnapshot{Profile: "work"})
	require.NoError(t, err)

	c, _, _ := env.mgr.GetOrInit("prof")
	filter := gateByCursor(storage.ObjectFilter{ProfileID: "work", Limit: 100, Status: "all"}, c)
	objs2, _, err := driver.Objects().List(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, objs2, 1)
}

// TestCursor_ComposesWithQAndAfter: cursor + --after compose; cursor's
// last_seen_at takes precedence when stricter than --after.
func TestCursor_ComposesWithQAndAfter(t *testing.T) {
	env := newCursorEnv(t)
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i := 0; i < 5; i++ {
		ko := storageutil.BuildGraphKO("ko-comp-"+string(rune('a'+i)), "text", "x")
		ko.CreatedAt = base.Add(time.Duration(i) * time.Second)
		ko.UpdatedAt = ko.CreatedAt
		require.NoError(t, driver.Objects().Create(ctx, ko))
	}

	// Advance cursor to item 2 (3rd item).
	all, _, err := driver.Objects().List(ctx, storage.ObjectFilter{Limit: 100, Status: "all", Sort: "created_at", Dir: "asc"})
	require.NoError(t, err)
	require.Len(t, all, 5)
	pivot := all[2]
	_, err = env.mgr.Advance("comp", pivot.CreatedAt, pivot.ID, cursor.QuerySnapshot{})
	require.NoError(t, err)

	// Filter with both --after (looser) and cursor (stricter): cursor wins.
	older := base.Add(-30 * time.Minute)
	c, _, _ := env.mgr.GetOrInit("comp")
	filter := gateByCursor(storage.ObjectFilter{
		After:  &older,
		Limit:  100,
		Status: "all",
	}, c)
	objs, _, err := driver.Objects().List(ctx, filter)
	require.NoError(t, err)
	// Items strictly after pivot.CreatedAt: 2 (indexes 3, 4).
	assert.Len(t, objs, 2)
	assert.Equal(t, all[3].ID, objs[0].ID)
}

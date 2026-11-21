//go:build cursor_e2e

package cursor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCursor_ComposesWithMention: cursor + --mention keeps the mention
// flag in the filter, stores it in the query snapshot, and gates by time.
// Against real storage the composed filter returns only objects that carry
// the mention AND sit past the cursor; a non-mentioning object is excluded.
func TestCursor_ComposesWithMention(t *testing.T) {
	env := newCursorEnv(t)
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()

	// Chronological: [0] mentions (becomes the cursor head), [1] does not,
	// [2] mentions (the only expected hit).
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i, mentioned := range []bool{true, false, true} {
		ko := storageutil.BuildGraphKO(fmt.Sprintf("ko-mention-%d", i), "text", "body")
		if mentioned {
			ko.Mentions = mentions.ParseSlice([]string{"@client.acme"})
		}
		ko.CreatedAt = base.Add(time.Duration(i) * time.Second)
		ko.UpdatedAt = ko.CreatedAt
		require.NoError(t, driver.Objects().Create(ctx, ko))
	}

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

	// Mention + time gate: only the later mentioning object. The head is
	// gated out; the non-mentioning object is filtered out.
	got, _, err := driver.Objects().List(ctx, filter)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "ko-mention-2", got[0].ID)

	// Without the mention filter the time gate alone keeps both later
	// objects, including the non-mentioning one.
	filter.Mention = ""
	all, _, err := driver.Objects().List(ctx, filter)
	require.NoError(t, err)
	assert.Len(t, all, 2, "cursor time gate excludes the advanced-to head")
}

// TestCursor_ComposesWithTag: cursor+--tagged.
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
		Tags: tagfilter.One("alpha"), Limit: 100, Status: "all", Sort: "created_at", Dir: "asc",
	})
	require.NoError(t, err)
	require.Len(t, objs, 2)
	first := objs[0]
	_, err = env.mgr.Advance("tagged", first.CreatedAt, first.ID, cursor.QuerySnapshot{Tag: []string{"alpha"}})
	require.NoError(t, err)

	c, _, _ := env.mgr.GetOrInit("tagged")
	filter := gateByCursor(storage.ObjectFilter{Tags: tagfilter.One("alpha"), Limit: 100, Status: "all"}, c)
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

// TestCursor_ComposesWithQAndAfter: cursor + --since compose; cursor's
// last_seen_at takes precedence when stricter than --since.
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

	// Filter with both --since (looser) and cursor (stricter): cursor wins.
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

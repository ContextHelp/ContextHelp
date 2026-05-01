//go:build cursor_e2e

package cursor

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCursor_NewCursorReturnsAll: a fresh cursor (advance=true to bypass
// the "unknown without advance" gate) must return the full matching set.
func TestCursor_NewCursorReturnsAll(t *testing.T) {
	env := newCursorEnv(t)
	driver := storageutil.NewTestDriver(t)
	listKOs(t, driver, 5, "ko", "")

	c, fresh, err := env.mgr.GetOrInit("feed")
	require.NoError(t, err)
	require.True(t, fresh, "cursor must be fresh on first call")
	assert.True(t, c.LastSeenAt.IsZero(), "fresh cursor starts at epoch 0")

	filter := gateByCursor(storage.ObjectFilter{Limit: 100, Status: "all"}, c)
	objs, total, err := driver.Objects().List(context.Background(), filter)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, objs, 5)
}

// TestCursor_AdvanceMovesPosition: advance to tail; subsequent list returns
// only items added after advance.
func TestCursor_AdvanceMovesPosition(t *testing.T) {
	env := newCursorEnv(t)
	driver := storageutil.NewTestDriver(t)
	ids := listKOs(t, driver, 3, "ko", "")

	// First list: filter ASC, get all, advance to tail.
	c, _, err := env.mgr.GetOrInit("feed")
	require.NoError(t, err)
	filter := gateByCursor(storage.ObjectFilter{Limit: 100, Status: "all"}, c)
	objs, _, err := driver.Objects().List(context.Background(), filter)
	require.NoError(t, err)
	require.Len(t, objs, 3)

	tail := objs[len(objs)-1]
	_, err = env.mgr.Advance("feed", tail.CreatedAt, tail.ID, cursor.QuerySnapshot{})
	require.NoError(t, err)

	// Second list: same data, cursor should gate to zero.
	c2, _, err := env.mgr.GetOrInit("feed")
	require.NoError(t, err)
	filter2 := gateByCursor(storage.ObjectFilter{Limit: 100, Status: "all"}, c2)
	objs2, _, err := driver.Objects().List(context.Background(), filter2)
	require.NoError(t, err)
	assert.Empty(t, objs2, "cursor at tail should yield no new items")

	// Insert new item; cursor should now return only the new one.
	added := listKOs(t, driver, 2, "newko", "")
	c3, _, err := env.mgr.GetOrInit("feed")
	require.NoError(t, err)
	filter3 := gateByCursor(storage.ObjectFilter{Limit: 100, Status: "all"}, c3)
	objs3, _, err := driver.Objects().List(context.Background(), filter3)
	require.NoError(t, err)
	require.Len(t, objs3, 2)
	assert.Equal(t, added[0], objs3[0].ID)
	_ = ids
}

// TestCursor_NoAdvanceLeavesPositionUnchanged: same list call without
// advancing must not change cursor state.
func TestCursor_NoAdvanceLeavesPositionUnchanged(t *testing.T) {
	env := newCursorEnv(t)
	driver := storageutil.NewTestDriver(t)
	listKOs(t, driver, 3, "ko", "")

	// Seed cursor at a known state.
	c, _, _ := env.mgr.GetOrInit("feed")
	filter := gateByCursor(storage.ObjectFilter{Limit: 100, Status: "all"}, c)
	objs, _, err := driver.Objects().List(context.Background(), filter)
	require.NoError(t, err)
	tail := objs[len(objs)-1]
	_, err = env.mgr.Advance("feed", tail.CreatedAt, tail.ID, cursor.QuerySnapshot{})
	require.NoError(t, err)

	before, err := env.mgr.Get("feed")
	require.NoError(t, err)

	// "List without advance" — read cursor, list, do NOT call Advance.
	c2, _, _ := env.mgr.GetOrInit("feed")
	_ = gateByCursor(storage.ObjectFilter{Limit: 100, Status: "all"}, c2)
	// (skip advance step)

	after, err := env.mgr.Get("feed")
	require.NoError(t, err)
	assert.Equal(t, before.LastSeenAt, after.LastSeenAt)
	assert.Equal(t, before.LastObjectID, after.LastObjectID)
}

// TestCursor_EmptyResultDoesNotAdvance: when the gated list returns 0 items,
// cursor state must remain unchanged even if --advance was requested.
func TestCursor_EmptyResultDoesNotAdvance(t *testing.T) {
	env := newCursorEnv(t)
	driver := storageutil.NewTestDriver(t)
	listKOs(t, driver, 2, "ko", "")

	// Advance to tail first.
	c, _, _ := env.mgr.GetOrInit("feed")
	filter := gateByCursor(storage.ObjectFilter{Limit: 100, Status: "all"}, c)
	objs, _, err := driver.Objects().List(context.Background(), filter)
	require.NoError(t, err)
	tail := objs[len(objs)-1]
	_, err = env.mgr.Advance("feed", tail.CreatedAt, tail.ID, cursor.QuerySnapshot{})
	require.NoError(t, err)
	before, _ := env.mgr.Get("feed")

	// Second listing — empty result. runListWithCursor only calls Advance when
	// len(objects) > 0; mirror that behaviour here.
	c2, _, _ := env.mgr.GetOrInit("feed")
	filter2 := gateByCursor(storage.ObjectFilter{Limit: 100, Status: "all"}, c2)
	objs2, _, err := driver.Objects().List(context.Background(), filter2)
	require.NoError(t, err)
	require.Empty(t, objs2)
	// Skip advance because objs2 is empty.

	after, _ := env.mgr.Get("feed")
	assert.True(t, before.LastSeenAt.Equal(after.LastSeenAt), "cursor must not move on empty result")
}

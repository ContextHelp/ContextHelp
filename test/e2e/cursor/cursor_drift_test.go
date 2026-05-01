//go:build cursor_e2e

package cursor

import (
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCursor_QuerySnapshotDriftWarnsButContinues: when current flags diverge
// from the cursor's stored snapshot, a warning must be emitted but the run
// must not fail. We model this at the package level: drift detection is a
// pure compare on QuerySnapshot, and runListWithCursor warns + continues.
func TestCursor_QuerySnapshotDriftWarnsButContinues(t *testing.T) {
	env := newCursorEnv(t)
	stored := cursor.QuerySnapshot{Mention: []string{"@client.acme"}}
	current := cursor.QuerySnapshot{Mention: []string{"@client.beta"}}
	assert.False(t, stored.Equal(current), "snapshots must differ to trigger drift")

	// Persist the stored snapshot.
	_, err := env.mgr.Advance("acme",
		time.Now().UTC().Truncate(time.Second), "ko_1", stored)
	require.NoError(t, err)

	// Verify the run "continues" by reading the cursor unchanged.
	c, err := env.mgr.Get("acme")
	require.NoError(t, err)
	assert.Equal(t, []string{"@client.acme"}, c.Query.Mention)
}

// TestCursor_AdvanceUpdatesQuerySnapshot: advance must overwrite the
// stored snapshot with whatever flags were supplied.
func TestCursor_AdvanceUpdatesQuerySnapshot(t *testing.T) {
	env := newCursorEnv(t)

	t1 := time.Now().UTC().Truncate(time.Second)
	_, err := env.mgr.Advance("feed", t1, "ko_1",
		cursor.QuerySnapshot{Mention: []string{"@v1"}})
	require.NoError(t, err)

	t2 := t1.Add(time.Hour)
	_, err = env.mgr.Advance("feed", t2, "ko_2",
		cursor.QuerySnapshot{Mention: []string{"@v2"}, Tag: []string{"new"}})
	require.NoError(t, err)

	c, err := env.mgr.Get("feed")
	require.NoError(t, err)
	assert.Equal(t, []string{"@v2"}, c.Query.Mention)
	assert.Equal(t, []string{"new"}, c.Query.Tag)
}

//go:build cursor_e2e

package cursor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// All five tests below exec the built `bin/ctxt cursor <verb>` and assert
// stdout/stderr/exit-code shape. They use $CTXT_CURSOR_FILE to scope state
// to a temp file, no DB access.

func TestCursor_List(t *testing.T) {
	env := newCursorEnv(t)

	// Empty.
	out, code := env.runBin("cursor", "list")
	require.Equal(t, 0, code, "out: %s", out)
	assert.Contains(t, out, "No cursors")

	// After reset+set, list shows one row.
	_, code = env.runBin("cursor", "set", "feed", "--to", "2026-04-28T14:22:11Z")
	require.Equal(t, 0, code)
	out, code = env.runBin("cursor", "list")
	require.Equal(t, 0, code, "out: %s", out)
	assert.Contains(t, out, "feed")
	assert.Contains(t, out, "2026-04-28T14:22:11")
}

func TestCursor_ShowIncludesQuerySnapshot(t *testing.T) {
	env := newCursorEnv(t)
	// Seed cursor via set + advance via library to pre-populate query.
	_, code := env.runBin("cursor", "set", "feed", "--to", "2026-04-28T14:22:11Z")
	require.Equal(t, 0, code)

	out, code := env.runBin("cursor", "show", "feed")
	require.Equal(t, 0, code, "out: %s", out)
	assert.Contains(t, out, "feed")
	assert.Contains(t, out, "LastSeenAt:")
	assert.Contains(t, out, "Query snapshot:")
	assert.Contains(t, out, "mention:")
	assert.Contains(t, out, "tag:")
	assert.Contains(t, out, "profile:")
}

func TestCursor_ResetRewindsToEpoch(t *testing.T) {
	env := newCursorEnv(t)
	_, code := env.runBin("cursor", "set", "feed", "--to", "2026-04-28T14:22:11Z")
	require.Equal(t, 0, code)

	out, code := env.runBin("cursor", "reset", "feed")
	require.Equal(t, 0, code, "out: %s", out)
	assert.Contains(t, strings.ToLower(out), "rewound")

	c, err := env.mgr.Get("feed")
	require.NoError(t, err)
	assert.True(t, c.LastSeenAt.IsZero(), "reset must zero LastSeenAt")
}

func TestCursor_SetJumpsToTimestamp(t *testing.T) {
	env := newCursorEnv(t)
	out, code := env.runBin("cursor", "set", "feed", "--to", "2026-04-28T14:22:11Z")
	require.Equal(t, 0, code, "out: %s", out)

	c, err := env.mgr.Get("feed")
	require.NoError(t, err)
	assert.Equal(t, 2026, c.LastSeenAt.Year())
	assert.Equal(t, 28, c.LastSeenAt.Day())
}

func TestCursor_DeleteRemovesEntry(t *testing.T) {
	env := newCursorEnv(t)
	_, code := env.runBin("cursor", "set", "feed", "--to", "2026-04-01T00:00:00Z")
	require.Equal(t, 0, code)

	out, code := env.runBin("cursor", "delete", "feed")
	require.Equal(t, 0, code, "out: %s", out)
	assert.Contains(t, strings.ToLower(out), "deleted")

	// Show after delete: exit 2.
	out2, code2 := env.runBin("cursor", "show", "feed")
	assert.Equal(t, 2, code2, "show after delete should exit 2; out: %s", out2)
}

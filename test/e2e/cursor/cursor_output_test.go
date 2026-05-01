//go:build cursor_e2e

package cursor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCursor_JSONOutputShape: `ctxt cursor list --format json` must emit
// {"cursors": [...]}. The list-with-cursor JSON shape ({cursor, items,
// advanced}) is exercised via the runtime CLI but not asserted here
// because it requires a running DB; covered by manual smoke + the
// runListWithCursor unit-paths.
func TestCursor_JSONOutputShape(t *testing.T) {
	env := newCursorEnv(t)

	// Empty list.
	out, code := env.runBin("cursor", "list", "--format", "json")
	require.Equal(t, 0, code, "out: %s", out)

	var emptyShape struct {
		Cursors []map[string]any `json:"cursors"`
	}
	parseJSON(t, out, &emptyShape)
	assert.NotNil(t, emptyShape.Cursors, "must have cursors key")

	// Populated.
	_, code = env.runBin("cursor", "set", "feed", "--to", "2026-04-28T14:22:11Z")
	require.Equal(t, 0, code)

	out, code = env.runBin("cursor", "list", "--format", "json")
	require.Equal(t, 0, code, "out: %s", out)

	var shape struct {
		Cursors []map[string]any `json:"cursors"`
	}
	parseJSON(t, out, &shape)
	require.Len(t, shape.Cursors, 1)
	assert.Equal(t, "feed", shape.Cursors[0]["name"])
	assert.NotEmpty(t, shape.Cursors[0]["last_seen_at"])
}

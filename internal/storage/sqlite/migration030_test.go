package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigration030_StampsBarePipelineNames inserts objects with bare and
// already-stamped pipeline values, re-runs the stamping statement (it's
// idempotent), and asserts every non-empty pipeline ends up with an "@vN"
// suffix exactly once.
func TestMigration030_StampsBarePipelineNames(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	cases := []struct {
		id    string
		input string
		want  string
	}{
		{"obj_m029_a", "text.short", "text.short@v0"},                // bare → stamped
		{"obj_m029_b", "text.long", "text.long@v0"},                  // bare → stamped
		{"obj_m029_c", "text.short@v1", "text.short@v1"},             // already versioned, untouched
		{"obj_m029_d", "doc.pdf@v2", "doc.pdf@v2"},                   // already versioned, untouched
		{"obj_m029_e", "", ""},                                       // empty stays empty
	}

	for _, c := range cases {
		_, err := d.db.ExecContext(ctx, `
			INSERT INTO objects (id, type, created_at, updated_at, pipeline)
			VALUES (?, ?, ?, ?, ?)`,
			c.id, "note", now, now, c.input,
		)
		require.NoError(t, err, "insert %s", c.id)
	}

	// Run the stamping SQL again — must be idempotent.
	_, err := d.db.ExecContext(ctx, migration030)
	require.NoError(t, err)

	for _, c := range cases {
		var got string
		err := d.db.QueryRowContext(ctx,
			`SELECT pipeline FROM objects WHERE id = ?`, c.id,
		).Scan(&got)
		require.NoError(t, err, "select %s", c.id)
		assert.Equal(t, c.want, got, "row %s", c.id)
	}

	// Run a third time to prove idempotency stays true even after the row
	// has already been stamped to "@v0".
	_, err = d.db.ExecContext(ctx, migration030)
	require.NoError(t, err)
	var got string
	require.NoError(t, d.db.QueryRowContext(ctx,
		`SELECT pipeline FROM objects WHERE id = ?`, "obj_m029_a",
	).Scan(&got))
	assert.Equal(t, "text.short@v0", got, "second migration run must not double-stamp")
}

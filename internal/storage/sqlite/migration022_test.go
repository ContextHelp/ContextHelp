package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration022_GraphColumns(t *testing.T) {
	d := newTestDriver(t) // runs Init (Migrate) internally
	ctx := context.Background()

	// --- graph_json column must exist on objects ---
	// Insert a minimal object with explicit graph_json.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO objects
		    (id, type, created_at, updated_at, graph_json)
		VALUES
		    (?, ?, ?, ?, ?)`,
		"obj_test022_a", "note", now, now, `{"nodes":[],"edges":[]}`,
	)
	require.NoError(t, err, "INSERT with graph_json must succeed after migration 022")

	// Insert a row without graph_json to verify backfill applied '{}' to
	// objects already present at migration time.  In a fresh DB every row
	// is new so backfill ran on zero rows, but the DEFAULT NULL + backfill
	// logic should leave newly inserted rows with NULL (not '{}') because
	// the UPDATE only ran during migration.  What matters here is that the
	// column exists and accepts values.
	_, err = d.db.ExecContext(ctx, `
		INSERT INTO objects
		    (id, type, created_at, updated_at)
		VALUES
		    (?, ?, ?, ?)`,
		"obj_test022_b", "note", now, now,
	)
	require.NoError(t, err, "INSERT without graph_json must succeed (column nullable)")

	var gj *string
	require.NoError(t, d.db.QueryRowContext(ctx,
		`SELECT graph_json FROM objects WHERE id = ?`, "obj_test022_b",
	).Scan(&gj))
	// Default is NULL; no automatic backfill for rows inserted after migration.
	assert.Nil(t, gj, "graph_json should be NULL for rows inserted without a value")

	// --- object_nodes table must exist ---
	var tableCount int
	require.NoError(t, d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='object_nodes'`,
	).Scan(&tableCount))
	assert.Equal(t, 1, tableCount, "object_nodes table must exist after migration 022")

	// --- object_nodes insert + cascade delete ---
	_, err = d.db.ExecContext(ctx, `
		INSERT INTO object_nodes
		    (id, object_id, node_type, ordinal, content, created_at)
		VALUES
		    (?, ?, ?, ?, ?, ?)`,
		"nd_test022_1", "obj_test022_a", "section", 0, "intro text", now,
	)
	require.NoError(t, err, "INSERT into object_nodes must succeed")

	// Cascade: delete parent object → node row must be gone.
	_, err = d.db.ExecContext(ctx, `DELETE FROM objects WHERE id = ?`, "obj_test022_a")
	require.NoError(t, err)

	var nodeCount int
	require.NoError(t, d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM object_nodes WHERE object_id = ?`, "obj_test022_a",
	).Scan(&nodeCount))
	assert.Equal(t, 0, nodeCount, "ON DELETE CASCADE must remove child nodes")

	// --- indexes must exist ---
	var idxCount int
	require.NoError(t, d.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='index'
		  AND name IN ('idx_object_nodes_object_id','idx_object_nodes_node_type')`,
	).Scan(&idxCount))
	assert.Equal(t, 2, idxCount, "both object_nodes indexes must exist")
}

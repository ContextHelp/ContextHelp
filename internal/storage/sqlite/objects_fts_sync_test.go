package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// objects_fts is an external-content FTS5 table (content='objects'). Its
// index only stays coherent when every write to objects.projected_fts_body
// is mirrored into objects_fts within the same transaction, and — because
// FTS5 reads the *current* content row to learn which tokens to remove —
// the FTS delete has to run BEFORE the content row is deleted or rewritten.
// These tests pin that contract for Delete and Update.

// assertFTSIntegrity runs the FTS5 integrity-check with rank=1 so that the
// index is verified against the content table, not just internally.
func assertFTSIntegrity(t *testing.T, d *Driver) {
	t.Helper()
	_, err := d.db.ExecContext(context.Background(),
		"INSERT INTO objects_fts(objects_fts, rank) VALUES('integrity-check', 1)")
	require.NoError(t, err, "objects_fts must be consistent with the objects content table")
}

func TestDelete_RemovesFTSEntry(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	const token = "quokkazephyr"
	obj := makeFTSObject("fts-del-1", "article", "notes about the "+token+" migration")
	require.NoError(t, d.Objects().Create(ctx, obj))

	// Sibling row that must keep working after the delete.
	other := makeFTSObject("fts-del-2", "article", "unrelated giraffe content")
	require.NoError(t, d.Objects().Create(ctx, other))

	results, err := d.Objects().FTSSearch(ctx, token, storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1, "object must be indexed before delete")

	require.NoError(t, d.Objects().Delete(ctx, obj.ID))

	results, err = d.Objects().FTSSearch(ctx, token, storage.ObjectFilter{Limit: 10})
	require.NoError(t, err, "FTS query touching a deleted rowid must not fail")
	assert.Empty(t, results, "deleted object must not be returned by FTS")

	// The subquery form used by the search compiler (similar==) reads the
	// UNINDEXED id column straight from the content table.
	var n int
	err = d.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM objects WHERE id IN (SELECT id FROM objects_fts WHERE objects_fts MATCH ?)",
		token).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	results, err = d.Objects().FTSSearch(ctx, "giraffe", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, other.ID, results[0].ID)

	assertFTSIntegrity(t, d)
}

// Objects with an empty projected body are never inserted into objects_fts.
// Deleting or updating them must not issue FTS deletes for a rowid the index
// never held, which would skew FTS5's row/token totals.
func TestDeleteAndUpdate_UnindexedObjectKeepsFTSIntegrity(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	empty := &storage.KnowledgeObject{ID: "fts-empty-1", Type: "article", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, d.Objects().Create(ctx, empty))
	var body string
	require.NoError(t, d.db.QueryRowContext(ctx,
		"SELECT projected_fts_body FROM objects WHERE id = ?", empty.ID).Scan(&body))
	require.Empty(t, body, "precondition: object has no FTS body")

	indexed := makeFTSObject("fts-empty-2", "article", "indexed sibling with pangolin")
	require.NoError(t, d.Objects().Create(ctx, indexed))

	// Update empty → still empty, then delete.
	empty.UpdatedAt = now.Add(time.Second)
	require.NoError(t, d.Objects().Update(ctx, empty))
	require.NoError(t, d.Objects().Delete(ctx, empty.ID))

	results, err := d.Objects().FTSSearch(ctx, "pangolin", storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assertFTSIntegrity(t, d)
}

func TestUpdate_ReplacesFTSEntry(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()

	const oldToken = "aardvarklumen"
	const newToken = "basiliskember"
	obj := makeFTSObject("fts-upd-1", "article", "first body mentions "+oldToken)
	require.NoError(t, d.Objects().Create(ctx, obj))

	results, err := d.Objects().FTSSearch(ctx, oldToken, storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1)

	obj.Graph.Nodes[0].Content = "second body mentions " + newToken
	obj.UpdatedAt = time.Now().Truncate(time.Second)
	require.NoError(t, d.Objects().Update(ctx, obj))

	results, err = d.Objects().FTSSearch(ctx, oldToken, storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, results, "old body tokens must be removed from FTS on update")

	results, err = d.Objects().FTSSearch(ctx, newToken, storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1, "new body tokens must be indexed on update")
	assert.Equal(t, obj.ID, results[0].ID)

	assertFTSIntegrity(t, d)
}

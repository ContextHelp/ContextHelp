package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/stretchr/testify/require"
)

// pruneBlobFixture is a store whose blob store is the one prune opens:
// the local backend under --data-dir.
type pruneBlobFixture struct {
	*testDB
	dataDir string
	blobs   storage.BlobStore
}

func setupPruneBlobDB(t *testing.T) *pruneBlobFixture {
	t.Helper()
	db := setupTestDB(t)
	dataDir := t.TempDir()
	bs, err := local.New(filepath.Join(dataDir, "blobs"))
	require.NoError(t, err)
	return &pruneBlobFixture{testDB: db, dataDir: dataDir, blobs: bs}
}

// seed stores an object whose content is externalized under key.
func (f *pruneBlobFixture) seed(t *testing.T, id, key string, created time.Time) {
	t.Helper()
	ctx := context.Background()
	if ok, _ := f.blobs.Exists(ctx, key); !ok {
		require.NoError(t, f.blobs.Put(ctx, key, strings.NewReader("content of "+key), storage.BlobMeta{}))
	}
	require.NoError(t, f.Driver.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: id, Type: "text", RawContent: "blob://" + key, TextContent: "content of " + key,
		Metadata:  map[string]any{"blob_key": key},
		CreatedAt: created, UpdatedAt: created,
	}))
}

func (f *pruneBlobFixture) prune(t *testing.T) string {
	t.Helper()
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	_, err := w.WriteString("y\n")
	require.NoError(t, err)
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()
	out, err := f.run("--data-dir", f.dataDir, "housekeeping", "prune", "--before", "2024-01-01")
	require.NoError(t, err)
	return out
}

func (f *pruneBlobFixture) blobExists(t *testing.T, key string) bool {
	t.Helper()
	ok, err := f.blobs.Exists(context.Background(), key)
	require.NoError(t, err)
	return ok
}

// Prune deletes a pruned object's blob, and keeps a blob another
// object still refers to.
func TestHousekeepingPruneDeletesBlobs(t *testing.T) {
	f := setupPruneBlobDB(t)
	old := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Now().UTC().Truncate(time.Second)
	f.seed(t, "obj_old_own", "keyown", old)
	f.seed(t, "obj_old_shared", "keyshared", old)
	f.seed(t, "obj_new_shared", "keyshared", recent)

	out := f.prune(t)
	require.Contains(t, out, "Pruned 2 objects")
	require.NotContains(t, out, "orphan")

	_, err := f.Driver.Objects().Get(context.Background(), "obj_old_own")
	require.ErrorIs(t, err, storage.ErrNotFound)
	require.False(t, f.blobExists(t, "keyown"), "prune left the pruned object's blob behind")
	require.True(t, f.blobExists(t, "keyshared"), "prune deleted a blob another object refers to")
}

// Prune deletes a pruned object's edges like DELETE /objects/{id}: no
// foreign key cascades them.
func TestHousekeepingPruneDeletesEdges(t *testing.T) {
	f := setupPruneBlobDB(t)
	ctx := context.Background()
	old := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Now().UTC().Truncate(time.Second)
	f.seed(t, "obj_old_linked", "keylinked", old)
	f.seed(t, "obj_new_linked", "keynew", recent)
	for _, e := range []*storage.Edge{
		{ID: "edge_out", FromType: "object", FromID: "obj_old_linked", ToType: "object", ToID: "obj_new_linked", EdgeType: "links"},
		{ID: "edge_in", FromType: "object", FromID: "obj_new_linked", ToType: "object", ToID: "obj_old_linked", EdgeType: "links"},
	} {
		e.Weight, e.CreatedAt = 1, recent
		require.NoError(t, f.Driver.Edges().Create(ctx, e))
	}

	out := f.prune(t)
	require.Contains(t, out, "Pruned 1 objects")

	from, err := f.Driver.Edges().ListFrom(ctx, "object", "obj_old_linked")
	require.NoError(t, err)
	to, err := f.Driver.Edges().ListTo(ctx, "object", "obj_old_linked")
	require.NoError(t, err)
	require.Empty(t, append(from, to...), "prune left the pruned object's edges behind")
}

// A blob that cannot be deleted leaves an orphan, never a row whose ref
// names no blob, and prune names it.
func TestHousekeepingPruneReportsOrphanBlobs(t *testing.T) {
	f := setupPruneBlobDB(t)
	old := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	f.seed(t, "obj_old_stuck", "keystuck", old)
	// local layout: <key[0:2]>/<key[2:4]>/<key>; a read-only shard dir
	// makes the delete fail.
	shard := filepath.Join(f.dataDir, "blobs", "ke", "ys")
	require.NoError(t, os.Chmod(shard, 0o500))
	t.Cleanup(func() { _ = os.Chmod(shard, 0o700) })

	out := f.prune(t)
	require.Contains(t, out, "Pruned 1 objects")
	require.Contains(t, out, "1 orphan blob")
	require.Contains(t, out, "keystuck")

	_, err := f.Driver.Objects().Get(context.Background(), "obj_old_stuck")
	require.ErrorIs(t, err, storage.ErrNotFound, "the row outlived its failed blob delete")
	require.True(t, f.blobExists(t, "keystuck"))
}

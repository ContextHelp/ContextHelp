package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
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

// seed stores an object whose content is externalized under key, in a
// blob written 3h ago: past the grace period a delete waits out.
func (f *pruneBlobFixture) seed(t *testing.T, id, key string, created time.Time) {
	t.Helper()
	f.seedAged(t, id, key, created, 3*time.Hour)
}

// seedAged is seed with a blob written age ago.
func (f *pruneBlobFixture) seedAged(t *testing.T, id, key string, created time.Time, age time.Duration) {
	t.Helper()
	ctx := context.Background()
	if ok, _ := f.blobs.Exists(ctx, key); !ok {
		f.putAgedBlob(t, key, age)
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
// object still refers to or one written inside the grace period.
func TestHousekeepingPruneDeletesBlobs(t *testing.T) {
	f := setupPruneBlobDB(t)
	old := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Now().UTC().Truncate(time.Second)
	f.seed(t, "obj_old_own", "keyown", old)
	f.seed(t, "obj_old_shared", "keyshared", old)
	f.seed(t, "obj_new_shared", "keyshared", recent)
	f.seedAged(t, "obj_old_fresh", "keyfresh", old, 10*time.Minute)

	out := f.prune(t)
	require.Contains(t, out, "Pruned 3 objects")
	require.NotContains(t, out, "orphan")

	_, err := f.Driver.Objects().Get(context.Background(), "obj_old_own")
	require.ErrorIs(t, err, storage.ErrNotFound)
	require.False(t, f.blobExists(t, "keyown"), "prune left the pruned object's blob behind")
	require.True(t, f.blobExists(t, "keyshared"), "prune deleted a blob another object refers to")
	require.True(t, f.blobExists(t, "keyfresh"), "prune deleted a blob inside the grace period")
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

// prune --dry-run lists the blobs a real prune would delete: those no
// object outside the pruned set refers to. It deletes none of them.
func TestHousekeepingPruneDryRunListsBlobs(t *testing.T) {
	f := setupPruneBlobDB(t)
	old := time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Now().UTC().Truncate(time.Second)
	f.seed(t, "obj_old_own", "keyown", old)
	f.seed(t, "obj_old_both_a", "keyboth", old)
	f.seed(t, "obj_old_both_b", "keyboth", old)
	f.seed(t, "obj_old_shared", "keyshared", old)
	f.seed(t, "obj_new_shared", "keyshared", recent)
	// Written inside the grace period: a real prune leaves it to the
	// housekeeping sweep.
	f.seedAged(t, "obj_old_fresh", "keyfresh", old, 10*time.Minute)

	args := []string{"--data-dir", f.dataDir, "housekeeping", "prune", "--before", "2024-01-01", "--dry-run"}
	out, err := f.run(args...)
	require.NoError(t, err)
	require.Contains(t, out, "delete blob keyown")
	require.Contains(t, out, "delete blob keyboth")
	require.NotContains(t, out, "keyshared", "a blob an unpruned object refers to is not deleted")
	require.NotContains(t, out, "delete blob keyfresh", "a blob inside the grace period is not deleted")
	require.Contains(t, out, "Would delete 2 blob(s)")

	out, err = f.run(append(args, "--format", "json")...)
	require.NoError(t, err)
	var plan cliformat.Plan
	require.NoError(t, json.Unmarshal([]byte(out), &plan), out)
	var blobs []string
	for _, a := range plan.Effects {
		if strings.HasPrefix(a.Target, "blob:") {
			blobs = append(blobs, a.Target)
		}
	}
	require.ElementsMatch(t, []string{"blob:keyown", "blob:keyboth"}, blobs)

	for _, key := range []string{"keyown", "keyboth", "keyshared", "keyfresh"} {
		require.True(t, f.blobExists(t, key), "dry-run deleted blob %s", key)
	}
}

// putAgedBlob stores a blob in the local store and backdates it.
func (f *pruneBlobFixture) putAgedBlob(t *testing.T, key string, age time.Duration) {
	t.Helper()
	require.NoError(t, f.blobs.Put(context.Background(), key, strings.NewReader("content of "+key), storage.BlobMeta{}))
	at := time.Now().Add(-age)
	// local layout: <key[0:2]>/<key[2:4]>/<key>
	require.NoError(t, os.Chtimes(filepath.Join(f.dataDir, "blobs", key[:2], key[2:4], key), at, at))
}

// The full maintenance pass sweeps the blob store: it deletes blobs no
// object refers to once they are older than the grace period, and
// keeps referenced and young ones. --dry-run deletes nothing.
func TestHousekeepingRunSweepsOrphanBlobs(t *testing.T) {
	f := setupPruneBlobDB(t)
	f.seed(t, "obj_live", "keylive", time.Now().UTC())
	f.putAgedBlob(t, "keyorphan", 3*time.Hour)
	f.putAgedBlob(t, "keyyoung", 10*time.Minute)
	f.putAgedBlob(t, "keylive", 3*time.Hour)

	out, err := f.run("--data-dir", f.dataDir, "housekeeping", "run", "--dry-run", "--format", "json")
	require.NoError(t, err)
	var plan cliformat.Plan
	require.NoError(t, json.Unmarshal([]byte(out), &plan), out)
	var sweep *cliformat.Action
	for i, a := range plan.Effects {
		if a.Target == "blob-store" {
			sweep = &plan.Effects[i]
		}
	}
	require.NotNil(t, sweep, "plan has no blob-store sweep action: %v", plan.Effects)
	require.EqualValues(t, 1, sweep.Count)
	require.True(t, f.blobExists(t, "keyorphan"), "dry run deleted an orphan blob")

	out, err = f.run("--data-dir", f.dataDir, "housekeeping", "run", "--format", "json")
	require.NoError(t, err)
	var summary map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &summary), out)
	require.EqualValues(t, 1, summary["orphan_blobs"])
	require.EqualValues(t, 1, summary["orphan_blobs_young"])
	require.False(t, f.blobExists(t, "keyorphan"), "run left an old orphan blob behind")
	require.True(t, f.blobExists(t, "keyyoung"), "run deleted an orphan blob inside the grace period")
	require.True(t, f.blobExists(t, "keylive"), "run deleted a blob an object refers to")
}

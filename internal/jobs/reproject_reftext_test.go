package jobs

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func TestReproject_RepairsRefText_SQLite(t *testing.T) {
	drv, pdb := sqliteReprojectDriver(t)
	runReprojectRepairsRefText(t, drv, pdb)
}

// seedRefText stores an externalized object the way storage left one
// before projection v3: text_content is the blob ref, the projection is
// stale. content goes in the blob store unless it is empty (a lost blob).
func seedRefText(t *testing.T, drv storage.StorageDriver, pdb storagetest.ProjectionDB, id, key, content string) {
	t.Helper()
	ctx := context.Background()
	if content != "" {
		require.NoError(t, drv.Blobs().Put(ctx, key, strings.NewReader(content), storage.BlobMeta{}))
	}
	obj := &storage.KnowledgeObject{ID: id, Type: "note", RawContent: blobref.For(key),
		Metadata: map[string]any{"blob_key": key}, Summaries: []string{"quokka summary"}}
	require.NoError(t, drv.Objects().Create(ctx, obj))
	obj.TextContent = obj.RawContent
	require.NoError(t, drv.Objects().Update(ctx, obj))
	pdb.Stale(t, id, "quokka summary "+obj.RawContent, "v2")
}

// runReprojectRepairsRefText runs the re-projection over rows written
// before projection v3, whose text_content is their blob ref: each gets
// the first TextCap bytes of its content back as text, flagged a prefix,
// and is indexed by it. A row whose blob is lost keeps its ref, is
// counted, and does not fail the run.
func runReprojectRepairsRefText(t *testing.T, drv storage.StorageDriver, pdb storagetest.ProjectionDB) {
	ctx := context.Background()
	seedRefText(t, drv, pdb, "reftext-big", "refbigkey", "numbat wombat "+strings.Repeat("filler ", 50))
	seedRefText(t, drv, pdb, "reftext-small", "refsmallkey", "bilby burrow")
	seedRefText(t, drv, pdb, "reftext-lost", "reflostkey", "")
	require.NoError(t, indexsig.Upsert(ctx, pdb.DB, pdb.Dialect, indexsig.FTSSignatureID, "stamped-by-v2", "projection=v2"))

	r, err := NewReprojector(drv, upgrade.NewManager(""), &recordingBus{})
	require.NoError(t, err)
	r.TextCap = 13
	res, err := r.Run(ctx, "job-reftext")
	require.NoError(t, err)
	require.Equal(t, 2, res.TextRepaired)
	require.Equal(t, 1, res.TextRepairFailed)
	require.Equal(t, []string{"reftext-lost"}, res.TextRepairFailedObjects)

	big, err := drv.Objects().Get(ctx, "reftext-big")
	require.NoError(t, err)
	require.Equal(t, "numbat wombat", big.TextContent, "text not repaired to a TextCap prefix")
	require.Equal(t, true, big.Metadata[blobref.MetaTextTruncated])
	small, err := drv.Objects().Get(ctx, "reftext-small")
	require.NoError(t, err)
	require.Equal(t, "bilby burrow", small.TextContent)
	lost, err := drv.Objects().Get(ctx, "reftext-lost")
	require.NoError(t, err)
	require.Equal(t, "blob://reflostkey", lost.TextContent)

	storagetest.AssertFTS(t, drv, "numbat", "[reftext-big]")
	storagetest.AssertFTS(t, drv, "bilby", "[reftext-small]")
	storagetest.AssertFTS(t, drv, "reflostkey", "[]")
	require.True(t, signatureMatches(t, pdb), "a lost blob kept the signature unstamped")
	n, err := drv.Objects().(storage.ProjectionStore).CountStaleProjections(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
}

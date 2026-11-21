package jobs

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// runBinaryJob runs one base64 job for bin through the worker and returns
// the object it stored.
func runBinaryJob(t *testing.T, driver storage.StorageDriver, id string, bin []byte) *storage.KnowledgeObject {
	t.Helper()
	q := NewQueue(driver.Jobs())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	job := makeJob(id)
	job.Type = "ingest:text" + storage.JobTypeBase64
	job.Payload = base64.StdEncoding.EncodeToString(bin)
	job.Source = "/tmp/x.png"
	require.NoError(t, q.Enqueue(ctx, job))

	pool := NewWorkerPool(q, builtins.Registry(), driver, 1, nil, defaultTestJobsCfg())
	go func() {
		for ctx.Err() == nil {
			if got, _ := q.Get(ctx, id); got != nil && (got.Status == storage.JobCompleted || got.Status == storage.JobFailed) {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	pool.Start(ctx)

	got, err := q.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, storage.JobCompleted, got.Status, got.Error)
	obj, err := driver.Objects().Get(context.Background(), got.ResultID)
	require.NoError(t, err)
	return obj
}

func blobBytes(t *testing.T, driver storage.StorageDriver, obj *storage.KnowledgeObject) []byte {
	t.Helper()
	require.True(t, blobref.Is(obj.RawContent), "binary stays out of the row: %q", obj.RawContent)
	s, err := blob.Content(context.Background(), driver.Blobs(), obj.RawContent)
	require.NoError(t, err)
	return []byte(s)
}

// A base64 job is decoded at the worker and the bytes land in the blob
// store, byte for byte, whatever the size threshold.
func TestWorkerStoresBinaryJobInBlobStore(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	bin := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80}
	obj := runBinaryJob(t, driver, "bin-1", bin)
	require.Equal(t, bin, blobBytes(t, driver, obj))
	require.Empty(t, obj.TextContent, "binary has no text")
}

// A copy stored corrupted (invalid bytes turned to U+FFFD) is not the
// file: capturing the true bytes makes a new, intact object instead of
// reinforcing the corrupt one.
func TestWorkerBinaryDoesNotReinforceCorruptCopy(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()
	bin := []byte{0x89, 'P', 'N', 'G', 0x00, 0xff, 0xfe, 0x80}
	const source = "/tmp/x.png"
	// The corrupt stored copy's hash is what dedup would match.
	// "\x89" is itself invalid, so use the bytes a lossy JSON round trip leaves.
	corrupt := "�PNG\x00���"
	require.NoError(t, driver.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "corrupt", Type: "text", Source: source, RawContent: corrupt,
		ContentHash: storageutil.DraftContentHash(corrupt, source, nil), ReinforcementCount: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	obj := runBinaryJob(t, driver, "bin-2", bin)
	require.NotEqual(t, "corrupt", obj.ID, "reinforced the corrupt copy")
	require.Equal(t, bin, blobBytes(t, driver, obj))
}

// Two binaries that differ only in invalid bytes are two objects.
func TestWorkerBinariesDifferingInInvalidBytesDoNotMerge(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	a := runBinaryJob(t, driver, "bin-a", []byte{0x00, 0xff, 0xfe, 'x'})
	b := runBinaryJob(t, driver, "bin-b", []byte{0x00, 0xfe, 0xff, 'x'})
	require.NotEqual(t, a.ID, b.ID)
	require.Equal(t, []byte{0x00, 0xff, 0xfe, 'x'}, blobBytes(t, driver, a))
	require.Equal(t, []byte{0x00, 0xfe, 0xff, 'x'}, blobBytes(t, driver, b))
}

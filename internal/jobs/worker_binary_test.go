package jobs

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	return runBinaryJobOn(t, driver, id, bin, "text.short", "/tmp/x.png")
}

// runBinaryJobOn is runBinaryJob under a named pipeline and source.
func runBinaryJobOn(t *testing.T, driver storage.StorageDriver, id string, bin []byte, pipelineName, source string) *storage.KnowledgeObject {
	t.Helper()
	q := NewQueue(driver.Jobs())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	job := makeJob(id)
	job.PayloadEncoding = storage.PayloadBase64
	job.Payload = base64.StdEncoding.EncodeToString(bin)
	job.Source = source
	job.Pipeline = pipelineName
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

// pngLike is n bytes of binary: a PNG signature, NULs and bytes that are
// not valid UTF-8, repeated.
func pngLike(n int) []byte {
	unit := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80, 0xc3, 0x28, 0x00}
	var b []byte
	for len(b) < n {
		b = append(b, unit...)
	}
	return b[:n]
}

// textClean reports whether s is something a text column holds, unchanged:
// valid UTF-8 with no NUL and none of the U+FFFD a lossy store leaves.
func textClean(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && !strings.ContainsRune(s, utf8.RuneError)
}

func metadataClean(v any) bool {
	switch x := v.(type) {
	case string:
		return textClean(x)
	case map[string]any:
		for k, e := range x {
			if !textClean(k) || !metadataClean(e) {
				return false
			}
		}
	case []any:
		for _, e := range x {
			if !metadataClean(e) {
				return false
			}
		}
	}
	return true
}

// requireNoBinaryProjection fails when any text the object stores, apart
// from its content blob, is cut from the bytes.
func requireNoBinaryProjection(t *testing.T, obj *storage.KnowledgeObject) {
	t.Helper()
	require.True(t, textClean(obj.TextContent), "text_content holds binary: %q", obj.TextContent)
	for _, s := range obj.Sections {
		require.True(t, textClean(s.Title) && textClean(s.Content), "section holds binary: %q", s.Content)
		require.True(t, metadataClean(s.Metadata), "section metadata holds binary")
	}
	if obj.Graph != nil {
		for _, n := range obj.Graph.Nodes {
			require.True(t, textClean(n.Label) && textClean(n.Content), "graph node %s holds binary: %q", n.ID, n.Content)
		}
	}
	require.True(t, metadataClean(obj.Metadata), "metadata holds binary: %v", obj.Metadata)
	for _, sum := range obj.Summaries {
		require.True(t, textClean(sum), "summary holds binary")
	}
}

// Binary routed to a text pipeline (typedetector, sectioner, tagger,
// entity_extractor, graph_extractor ...) never reaches sections, nodes,
// text_content or metadata, and still lands byte for byte in the blob
// store. 600 bytes puts it on text.long, whose sectioner would otherwise
// copy it into a section and the root graph node.
func TestWorkerBinaryOnTextPipelineLeavesNoBinaryInProjection(t *testing.T) {
	for _, pipe := range []string{"text.long", "text.short"} {
		t.Run(pipe, func(t *testing.T) {
			driver := storageutil.NewTestDriver(t)
			bin := pngLike(600)
			obj := runBinaryJobOn(t, driver, "bin-proj", bin, pipe, "capture")
			require.Equal(t, bin, blobBytes(t, driver, obj))
			requireNoBinaryProjection(t, obj)
		})
	}
}

// Steps that need the bytes (OCR) still get them, and what they extract
// is text.
func TestWorkerBinaryReachesOCRStep(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	bin := pngLike(600)
	obj := runBinaryJobOn(t, driver, "bin-ocr", bin, "image.ocr", "/tmp/scan.png")
	requireNoBinaryProjection(t, obj)
	require.Equal(t, "image/png", obj.ContentType)
	require.Contains(t, obj.Metadata, "ocr_provider", "the ocr step did not run on the bytes")
}

// The dedup hash is over the bytes, not the blob ref: the same binary
// submitted twice is one object, reinforced.
func TestWorkerSameBinaryTwiceIsOneObject(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	bin := pngLike(600)
	a := runBinaryJobOn(t, driver, "bin-same-1", bin, "text.long", "capture")
	b := runBinaryJobOn(t, driver, "bin-same-2", bin, "text.long", "capture")
	require.Equal(t, a.ID, b.ID)
	require.Equal(t, storageutil.ContentHash(string(bin), "capture"), a.ContentHash,
		"the stored hash is not the hash of the bytes analyze computes")
}

type recordingObjects struct {
	obj     *storage.KnowledgeObject
	updates int
}

func (r *recordingObjects) GetObject(context.Context, string) (*storage.KnowledgeObject, error) {
	return r.obj, nil
}
func (r *recordingObjects) UpdateObject(context.Context, *storage.KnowledgeObject) error {
	r.updates++
	return nil
}

// Reprocess steps read text: a binary object has none to enrich, so the
// job leaves it untouched instead of tagging or extracting from bytes.
func TestReprocessLeavesBinaryObjectAlone(t *testing.T) {
	objs := &recordingObjects{obj: &storage.KnowledgeObject{ID: "bin", Type: "text", RawContent: string(pngLike(600))}}
	r := &Reprocessor{Objects: objs}
	for _, step := range ReprocessSteps {
		id, err := r.Handle(context.Background(), &storage.Job{Payload: `{"object_id":"bin","step":"` + step + `"}`})
		require.NoError(t, err)
		require.Equal(t, "bin", id)
	}
	require.Zero(t, objs.updates)
	require.Empty(t, objs.obj.Tags)
	require.Empty(t, objs.obj.Mentions)
}

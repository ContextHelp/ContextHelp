package jobs

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// textOverwrite stands for OCR: it replaces the media bytes with text,
// after which the bytes are only in the blob store.
type textOverwrite struct {
	pipeline.BaseContract
	sawBlob func(key string) bool
	keyOf   func(d *storage.KnowledgeObject) string
	t       *testing.T
}

func (s *textOverwrite) Name() string { return "text_overwrite" }
func (s *textOverwrite) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if !s.sawBlob(s.keyOf(d)) {
		s.t.Error("original not in the blob store before the first step ran")
	}
	d.RawContent = "recognized text"
	return d, nil
}

func originalPipes(t *testing.T, drv storage.StorageDriver, name string) pipeline.Registry {
	step := &textOverwrite{
		t:            t,
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{Binary: true, Requires: []string{"RawContent"}, Produces: []string{"RawContent"}}),
		keyOf: func(d *storage.KnowledgeObject) string {
			o, _ := blob.OriginalOf(d)
			return o.Key
		},
		sawBlob: func(key string) bool {
			ok, err := drv.Blobs().Exists(context.Background(), key)
			return err == nil && ok
		},
	}
	reg := pipeline.NewRegistry()
	reg.Register(name, &pipeline.Pipeline{PipelineName: name, Steps: []pipeline.PipelineStep{step}})
	return reg
}

var originalPNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80}

// The original is stored by SHA-256 before any step runs, even when a
// step replaces the object's content with text, and an artifact node
// records it.
func TestWorkerKeepsOriginalBeforeSteps(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	obj := runBinaryJobIn(t, originalPipes(t, drv, "test.original"), drv, "orig-1", originalPNG, "test.original", "/tmp/photos/cat.png")

	require.Equal(t, "recognized text", obj.RawContent)
	o, ok := blob.OriginalOf(obj)
	require.True(t, ok, "artifact node with the original")
	require.Equal(t, blob.OriginalKey(string(originalPNG)), o.Key)
	require.Equal(t, o.Key, o.SHA256)
	require.EqualValues(t, len(originalPNG), o.Size)
	require.Equal(t, "cat.png", o.FileName)
	require.Equal(t, "image/png", o.ContentType)
	require.Equal(t, o.Key, obj.Metadata[blob.MetaOriginalBlobKey])

	rc, _, err := drv.Blobs().Get(context.Background(), o.Key)
	require.NoError(t, err)
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	require.Equal(t, originalPNG, got)
}

// Text jobs have no original to keep: they take today's path.
func TestWorkerTextJobHasNoOriginal(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	obj := runBinaryJobOn(t, drv, "orig-text", []byte("plain text note"), "text.short", "cli")
	_, ok := blob.OriginalOf(obj)
	require.False(t, ok)
	items, err := drv.Blobs().List(context.Background(), "")
	require.NoError(t, err)
	require.Empty(t, items)
}

// Two captures of the same bytes share one blob; deleting one keeps it,
// deleting the last removes it with its nodes.
func TestDeleteObjectRemovesUnreferencedOriginal(t *testing.T) {
	base := storageutil.NewTestDriver(t)
	drv := storagetest.NewAgedBlobs(base, time.Now().Add(-48*time.Hour))
	reg := originalPipes(t, drv, "test.original")
	a := runBinaryJobIn(t, reg, drv, "orig-a", originalPNG, "test.original", "/a/one.png")
	b := runBinaryJobIn(t, reg, drv, "orig-b", originalPNG, "test.original", "/b/two.png")
	o, _ := blob.OriginalOf(a)

	items, err := drv.Blobs().List(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, items, 1, "identical bytes share one blob")

	orphans, err := blob.DeleteObject(context.Background(), drv, a.ID)
	require.NoError(t, err)
	require.Empty(t, orphans)
	ok, _ := drv.Blobs().Exists(context.Background(), o.Key)
	require.True(t, ok, "still referenced by the other capture")

	orphans, err = blob.DeleteObject(context.Background(), drv, b.ID)
	require.NoError(t, err)
	require.Empty(t, orphans)
	ok, _ = drv.Blobs().Exists(context.Background(), o.Key)
	require.False(t, ok, "unreferenced original is removed")
	_, err = drv.Objects().Get(context.Background(), b.ID)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

func TestOriginalFileName(t *testing.T) {
	long := strings.Repeat("é", 200) + ".png" // 404 bytes
	for _, tc := range []struct{ source, want string }{
		{"/tmp/photos/cat.png", "cat.png"},
		{`C:\Users\me\cat.png`, "cat.png"},
		{"mixed\\dir/cat.png", "cat.png"},
		{"file:///tmp/cat.png", "cat.png"},
		{"https://example.com/a/b.mp3?x=1", "b.mp3"},
		{"/tmp/ev\x1b[31mil\x00.png", "ev[31mil.png"},
		{"/tmp/a\u0085b\u009fc\x7f.png", "abc.png"},
		{"/tmp/", ""},
		{`C:\dir\`, ""},
		{"https://example.com/dir/", ""},
		{"mailto:me@example.com", ""},
		{"ambient:clipboard/x.png", ""},
		{"v1.2", ""},
		{"user@example.com", ""},
		{"com.apple.Safari", ""},
		{"/tmp/evil\u202egnp.exe", "evilgnp.exe"},
		{"/tmp/a\u200eb\u200f\u2066c\u2069.png", "abc.png"},
		{"watch:obsidian:a/b.png", "b.png"},
		{"watch:generic:b.png", "b.png"},
		{"watch:logseq:notes/pic.jpg", "pic.jpg"},
		{"watch:generic:", ""},
		{"cli", ""},
		{"stdin", ""},
		{"clipboard", ""},
		{"", ""},
		{"/tmp/..", ""},
		{"..", ""},
	} {
		require.Equal(t, tc.want, originalFileName(tc.source), tc.source)
	}
	got := originalFileName("/x/" + long)
	require.LessOrEqual(t, len(got), maxFileNameLen)
	require.True(t, utf8.ValidString(got))
}

// With a PipelineExecutor installed (e.g. pod offload router), the host
// WorkerPool still stores the binary original before the executor runs,
// stateless RunDraft still attaches artifact/0 before steps run, and
// BlobContent is repopulated on return (since it is json:"-").
func TestWorkerExecutorKeepsOriginalBeforeSteps(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	reg := originalPipes(t, drv, "test.original")
	q := NewQueue(drv.Jobs())
	pool := NewWorkerPool(q, reg, drv, 1, nil, config.JobsConfig{
		PollInterval: 20 * time.Millisecond,
		StaleTimeout: 5 * time.Minute,
		MaxRetries:   1,
		MaxHops:      5,
	})
	pool.SetExecutor(&LocalExecutor{Pipelines: reg, MaxHops: 5})

	payload, enc := Payload(string(originalPNG))
	job := &storage.Job{
		ID:              "orig-exec-1",
		Type:            "ingest:image",
		Status:          storage.JobPending,
		Payload:         payload,
		PayloadEncoding: enc,
		Pipeline:        "test.original",
		Source:          "/tmp/photos/cat.png",
		MaxRetries:      1,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	require.NoError(t, q.Enqueue(context.Background(), job))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = pool.Start(ctx)
	}()
	require.Eventually(t, func() bool {
		j, err := q.Get(context.Background(), job.ID)
		return err == nil && j != nil && j.Status == storage.JobCompleted
	}, 5*time.Second, 20*time.Millisecond)
	cancel()
	<-done

	j, err := q.Get(context.Background(), job.ID)
	require.NoError(t, err)
	obj, err := drv.Objects().Get(context.Background(), j.ResultID)
	require.NoError(t, err)
	require.Equal(t, "recognized text", obj.RawContent)
	o, ok := blob.OriginalOf(obj)
	require.True(t, ok, "artifact node with the original")
	require.Equal(t, blob.OriginalKey(string(originalPNG)), o.Key)
	require.Equal(t, "cat.png", o.FileName)
	require.Equal(t, "image/png", o.ContentType)
	require.Equal(t, o.Key, obj.Metadata[blob.MetaOriginalBlobKey])
}

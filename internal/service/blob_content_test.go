package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	psteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/stretchr/testify/require"
)

const testBlobThreshold = 64

// blobTestService is a service whose blob threshold is testBlobThreshold
// bytes, so a short test body is externalized.
func blobTestService(t *testing.T) *Service {
	t.Helper()
	svc := newTestService(t)
	svc.Cfg.Storage.Blob.Threshold = testBlobThreshold
	return svc
}

// seedBlobObject stores an object the way ingest does: the
// externalize_content step moves content above the threshold to the
// blob store and leaves a blob:// ref in RawContent; storage defaults
// the empty TextContent to that ref.
func seedBlobObject(t *testing.T, svc *Service, id, content string) {
	t.Helper()
	ctx := context.Background()
	draft := &storage.KnowledgeObject{
		ID:          id,
		Type:        "text",
		RawContent:  content,
		ContentType: "text/plain",
		Source:      "test://" + id,
		Summaries:   []string{"zephyrine summary of " + id},
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}
	out, err := psteps.NewExternalizer(svc.Store.Blobs(), testBlobThreshold).Run(ctx, draft)
	require.NoError(t, err)
	require.True(t, blob.IsBlobRef(out.RawContent), "seed: content not externalized")
	require.NoError(t, svc.Store.Objects().Create(ctx, out))

	stored, err := svc.Store.Objects().Get(ctx, id)
	require.NoError(t, err)
	require.True(t, blob.IsBlobRef(stored.RawContent), "seed: stored content is not a ref")
}

func bigBody(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		b.WriteString("lorem ipsum dolor sit amet ")
	}
	return b.String()[:n]
}

// requireNoRef fails when any content field still carries a blob:// ref.
func requireNoRef(t *testing.T, o *storage.KnowledgeObject) {
	t.Helper()
	require.False(t, blob.IsBlobRef(o.RawContent), "object %s: raw_content is a blob ref: %q", o.ID, o.RawContent)
	require.False(t, blob.IsBlobRef(o.TextContent), "object %s: text_content is a blob ref: %q", o.ID, o.TextContent)
}

// requirePreview checks a list-style result: no ref, content capped at
// the threshold and a prefix of the original, flagged truncated.
func requirePreview(t *testing.T, o *storage.KnowledgeObject, full string) {
	t.Helper()
	requireNoRef(t, o)
	require.LessOrEqual(t, len(o.RawContent), testBlobThreshold)
	require.NotEmpty(t, o.RawContent)
	require.True(t, strings.HasPrefix(full, o.RawContent), "preview is not a prefix of the content")
	require.Equal(t, true, o.Metadata[MetaContentTruncated], "preview not flagged %s", MetaContentTruncated)
}

func TestGetObjectResolvesBlobContent(t *testing.T) {
	svc := blobTestService(t)
	body := bigBody(500)
	seedBlobObject(t, svc, "obj-big", body)

	got, err := svc.GetObject(context.Background(), "obj-big")
	require.NoError(t, err)
	requireNoRef(t, got)
	require.Equal(t, body, got.RawContent)
	require.Equal(t, body, got.TextContent)
	require.NotContains(t, got.Metadata, MetaContentTruncated)
}

func TestGetObjectMissingBlobFails(t *testing.T) {
	svc := blobTestService(t)
	seedBlobObject(t, svc, "obj-lost", bigBody(500))
	stored, err := svc.Store.Objects().Get(context.Background(), "obj-lost")
	require.NoError(t, err)
	key, _ := blob.BlobKey(stored.RawContent)
	require.NoError(t, svc.Store.Blobs().Delete(context.Background(), key))

	_, err = svc.GetObject(context.Background(), "obj-lost")
	require.Error(t, err, "a lost blob must fail the read, not answer the ref")
}

func TestListReadPathsPreviewBlobContent(t *testing.T) {
	svc := blobTestService(t)
	ctx := context.Background()
	body := bigBody(500)
	seedBlobObject(t, svc, "obj-big", body)
	small := "short inline body"
	require.NoError(t, svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-small", Type: "text", RawContent: small, Summaries: []string{"zephyrine small"},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}))

	pick := func(t *testing.T, objs []*storage.KnowledgeObject) {
		t.Helper()
		var big, sm *storage.KnowledgeObject
		for _, o := range objs {
			switch o.ID {
			case "obj-big":
				big = o
			case "obj-small":
				sm = o
			}
		}
		require.NotNil(t, big, "blob-backed object missing from results")
		requirePreview(t, big, body)
		if sm != nil {
			require.Equal(t, small, sm.RawContent)
			require.NotContains(t, sm.Metadata, MetaContentTruncated)
		}
	}

	t.Run("ListObjects", func(t *testing.T) {
		objs, _, err := svc.ListObjects(ctx, storage.ObjectFilter{Limit: 10})
		require.NoError(t, err)
		pick(t, objs)
	})
	t.Run("SearchObjects", func(t *testing.T) {
		objs, _, err := svc.SearchObjects(ctx, "type==text", 10, 0)
		require.NoError(t, err)
		pick(t, objs)
	})
	t.Run("SearchObjectsNodeAware", func(t *testing.T) {
		objs, _, err := svc.SearchObjectsNodeAware(ctx, "type==text", 10, 0, nil)
		require.NoError(t, err)
		pick(t, objs)
	})
	t.Run("FindByText", func(t *testing.T) {
		objs, err := svc.FindByText(ctx, "zephyrine", 10)
		require.NoError(t, err)
		pick(t, objs)
	})
	for _, mode := range []string{FindModeFTS, FindModeHybrid} {
		t.Run("Find/"+mode, func(t *testing.T) {
			res, err := svc.Find(ctx, FindRequest{Query: "zephyrine", Mode: mode, Limit: 10}, retrieval.SemanticSource{})
			require.NoError(t, err)
			pick(t, res.Objects)
		})
	}
	t.Run("Find/hybrid explain", func(t *testing.T) {
		res, err := svc.Find(ctx, FindRequest{Query: "zephyrine", Mode: FindModeHybrid, Limit: 10, Explain: true}, retrieval.SemanticSource{})
		require.NoError(t, err)
		pick(t, res.Objects)
	})
	t.Run("HybridSearchExplain document view", func(t *testing.T) {
		results, err := svc.HybridSearchExplainFiltered(ctx, "zephyrine", storage.ObjectFilter{Limit: 10}, retrieval.SemanticSource{}, config.DefaultSearchConfig())
		require.NoError(t, err)
		require.NotEmpty(t, results)
		for _, r := range results {
			requireNoRef(t, r.Object)
			require.False(t, blob.IsBlobRef(r.DocumentView.Body), "document view body is a blob ref")
		}
	})
	t.Run("ListPendingReminders", func(t *testing.T) {
		require.NoError(t, svc.SetObjectReminder(ctx, "obj-big", time.Now().Add(time.Hour)))
		objs, err := svc.ListPendingReminders(ctx)
		require.NoError(t, err)
		pick(t, objs)
	})
}

func TestRelatedAndBacklinksPreviewBlobContent(t *testing.T) {
	svc := blobTestService(t)
	ctx := context.Background()
	body := bigBody(500)
	seedBlobObject(t, svc, "obj-big", body)
	seedBlobObject(t, svc, "obj-big2", bigBody(400))
	for _, id := range []string{"obj-big", "obj-big2"} {
		require.NoError(t, svc.Store.Edges().Create(ctx, &storage.Edge{
			ID: "e-" + id, FromType: "object", FromID: id, ToType: "entity", ToID: "ctxt://entity/person/ada", EdgeType: "mentions",
		}))
	}

	related, err := svc.RelatedObjects(ctx, "obj-big2", 1, 10)
	require.NoError(t, err)
	require.NotEmpty(t, related)
	for _, o := range related {
		requirePreview(t, o, body)
	}

	backlinks, err := svc.EntityBacklinks(ctx, "ctxt://entity/person/ada")
	require.NoError(t, err)
	require.Len(t, backlinks, 2)
	for _, o := range backlinks {
		requireNoRef(t, o)
		require.Equal(t, true, o.Metadata[MetaContentTruncated])
	}
}

// A read-modify-write through the service keeps the content
// externalized: the stored row still holds the ref, the caller gets the
// real content back.
func TestPatchObjectKeepsBlobExternalized(t *testing.T) {
	svc := blobTestService(t)
	ctx := context.Background()
	body := bigBody(500)
	seedBlobObject(t, svc, "obj-big", body)
	before, err := svc.Store.Objects().Get(ctx, "obj-big")
	require.NoError(t, err)

	title := "renamed"
	got, err := svc.PatchObject(ctx, "obj-big", ObjectPatch{Title: &title})
	require.NoError(t, err)
	require.Equal(t, body, got.RawContent)

	stored, err := svc.Store.Objects().Get(ctx, "obj-big")
	require.NoError(t, err)
	require.Equal(t, before.RawContent, stored.RawContent, "patch inlined externalized content")
	require.Equal(t, before.TextContent, stored.TextContent, "patch inlined externalized text")
	require.Equal(t, []string{"renamed"}, stored.Summaries)

	reread, err := svc.GetObject(ctx, "obj-big")
	require.NoError(t, err)
	require.Equal(t, body, reread.RawContent)
}

// Writing back a list preview must not truncate the stored content.
func TestUpdateObjectFromPreviewKeepsFullContent(t *testing.T) {
	svc := blobTestService(t)
	ctx := context.Background()
	body := bigBody(500)
	seedBlobObject(t, svc, "obj-big", body)

	objs, _, err := svc.ListObjects(ctx, storage.ObjectFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, objs, 1)
	preview := objs[0]
	requirePreview(t, preview, body)
	preview.Summaries = []string{"edited from a list"}
	require.NoError(t, svc.UpdateObject(ctx, preview))

	got, err := svc.GetObject(ctx, "obj-big")
	require.NoError(t, err)
	require.Equal(t, body, got.RawContent, "update from a preview truncated the content")
	require.Equal(t, []string{"edited from a list"}, got.Summaries)
	require.NotContains(t, got.Metadata, MetaContentTruncated)
}

// Content that grows past the threshold through an update is
// externalized like ingest does it.
func TestUpdateObjectExternalizesLargeContent(t *testing.T) {
	svc := blobTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-grow", Type: "text", RawContent: "small", Source: "test://grow",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}))
	obj, err := svc.GetObject(ctx, "obj-grow")
	require.NoError(t, err)
	body := bigBody(300)
	obj.RawContent = body
	require.NoError(t, svc.UpdateObject(ctx, obj))
	require.Equal(t, body, obj.RawContent, "UpdateObject rewrote the caller's object")

	stored, err := svc.Store.Objects().Get(ctx, "obj-grow")
	require.NoError(t, err)
	require.True(t, blob.IsBlobRef(stored.RawContent), "large update stored inline")
	got, err := svc.GetObject(ctx, "obj-grow")
	require.NoError(t, err)
	require.Equal(t, body, got.RawContent)
}

// contentRecorder records the RawContent its step sees.
type contentRecorder struct {
	pipeline.BaseContract
	seen *string
}

func (c *contentRecorder) Name() string { return "content_recorder" }
func (c *contentRecorder) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	*c.seen = d.RawContent
	return d, nil
}

// Re-analysis runs the pipeline on the content, never on its ref, and
// stores the result externalized again.
func TestReanalyzeRunsOnResolvedContent(t *testing.T) {
	svc := blobTestService(t)
	ctx := context.Background()
	body := bigBody(500)
	seedBlobObject(t, svc, "obj-big", body)
	stored, err := svc.Store.Objects().Get(ctx, "obj-big")
	require.NoError(t, err)
	stored.Pipeline = "blobtest@v1"
	require.NoError(t, svc.Store.Objects().Update(ctx, stored))

	var seen string
	svc.Pipes.Upsert("blobtest@v1", &pipeline.Pipeline{
		PipelineName: "blobtest@v1",
		Steps:        []pipeline.PipelineStep{&contentRecorder{seen: &seen}},
	})

	_, _, err = svc.ReanalyzeObject(ctx, "obj-big")
	require.NoError(t, err)
	require.Equal(t, body, seen, "pipeline ran on the blob ref")

	after, err := svc.Store.Objects().Get(ctx, "obj-big")
	require.NoError(t, err)
	require.True(t, blob.IsBlobRef(after.RawContent), "reanalyze stored large content inline")
	got, err := svc.GetObject(ctx, "obj-big")
	require.NoError(t, err)
	require.Equal(t, body, got.RawContent)
}

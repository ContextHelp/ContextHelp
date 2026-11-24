package service

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/representation/representationtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

var originalBytes = string([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80})

// seedOriginalObject stores an object the way the worker leaves a
// captured image: OCR text in RawContent, the bytes only in the blob
// store, an artifact node and the object-level key naming them.
func seedOriginalObject(t *testing.T, svc *Service, id string) string {
	t.Helper()
	return seedOriginalObjectText(t, svc, id, "recognized text")
}

// seedOriginalObjectText is seedOriginalObject with the given OCR text.
func seedOriginalObjectText(t *testing.T, svc *Service, id, text string) string {
	t.Helper()
	ctx := ownerCtx()
	o, err := blob.PutOriginal(ctx, svc.store.Blobs(), originalBytes, "image/png", "cat.png")
	require.NoError(t, err)
	obj := &storage.KnowledgeObject{
		ID: id, Type: "image", RawContent: text, ContentType: "image/png",
		Source: "/tmp/" + id + ".png", Pipeline: "blobtest@v1",
		CreatedAt: time.Now().UTC().Truncate(time.Second), UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
	blob.AttachOriginal(obj, o, originalBytes)
	require.NoError(t, svc.store.Objects().Create(ctx, obj))
	ageBlob(t, svc, o.Key)
	return o.Key
}

func requireOriginalKept(t *testing.T, svc *Service, id, key string) {
	t.Helper()
	require.True(t, blobExists(t, svc, key), "original blob was deleted")
	stored, err := svc.store.Objects().Get(ownerCtx(), id)
	require.NoError(t, err)
	o, ok := blob.OriginalOf(stored)
	require.True(t, ok, "artifact node lost")
	require.Equal(t, key, o.Key)
	require.Contains(t, blob.Keys(stored), key)
}

// Text that grows past the threshold is externalized under
// metadata.blob_key; the original must survive that write.
func TestUpdateTextOverThresholdKeepsOriginal(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObject(t, svc, "img-1")

	obj, err := svc.GetObject(ctx, "img-1")
	require.NoError(t, err)
	obj.RawContent = bigBody(500)
	obj.TextContent = ""
	require.NoError(t, svc.UpdateObject(ctx, obj))

	requireOriginalKept(t, svc, "img-1", key)
	textKey := storedBlobKey(t, svc, "img-1")
	require.NotEqual(t, key, textKey)
}

func TestGetThenPutKeepsOriginal(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObject(t, svc, "img-2")

	obj, err := svc.GetObject(ctx, "img-2")
	require.NoError(t, err)
	obj.Summaries = []string{"edited"}
	require.NoError(t, svc.UpdateObject(ctx, obj))
	requireOriginalKept(t, svc, "img-2", key)

	title := "renamed"
	_, err = svc.PatchObject(ctx, "img-2", ObjectPatch{Title: &title})
	require.NoError(t, err)
	requireOriginalKept(t, svc, "img-2", key)
}

func TestSharedOriginalDeletedOneByOne(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObject(t, svc, "img-a")
	require.Equal(t, key, seedOriginalObject(t, svc, "img-b"), "same bytes share one blob")

	require.NoError(t, svc.DeleteObject(ctx, "img-a"))
	requireOriginalKept(t, svc, "img-b", key)
	require.NoError(t, svc.DeleteObject(ctx, "img-b"))
	require.False(t, blobExists(t, svc, key), "last delete left the original behind")
}

// Re-analysis re-runs on the original's bytes and keeps the original.
func TestReanalyzeRunsOnOriginalAndKeepsIt(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObject(t, svc, "img-3")

	var seen string
	svc.Pipes.Upsert("blobtest@v1", &pipeline.Pipeline{
		PipelineName: "blobtest@v1",
		Steps:        []pipeline.PipelineStep{newBinRecorder(&seen), newTextReplacer()},
	})
	_, _, err := svc.ReanalyzeObject(ctx, "img-3")
	require.NoError(t, err)
	require.Equal(t, originalBytes, seen, "pipeline did not run on the original's bytes")
	requireOriginalKept(t, svc, "img-3", key)

	rc, _, err := svc.store.Blobs().Get(context.Background(), key)
	require.NoError(t, err)
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	require.Equal(t, originalBytes, string(got))
}

// binRecorder records the content it sees; Binary, so it runs on media.
type binRecorder struct {
	pipeline.BaseContract
	seen *string
}

func newBinRecorder(seen *string) *binRecorder {
	return &binRecorder{BaseContract: pipeline.NewBaseContract(pipeline.StepContract{Binary: true}), seen: seen}
}

func (*binRecorder) Name() string { return "bin_recorder" }
func (c *binRecorder) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	*c.seen = d.RawContent
	return d, nil
}

// textReplacer stands for OCR: media bytes in, text out.
type textReplacer struct{ pipeline.BaseContract }

func newTextReplacer() textReplacer {
	return textReplacer{pipeline.NewBaseContract(pipeline.StepContract{Binary: true})}
}

func (textReplacer) Name() string { return "text_replacer" }
func (textReplacer) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	d.RawContent = "re-recognized text"
	return d, nil
}

func registerOriginalPipeline(svc *Service, seen *string) {
	svc.Pipes.Upsert("blobtest@v1", &pipeline.Pipeline{
		PipelineName: "blobtest@v1",
		Steps:        []pipeline.PipelineStep{newBinRecorder(seen), newTextReplacer()},
	})
}

// A lost original blob must not fail re-analysis (it would end an
// upgrade run): it runs on the stored text and keeps the recorded keys.
func TestReanalyzeMissingOriginalFallsBackToStoredText(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObject(t, svc, "img-4")
	require.NoError(t, svc.store.Blobs().Delete(ctx, key))

	var seen string
	registerOriginalPipeline(svc, &seen)
	_, _, err := svc.ReanalyzeObject(ctx, "img-4")
	require.NoError(t, err)
	require.Equal(t, "recognized text", seen)

	stored, err := svc.store.Objects().Get(ctx, "img-4")
	require.NoError(t, err)
	o, ok := blob.OriginalOf(stored)
	require.True(t, ok, "artifact node lost")
	require.Equal(t, key, o.Key)
	require.Contains(t, blob.Keys(stored), key)
	require.False(t, blobExists(t, svc, key), "an empty blob was put back")
}

// An image whose OCR found no text is not "empty": the original is
// there to run on.
func TestReanalyzeEmptyOCRTextRunsOnOriginal(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObjectText(t, svc, "img-5", "")

	var seen string
	registerOriginalPipeline(svc, &seen)
	_, _, err := svc.ReanalyzeObject(ctx, "img-5")
	require.NoError(t, err)
	require.Equal(t, originalBytes, seen)
	requireOriginalKept(t, svc, "img-5", key)
}

// With neither text nor an original there is nothing to run on.
func TestReanalyzeRefusesNoTextAndNoOriginal(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObjectText(t, svc, "img-6", "")
	require.NoError(t, svc.store.Blobs().Delete(ctx, key))
	var seen string
	registerOriginalPipeline(svc, &seen)
	_, _, err := svc.ReanalyzeObject(ctx, "img-6")
	require.Error(t, err)
}

// Content the object's RawContent refers to is the original itself: a
// lost original is then a lost content blob. Either way the object is
// reported as not re-runnable, so an upgrade run skips it.
func TestReanalyzeLostOriginalAsContentIsSkippable(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObjectText(t, svc, "img-7", blobref.For(blob.OriginalKey(originalBytes)))
	require.NoError(t, svc.store.Blobs().Delete(ctx, key))

	var seen string
	registerOriginalPipeline(svc, &seen)
	_, _, err := svc.ReanalyzeObject(ctx, "img-7")
	require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
}

func TestReanalyzeEmptyContentIsSkippable(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObjectText(t, svc, "img-8", "")
	require.NoError(t, svc.store.Blobs().Delete(ctx, key))
	var seen string
	registerOriginalPipeline(svc, &seen)
	_, _, err := svc.ReanalyzeObject(ctx, "img-8")
	require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
}

// textRejectingOCR fails on anything that is not binary, as a real OCR
// engine fails on text.
type textRejectingOCR struct{}

func (textRejectingOCR) Name() string { return "strict" }
func (textRejectingOCR) Extract(_ context.Context, data []byte, _ string) (*providers.OCRResult, error) {
	if !blobref.NeedsBlob(string(data)) {
		return nil, fmt.Errorf("not an image")
	}
	return &providers.OCRResult{Text: "found", Confidence: 0.9}, nil
}

// With the original gone, the stored text is not image data: a pipeline
// with an OCR step is not run on it.
func TestReanalyzeLostOriginalWithMediaStepIsSkippable(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	key := seedOriginalObject(t, svc, "img-9")
	require.NoError(t, svc.store.Blobs().Delete(ctx, key))
	svc.Pipes.Upsert("blobtest@v1", &pipeline.Pipeline{
		PipelineName: "blobtest@v1",
		Steps:        []pipeline.PipelineStep{steps.NewOCRExtractor(steps.WithOCRProvider(textRejectingOCR{}))},
	})
	_, _, err := svc.ReanalyzeObject(ctx, "img-9")
	require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
	stored, gerr := svc.store.Objects().Get(ctx, "img-9")
	require.NoError(t, gerr)
	require.Equal(t, "blobtest@v1", stored.Pipeline)
	require.Equal(t, "recognized text", stored.RawContent, "object was rewritten")
}

// With the original gone, a pipeline whose first media step is a pdf,
// office, audio or frame extractor is skipped too: each reads the media
// itself, not the stored text.
func TestReanalyzeLostOriginalWithExtractorStepIsSkippable(t *testing.T) {
	extractors := map[string]pipeline.PipelineStep{
		"pdf_extractor":    steps.NewPDFExtractor(),
		"office_extractor": steps.NewOfficeExtractor(),
		"audio_extractor":  steps.NewAudioExtractor(),
		"frame_sampler":    steps.NewFrameSampler(),
	}
	i := 0
	for name, step := range extractors {
		t.Run(name, func(t *testing.T) {
			svc := blobTestService(t)
			ctx := ownerCtx()
			id := fmt.Sprintf("ext-%d", i)
			i++
			key := seedOriginalObject(t, svc, id)
			require.NoError(t, svc.store.Blobs().Delete(ctx, key))
			svc.Pipes.Upsert("blobtest@v1", &pipeline.Pipeline{
				PipelineName: "blobtest@v1",
				Steps:        []pipeline.PipelineStep{step},
			})
			_, _, err := svc.ReanalyzeObject(ctx, id)
			require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
			stored, gerr := svc.store.Objects().Get(ctx, id)
			require.NoError(t, gerr)
			require.Equal(t, "recognized text", stored.RawContent, "object was rewritten")
		})
	}
}

// After re-analysis the representations derive from the re-attached
// original, once each, with unique node IDs.
func TestReanalyzeRepresentationsDeriveFromOriginal(t *testing.T) {
	svc := blobTestService(t)
	ctx := ownerCtx()
	seedOriginalObject(t, svc, "img-rep")
	svc.Pipes.Upsert("blobtest@v1", &pipeline.Pipeline{
		PipelineName: "blobtest@v1",
		Steps: []pipeline.PipelineStep{
			steps.NewOCRExtractor(steps.WithOCRProvider(representationtest.OCR{})),
			steps.NewVisionAnalyzer(steps.WithVisionProvider(representationtest.Vision{})),
		},
	})
	_, _, err := svc.ReanalyzeObject(ctx, "img-rep")
	require.NoError(t, err)
	stored, err := svc.store.Objects().Get(ctx, "img-rep")
	require.NoError(t, err)
	representationtest.RequireProvenance(t, stored.ID, stored.Graph)
	first := nodeIDs(stored.Graph)
	// A second pass must not duplicate nodes or edges: same IDs, same order.
	_, _, err = svc.ReanalyzeObject(ctx, "img-rep")
	require.NoError(t, err)
	stored, err = svc.store.Objects().Get(ctx, "img-rep")
	require.NoError(t, err)
	representationtest.RequireProvenance(t, stored.ID, stored.Graph)
	require.Equal(t, first, nodeIDs(stored.Graph))
}

func nodeIDs(g *storage.ObjectGraph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		ids = append(ids, n.ID)
	}
	return ids
}

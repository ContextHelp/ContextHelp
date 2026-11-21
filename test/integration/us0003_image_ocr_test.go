package integration

// US-0003: Image OCR ingestion.
//
// Drives the real image.ocr pipeline (filereader, formatdetector,
// ocr_extractor, textcleaner, tagger, embedding) over image files. The only
// stand-in is the OCR engine (fakeOCR, injected through BuildOpts.OCR).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fakeOCR is the OCR provider boundary.
type fakeOCR struct {
	text       string
	confidence float64
	err        error
}

func (f *fakeOCR) Name() string { return "fake-ocr" }
func (f *fakeOCR) Extract(_ context.Context, _ []byte, _ string) (*providers.OCRResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &providers.OCRResult{Text: f.text, Confidence: f.confidence}, nil
}

const stubOCRText = "invoice total forty two euros"

// startOCREnv is a test env whose builtin image.ocr pipeline runs the
// given provider.
func startOCREnv(t *testing.T, ocr providers.OCRProvider) *testEnv {
	t.Helper()
	return startTestEnvWith(t, func(o *builtins.BuildOpts) { o.OCR = ocr })
}

// writeImage writes a real PNG's bytes under dir with the given file name
// (the extension drives format detection, not the bytes).
func writeImage(t *testing.T, name string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.Black)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	return path
}

func analyzeFile(t *testing.T, env *testEnv, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	jobID, err := env.svc.Analyze(ownerCtx(), service.AnalyzeRequest{
		Content: string(data), Type: "image", Source: path, Pipeline: "image.ocr",
	})
	require.NoError(t, err)
	return jobID
}

func ingestImage(t *testing.T, env *testEnv, name string) (*storage.Job, storage.KnowledgeObject) {
	t.Helper()
	path := writeImage(t, name)
	job := waitForJob(t, env.URL, analyzeFile(t, env, path), storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)
	return job, getObject(t, env.URL, job.ResultID)
}

// POST /analyze for an image file returns 202 + job_id and the job runs on
// image.ocr.
func TestUS0003_ImageFileReturnsJobID(t *testing.T) {
	env := startOCREnv(t, &fakeOCR{text: stubOCRText, confidence: 0.95})
	defer env.stop(t)

	path := writeImage(t, "scan.png")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]string{
		"content": string(data), "type": "image", "source": path, "pipeline": "image.ocr",
	})
	require.NoError(t, err)
	jobID := postForJobID(t, env.URL+"/api/v1/analyze", body)
	require.NotEmpty(t, jobID)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	assert.Equal(t, "image.ocr", job.Pipeline)
}

// The OCR provider's text lands in an "OCR Text" section of the object.
func TestUS0003_JobCompletesWithOCRText(t *testing.T) {
	env := startOCREnv(t, &fakeOCR{text: stubOCRText, confidence: 0.95})
	defer env.stop(t)

	_, obj := ingestImage(t, env, "scan.png")

	require.NotEmpty(t, obj.Sections)
	var got string
	for _, sec := range obj.Sections {
		if sec.Title == "OCR Text" {
			got = sec.Content
		}
	}
	assert.Equal(t, stubOCRText, got)
	assert.Equal(t, "image", obj.Type)
}

// Every supported image extension routes to image.ocr and completes.
func TestUS0003_SupportedFormats(t *testing.T) {
	env := startOCREnv(t, &fakeOCR{text: stubOCRText, confidence: 0.95})
	defer env.stop(t)

	for _, name := range []string{"a.png", "a.jpg", "a.jpeg", "a.webp", "a.tiff", "a.bmp", "a.gif"} {
		job, obj := ingestImage(t, env, name)
		assert.Equal(t, "image.ocr", job.Pipeline, name)
		assert.Equal(t, "image", obj.Type, name)
	}
}

// An unsupported extension fails the job with a format error rather than
// producing an object.
func TestUS0003_UnsupportedFormatRejectsGracefully(t *testing.T) {
	env := startOCREnv(t, &fakeOCR{text: stubOCRText, confidence: 0.95})
	defer env.stop(t)

	path := writeImage(t, "scan.xyz")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	jobID, err := env.svc.Analyze(ownerCtx(), service.AnalyzeRequest{
		Content: string(data), Type: "image", Source: path, Pipeline: "image.ocr",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.Contains(t, job.Error, "unsupported format")
	assert.Empty(t, job.ResultID)
}

// OCR text is searchable through the full-text index.
func TestUS0003_OCRTextSearchable(t *testing.T) {
	env := startOCREnv(t, &fakeOCR{text: stubOCRText, confidence: 0.95})
	defer env.stop(t)

	_, obj := ingestImage(t, env, "scan.png")
	rebuildFTSIntegration(t, env)

	cfg := config.SearchConfig{DefaultMode: "fts", FallbackToFTS: true,
		CandidatePool: config.CandidatePoolConfig{FTS: 20, Vector: 20}}
	results, err := env.svc.HybridSearch(ownerCtx(), "invoice", 5, retrieval.SemanticSource{}, cfg)
	require.NoError(t, err)
	var ids []string
	for _, r := range results {
		ids = append(ids, r.ID)
	}
	assert.Contains(t, ids, obj.ID, "object must be found by its OCR text")
}

// Provider name, confidence and file extension are stored in metadata.
func TestUS0003_MetadataStored(t *testing.T) {
	env := startOCREnv(t, &fakeOCR{text: stubOCRText, confidence: 0.95})
	defer env.stop(t)

	_, obj := ingestImage(t, env, "scan.png")

	assert.Equal(t, "fake-ocr", obj.Metadata["ocr_provider"])
	assert.InDelta(t, 0.95, obj.Metadata["ocr_confidence"], 1e-9)
	assert.Equal(t, ".png", obj.Metadata["file_extension"])
}

// The stored object is retrievable by id with the same content.
func TestUS0003_ObjectRetrievableViaAPI(t *testing.T) {
	env := startOCREnv(t, &fakeOCR{text: stubOCRText, confidence: 0.95})
	defer env.stop(t)

	job, obj := ingestImage(t, env, "scan.png")
	again := getObject(t, env.URL, job.ResultID)
	assert.Equal(t, obj.ID, again.ID)
	assert.Equal(t, "image.ocr", again.Pipeline)
}

// Multipart upload: unbuilt. /analyze accepts JSON only.
func TestUS0003_MultipartUploadReturns202(t *testing.T) {
	t.Skip("unbuilt: no multipart upload route; POST /api/v1/analyze accepts JSON only")
}

// Text below the confidence threshold is flagged for review.
func TestUS0003_LowConfidenceFlaggedForReview(t *testing.T) {
	env := startOCREnv(t, &fakeOCR{text: stubOCRText, confidence: 0.30})
	defer env.stop(t)

	_, obj := ingestImage(t, env, "blurry.png")

	assert.Equal(t, true, obj.Metadata["ocr_low_confidence"])
	var labels []string
	for _, tg := range obj.Tags {
		labels = append(labels, tg.Label)
	}
	assert.Contains(t, labels, "needs-review")
}

// A provider failure fails the job with the provider's error.
func TestUS0003_OCRProviderErrorFailsJob(t *testing.T) {
	env := startOCREnv(t, &fakeOCR{err: errors.New("engine crashed")})
	defer env.stop(t)

	path := writeImage(t, "scan.png")
	job := waitForJob(t, env.URL, analyzeFile(t, env, path), storage.JobFailed)
	assert.Contains(t, job.Error, "engine crashed")
}

// File-size limit: unbuilt through the pipeline. /analyze stores Content as
// the draft's raw content, so the file reader never opens Source and its
// limit is unreachable here.
func TestUS0003_FileSizeLimitEnforced(t *testing.T) {
	t.Skip("unbuilt: analyze fills RawContent, so FileReader never reads Source; its size limit is unreachable via the pipeline")
}

func TestUS0003_GIFFirstFrameOnly(t *testing.T) {
	t.Skip("unbuilt: no step extracts the first GIF frame")
}

func TestUS0003_OCRProviderConfigurable(t *testing.T) {
	t.Skip("unbuilt: provider choice is covered at factory level; no per-request provider selection")
}

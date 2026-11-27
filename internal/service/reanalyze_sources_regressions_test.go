package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// An extractor that reads the source file is refused whether or not the
// object kept an original: a different file at the path must not replace
// the stored text.
func TestReanalyzeRegressions_SourceFileIsNeverRead(t *testing.T) {
	cases := []struct {
		name, pipeline, subtype, file string
		content                       func(*testing.T) []byte
	}{
		{"office without original", "doc.office", "office", "n.docx", invDOCX},
		{"pdf without original", "doc.pdf", "pdf", "n.pdf", func(*testing.T) []byte { return invPDF() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRealEnv(t)
			src := filepath.Join(t.TempDir(), c.file)
			require.NoError(t, os.WriteFile(src, c.content(t), 0o600)) // the file now at the path
			obj := &storage.KnowledgeObject{
				ID: "sf-1", Type: "document", Subtype: c.subtype, RawContent: "STORED OLD TEXT that the first extraction saw",
				Pipeline: c.pipeline + "@v0", Source: src,
			}
			require.NoError(t, e.driver.Objects().Create(ownerCtx(), obj))
			before := e.get(t, "sf-1")

			_, _, err := e.svc.ReanalyzeObject(ownerCtx(), "sf-1")
			require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
			assert.Equal(t, before, e.get(t, "sf-1"), "the live file replaced the stored text")
		})
	}
}

// A url.generic row whose stored subtype is PDF would run pdf_extractor
// on its URL: refused, the row unchanged, the page not fetched.
func TestReanalyzeRegressions_PDFURLRowIsRefused(t *testing.T) {
	e := newRealEnv(t)
	obj := &storage.KnowledgeObject{
		ID: "pu-1", Type: "url", Subtype: "pdf", RawContent: "extracted pdf text", ContentType: "application/pdf",
		Pipeline: "url.generic@v0", Source: e.srv.URL + "/doc.pdf",
	}
	require.NoError(t, e.driver.Objects().Create(ownerCtx(), obj))
	before := e.get(t, "pu-1")

	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), "pu-1")
	require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
	assert.Equal(t, before, e.get(t, "pu-1"))
	assert.Zero(t, e.hits.Load())
}

// Every step that reads the source file is refused, whatever the pipeline.
func TestReanalyzeRegressions_EverySourceReadingStepIsRefused(t *testing.T) {
	for name := range sourceReadingSteps {
		t.Run(name, func(t *testing.T) {
			driver := storageutil.NewTestDriver(t)
			runs := new(int)
			svc := regrServiceSteps(t, driver, []pipeline.PipelineStep{
				countingStep{pipeline.NewBaseContract(pipeline.StepContract{}), name, runs},
			})
			regrSeed(t, driver, "sr-1", "stored text")
			seeded := regrGet(t, driver, "sr-1")
			seeded.Subtype = map[string]string{"pdf_extractor": "pdf", "office_extractor": "office"}[name]
			require.NoError(t, driver.Objects().Update(ownerCtx(), seeded))
			before := regrGet(t, driver, "sr-1")

			_, _, err := svc.ReanalyzeObject(ownerCtx(), "sr-1")
			require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
			assert.Zero(t, *runs)
			assert.Equal(t, before, regrGet(t, driver, "sr-1"))
		})
	}
}

// image.ocr reads the stored original, not the file at its source: with
// the file replaced the OCR still sees the stored bytes.
func TestReanalyzeRegressions_ImageOCRReadsTheStoredOriginal(t *testing.T) {
	e := newRealEnv(t)
	fx := invFixtures["image.ocr"](t, e)
	id := invIngest(t, e, "image.ocr", fx)
	require.NoError(t, os.WriteFile(fx.source, []byte("\x89PNG replaced on disk"), 0o600))

	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
	require.NoError(t, err)
	e.ocr.mu.Lock()
	defer e.ocr.mu.Unlock()
	assert.Equal(t, invPNG, e.ocr.seen, "OCR read the live file")
}

// dedupStub plays the injected dedup step under the drop policy.
type dedupStub struct {
	pipeline.BaseContract
	runs *int
}

func (dedupStub) Name() string { return "dedup" }
func (s dedupStub) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	*s.runs++
	if d.Metadata == nil {
		d.Metadata = map[string]any{}
	}
	d.Metadata["duplicate_of"] = "another-object"
	d.Metadata["duplicate_kind"] = "similar"
	d.Metadata["suppress_output"] = true
	return d, nil
}

// dedup decides at ingest, from the store as it was: in a reanalysis it
// would mark the object a duplicate of another and, under the drop policy,
// suppressed. It is not run.
func TestReanalyzeRegressions_DedupIsNotRun(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	runs, other := new(int), new(int)
	steps := append(regrPipe(other, identity, "Metadata"), dedupStub{pipeline.NewBaseContract(pipeline.StepContract{Produces: []string{"Metadata"}}), runs})
	svc := regrServiceSteps(t, driver, steps)
	regrSeed(t, driver, "dd-1", "numbat sightings")

	for _, opts := range []ReanalyzeOptions{{}, {AllowNetwork: true}} {
		_, _, err := svc.ReanalyzeObjectWith(ownerCtx(), "dd-1", opts)
		require.NoError(t, err)
		got := regrGet(t, driver, "dd-1")
		assert.Zero(t, *runs, "dedup ran")
		assert.NotContains(t, got.Metadata, "suppress_output")
		assert.NotContains(t, got.Metadata, "duplicate_of")
	}
}

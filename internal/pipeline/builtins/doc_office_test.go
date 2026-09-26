package builtins

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// surveyParagraph is the text of surveyDocx's second paragraph.
const surveyParagraph = "More animals near the salt lakes than last season."

// surveyDocx is a minimal WordprocessingML package: the parts Word needs
// to open it, with surveyParagraph split across bold and plain runs the
// way editors write mixed formatting.
var surveyDocx = docxPackage(`<w:p><w:r><w:t>Quokka survey</w:t></w:r></w:p>` +
	`<w:p><w:r><w:t xml:space="preserve">More animals near the </w:t></w:r>` +
	`<w:r><w:rPr><w:b/></w:rPr><w:t>salt lakes</w:t></w:r>` +
	`<w:r><w:t xml:space="preserve"> than last season.</w:t></w:r></w:p>`)

func docxPackage(body string) string {
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`</Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
			`<w:body>` + body + `</w:body></w:document>`},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(p.content)); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.String()
}

func coverageOpts(t *testing.T) BuildOpts {
	t.Helper()
	return BuildOpts{
		Factory:    stubProviders(),
		Models:     wiringModels(),
		Resolver:   wiringResolver(t),
		Embeddings: embeddingtest.NewMemStore(),
	}
}

// A file doc.office cannot extract stops the pipeline before anything is
// embedded, instead of indexing whatever bytes it holds.
func TestDocOfficeFailsOnUnextractableFile(t *testing.T) {
	d := Defs()["doc.office"]
	tc := coverageCase{source: "survey.docx", file: "Quokka survey report, saved as plain text.", document: "golib"}
	opts := coverageOpts(t)
	opts.Factory = stubProvidersWithDocument(tc.document)

	draft := tc.draft(t, "doc.office")
	for _, s := range buildForCoverage(t, "doc.office", d, opts) {
		out, err := s.Run(context.Background(), draft)
		if errors.Is(err, pipeline.ErrDelegate) {
			continue
		}
		if err != nil {
			if s.Name() != "office_extractor" {
				t.Fatalf("failed at %s, want office_extractor: %v", s.Name(), err)
			}
			return
		}
		draft = out
	}
	t.Fatalf("pipeline accepted an unextractable .docx; embedded %d vectors of %q", len(draft.Vectors), draft.RawContent)
}

// extractorSubtype maps each extraction step to the Subtype it acts on;
// it passes any other draft through untouched, bytes and all.
var extractorSubtype = map[string]string{
	"pdf_extractor":    "pdf",
	"office_extractor": "office",
}

// Every extension a pipeline claims is one its format detector accepts,
// and one its extraction step acts on.
func TestPipelineClaimsMatchFormatDetector(t *testing.T) {
	for name, d := range Defs() {
		if !hasStep(d, "formatdetector") {
			continue
		}
		want := ""
		for _, s := range d.Steps {
			if sub, ok := extractorSubtype[s]; ok {
				want = sub
			}
		}
		for _, ext := range d.Extensions {
			draft := &storage.KnowledgeObject{Source: filepath.Join(os.TempDir(), "claim"+ext), RawContent: "x"}
			got, err := steps.NewFormatDetector().Run(context.Background(), draft)
			if err != nil {
				t.Errorf("%s claims %s; format detector rejects it: %v", name, ext, err)
				continue
			}
			if want != "" && got.Subtype != want {
				t.Errorf("%s claims %s; detected subtype %q, its extractor acts on %q", name, ext, got.Subtype, want)
			}
		}
	}
}

// doc.office extracts .docx only; other office formats must not reach it.
func TestSelectPipelineUnextractableOfficeFormats(t *testing.T) {
	r := Registry()
	for _, ext := range []string{".doc", ".odt", ".rtf", ".epub"} {
		if got := r.SelectPipeline("/tmp/report" + ext); got == "doc.office" {
			t.Errorf("SelectPipeline(%s) = doc.office, which cannot extract it", ext)
		}
	}
}

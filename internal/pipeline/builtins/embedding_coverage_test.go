package builtins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fanOutPipelines never persist content of their own worth embedding:
// they split an export, feed or batch into items and enqueue each one
// into a content pipeline, which embeds it. They carry no embedding step.
var fanOutPipelines = map[string]bool{
	"batch.csv":                true,
	"batch.jsonl":              true,
	"batch.tsv":                true,
	"dropbox.sync":             true,
	"email.billing":            true,
	"email.general":            true,
	"email.newsletter":         true,
	"feed.sync":                true,
	"import.discord":           true,
	"import.linkedin.articles": true,
	"import.linkedin.posts":    true,
	"import.slack":             true,
	"import.twitter":           true,
}

// acquisitionSteps fetch over the network; the coverage run seeds the
// draft with what they hand over instead of dialing out.
var acquisitionSteps = map[string]bool{
	"url_fetcher": true,
	"ibr_fetcher": true,
}

const articleHTML = `<html><head><title>Numbat recovery</title></head><body>
<h2>Survey</h2><p>Rangers counted forty numbats in the Dryandra woodland this spring.</p>
<h2>Threats</h2><p>Feral cats and foxes remain the main predators of the numbat.</p>
</body></html>`

const longNote = `Field notes from the Rottnest Island quokka survey. The team walked
the northern transects at dawn and logged every animal seen within ten meters
of the track, with its approximate age and any visible injuries. Numbers were
higher than last season near the salt lakes, where the vegetation recovered
after the winter rains, and lower near the settlement, where tourist traffic
keeps the animals away from the paths during the day. Next season the survey
will add night transects to count animals that avoid the paths in daylight.`

// coverageCase is a representative input for one content pipeline, as the
// job (or the skipped acquisition step) hands it to the first step.
type coverageCase struct {
	source  string // a URL, or a file name written under t.TempDir()
	file    string // file contents when source names a file
	raw     string // RawContent seeded before the first step
	ctype   string // ContentType seeded before the first step
	fetched bool   // an acquisition step was skipped; raw is its output
}

var embeddingPipelines = map[string]coverageCase{
	"text.short": {source: "cli", raw: "call alice about the quokka survey on friday"},
	"text.long":  {source: "cli", raw: longNote},
	"doc.markdown": {source: "survey.md", file: "# Quokka survey\n\n## Method\nDawn transects along the north shore.\n\n" +
		"## Results\nMore quokkas near the salt lakes than last season.\n"},
	"doc.code":         {source: "survey.go", file: "package survey\n\n// Count returns the quokkas seen on one transect.\nfunc Count(seen []string) int { return len(seen) }\n"},
	"doc.office":       {source: "survey.docx", file: "Quokka survey report: more animals near the salt lakes than last season."},
	"doc.pdf":          {source: "survey.pdf", file: "%PDF-1.4 quokka survey"},
	"image.ocr":        {source: "whiteboard.png", file: "\x89PNG\r\n\x1a\nquokka"},
	"image.analysis":   {source: "photo.jpg", file: "\xff\xd8\xff\xe0quokka"},
	"audio.transcribe": {source: "interview.mp3", file: "ID3quokka"},
	"video.audio_only": {source: "talk.mp4", file: "\x00\x00\x00\x18ftypmp42quokka"},
	"video.full":       {source: "talk.mov", file: "\x00\x00\x00\x14ftypqtquokka"},
	"url.generic":      {source: "https://example.com/numbats", raw: articleHTML, ctype: "text/html", fetched: true},
	"url.authenticated": {
		source: "https://intranet.example.com/numbats", ctype: "text/markdown", fetched: true,
		raw: "## Survey\nRangers counted forty numbats in the Dryandra woodland this spring.\n",
	},
	"url.interactive": {
		source: "https://app.example.com/numbats", ctype: "text/markdown", fetched: true,
		raw: "## Threats\nFeral cats and foxes remain the main predators of the numbat.\n",
	},
	"url.repo":           {source: "https://gitlab.com/wildlife/numbat-tracker", raw: articleHTML, ctype: "text/html", fetched: true},
	"url.github.repo":    {source: "https://github.com/wildlife/numbat-tracker", raw: articleHTML, ctype: "text/html", fetched: true},
	"url.github.issue":   {source: "https://github.com/wildlife/numbat-tracker/issues/7", raw: articleHTML, ctype: "text/html", fetched: true},
	"url.github.pr":      {source: "https://github.com/wildlife/numbat-tracker/pull/8", raw: articleHTML, ctype: "text/html", fetched: true},
	"url.github.release": {source: "https://github.com/wildlife/numbat-tracker/releases/tag/v1.0.0", raw: articleHTML, ctype: "text/html", fetched: true},
	"url.github.profile": {source: "https://github.com/wildlife", raw: articleHTML, ctype: "text/html", fetched: true},
	"url.github.starred": {source: "https://github.com/wildlife/numbat-tracker", raw: articleHTML, ctype: "text/html", fetched: true},
}

// stubProviders answers every provider role with its deterministic stub,
// so extraction steps run for real and never touch local tools.
func stubProviders() *providers.Factory {
	stub := config.ProviderBackendConfig{Backend: "stub"}
	return providers.NewFactory(config.ProvidersConfig{
		Video: stub, Document: stub, OCR: stub, Transcription: stub,
		Vision: stub, Diarization: stub, LLM: stub,
	}, nil)
}

// buildForCoverage builds every step of d, provider steps included: the
// capability pruning of buildPipeline would drop the extraction steps a
// stub cannot vouch for, and with them the text they produce.
func buildForCoverage(t *testing.T, name string, d Def, opts BuildOpts) []pipeline.PipelineStep {
	t.Helper()
	out := make([]pipeline.PipelineStep, 0, len(d.Steps))
	for _, stepName := range d.Steps {
		if acquisitionSteps[stepName] {
			continue
		}
		s, err := resolveStep(stepName, opts)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if o, ok := d.Overrides[s.Name()]; ok {
			s = pipeline.ApplyOverride(s, o)
		}
		out = append(out, s)
	}
	return out
}

func (c coverageCase) draft(t *testing.T, name string) *storage.KnowledgeObject {
	t.Helper()
	d := &storage.KnowledgeObject{
		ID:          "cov-" + strings.ReplaceAll(name, ".", "-"),
		Pipeline:    name,
		Source:      c.source,
		RawContent:  c.raw,
		ContentType: c.ctype,
	}
	if c.file != "" {
		path := filepath.Join(t.TempDir(), c.source)
		if err := os.WriteFile(path, []byte(c.file), 0o600); err != nil {
			t.Fatal(err)
		}
		d.Source = path
	}
	if c.fetched {
		d.Metadata = map[string]any{"final_url": c.source, "content_type": c.ctype}
	}
	return d
}

// Every builtin pipeline either embeds what it ingests or fans its items
// out to one that does. Content pipelines, run end to end on a
// representative input, attach a vector for every populating model.
func TestEveryBuiltinPipelineProducesEmbeddingVectors(t *testing.T) {
	defs := Defs()
	requireRegistered(t, defs)

	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)

	opts := BuildOpts{
		Factory:    stubProviders(),
		Models:     wiringModels(),
		Resolver:   wiringResolver(t),
		Embeddings: embeddingtest.NewMemStore(),
	}
	for _, name := range names {
		d := defs[name]
		if fanOutPipelines[name] {
			if hasStep(d, "embedding") {
				t.Errorf("%s: listed as fan-out but embeds; give it a coverage case", name)
			}
			continue
		}
		tc, ok := embeddingPipelines[name]
		if !ok {
			t.Errorf("%s: no embedding coverage case; add a representative input", name)
			continue
		}
		t.Run(name, func(t *testing.T) { requireVectors(t, name, d, tc, opts) })
	}
}

// requireRegistered fails for table entries naming no registered pipeline.
func requireRegistered(t *testing.T, defs map[string]Def) {
	t.Helper()
	for name := range embeddingPipelines {
		if _, ok := defs[name]; !ok {
			t.Errorf("coverage case for unregistered pipeline %q", name)
		}
	}
	for name := range fanOutPipelines {
		if _, ok := defs[name]; !ok {
			t.Errorf("fan-out entry for unregistered pipeline %q", name)
		}
	}
}

func hasStep(d Def, step string) bool {
	for _, s := range d.Steps {
		if s == step {
			return true
		}
	}
	return false
}

// requireVectors runs d on tc's input and checks every populating model
// embedded the projected text.
func requireVectors(t *testing.T, name string, d Def, tc coverageCase, opts BuildOpts) {
	t.Helper()
	if !hasStep(d, "embedding") {
		t.Fatalf("content pipeline has no embedding step: %v", d.Steps)
	}
	draft := tc.draft(t, name)
	for _, s := range buildForCoverage(t, name, d, opts) {
		out, err := s.Run(context.Background(), draft)
		if err != nil {
			if errors.Is(err, pipeline.ErrDelegate) {
				continue
			}
			t.Fatalf("step %s: %v", s.Name(), err)
		}
		draft = out
	}

	text := projection.EmbeddingText(draft)
	if strings.TrimSpace(text) == "" {
		t.Fatal("pipeline output projects to empty embedding text")
	}
	got := modelIDs(draft.Vectors)
	if len(got) != 2 || got[0] != "snowflake-arctic-embed2@default" || got[1] != "snowflake-arctic-embed2@candidate" {
		t.Fatalf("vectors for %v, want one per populating model", got)
	}
	for _, v := range draft.Vectors {
		if len(v.Vector) != 1024 || v.Text != text {
			t.Errorf("%s: dim=%d text=%q, want 1024 dims of the projected text", v.ModelID, len(v.Vector), v.Text)
		}
	}
}

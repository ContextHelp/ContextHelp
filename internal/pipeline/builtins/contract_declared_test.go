package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// contractLLM answers each prompt kind with a reply the step can use.
type contractLLM struct{}

func (contractLLM) Name() string { return "fake" }
func (contractLLM) Generate(_ context.Context, prompt string) (string, error) {
	switch {
	case strings.Contains(prompt, "knowledge extraction assistant"):
		return `{"summary":"S.","topics":["topic"],"decisions":["Use SQLite"],"open_questions":["Why?"],"artifacts":["pkg"]}`, nil
	case strings.Contains(prompt, "Identify named entities"):
		return "@person.ada", nil
	case strings.Contains(prompt, "concise tags"):
		return "llmtagone, llmtagtwo", nil
	case strings.Contains(prompt, "one-sentence summary"):
		return "A summary.", nil
	}
	return "{}", nil
}

const contractMarkdown = "# Title\n\nIntro paragraph by @person.bob about the storage layer and its tests, long enough for the tagger.\n\n## Part one\n\nSome text with a `code` mention and more words about storage, storage and tests.\n\n```go\n// comment\nfunc Foo() {}\n```\n\n## Part two\n\nMore text, a decision to use SQLite, and a link https://example.com/x.\n"

func contractDraft() *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID: "obj1", Type: "text", RawContent: contractMarkdown, Source: "note.md",
		ContentType: "text/markdown", Metadata: map[string]any{"k": "v"},
	}
}

func contractStep(t *testing.T, name string) (pipeline.PipelineStep, bool) {
	t.Helper()
	llm := contractLLM{}
	switch name {
	case "tagger":
		return steps.NewTaggerWithLLM(llm), true
	case "sectioner":
		return steps.NewSectionerWithLLM(llm), true
	case "entity_extractor":
		return steps.NewEntityExtractorWithLLM(llm), true
	case "graph_extractor":
		return steps.NewGraphExtractorWithLLM(llm), true
	}
	st, err := resolveStep(name, BuildOpts{})
	return st, err == nil
}

// fieldSnapshot is each exported field of o as JSON, by Go field name.
func fieldSnapshot(o *storage.KnowledgeObject) map[string]string {
	out := map[string]string{}
	v := reflect.ValueOf(o).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !f.IsExported() {
			continue
		}
		b, err := json.Marshal(v.Field(i).Interface())
		if err != nil {
			b = []byte(fmt.Sprintf("%+v", v.Field(i).Interface()))
		}
		out[f.Name] = string(b)
	}
	return out
}

func declares(produces []string, field string) bool {
	return slices.ContainsFunc(produces, func(p string) bool {
		c := pipeline.Canonicalize(p)
		return c == field || strings.HasPrefix(c, field+".")
	})
}

// builtinStepNames is every built-in step name: the constructor tables and
// the steps the pipeline definitions use.
func builtinStepNames() map[string]bool {
	names := map[string]bool{}
	add := func(ks ...string) {
		for _, k := range ks {
			names[k] = true
		}
	}
	for k := range stepConstructors {
		add(k)
	}
	for k := range fetchStepConstructors {
		add(k)
	}
	for k := range githubStepConstructors {
		add(k)
	}
	for k := range browserStepConstructors {
		add(k)
	}
	for k := range blobStepConstructors {
		add(k)
	}
	for k := range embeddingStepConstructors {
		add(k)
	}
	for k := range providerStepConstructors {
		add(k)
	}
	for _, d := range Defs() {
		add(d.Steps...)
	}
	return names
}

// notExercised names the built-in steps the fixtures cannot run, each with
// the reason. Their Produces are not checked here.
var notExercised = map[string]string{
	"ocr_extractor":          "reads image bytes through an OCR provider",
	"vision_analyzer":        "reads image bytes through a vision provider",
	"pdf_extractor":          "reads a PDF file at the source path",
	"office_extractor":       "reads an office file at the source path",
	"audio_extractor":        "extracts audio from a media file with a video provider",
	"audio_transcriber":      "needs an audio file and a transcription provider",
	"speaker_diarizer":       "needs transcript segments and a diarization provider",
	"frame_sampler":          "needs a media file and a frame provider",
	"frame_ocr":              "needs sampled frame files and an OCR provider",
	"timestamp_aligner":      "needs transcript segments from audio_transcriber",
	"url_fetcher":            "fetches from the network",
	"feed_fetcher":           "fetches from the network",
	"ibr_fetcher":            "needs a browser client",
	"dropbox_fetcher":        "fetches from the network",
	"slack_parser":           "parses a Slack export, not text",
	"discord_parser":         "parses a Discord export, not text",
	"twitter_archive_parser": "parses a Twitter archive, not text",
	"jsonl_parser":           "parses JSON lines records, not text",
	"email_parser":           "parses a raw email message, not text",
	"feed_parser":            "parses a fetched RSS or Atom body",
	"github_repo_fetcher":    "calls the GitHub API",
	"github_repo_mapper":     "maps a snapshot github_repo_fetcher left on the draft",
	"externalize_content":    "needs a blob store",
	"filereader":             "reads the file at the source path",
	"item_enqueuer":          "stages items for a queue",
	"batch_enqueuer":         "stages items for a queue",
	"email_enqueuer":         "stages items for a queue",
	"dropbox_enqueuer":       "stages items for a queue",
	"item_deduplicator":      "needs the store of known items",
}

// Every built-in step declares in Produces each draft field it changes,
// graph nodes and edges included ("Graph"): a reanalysis clears what a
// step produces, keyed on its declaration. A step is either exercised
// (it runs without panic and its writes are compared) or named in
// notExercised with the reason; a step in neither fails.
func TestBuiltinStepsDeclareWhatTheyWrite(t *testing.T) {
	for name := range builtinStepNames() {
		if reason, skip := notExercised[name]; skip {
			if reason == "" {
				t.Errorf("step %s is in notExercised without a reason", name)
			}
			continue
		}
		st, ok := contractStep(t, name)
		if !ok {
			t.Errorf("step %s cannot be built here: exercise it or add it to notExercised with the reason", name)
			continue
		}
		exercised := false
		for _, draft := range []*storage.KnowledgeObject{contractDraft(), contractCodeDraft()} {
			if runAndCompare(t, name, st, draft) {
				exercised = true
			}
		}
		if !exercised {
			t.Errorf("step %s did not run on any fixture: exercise it or add it to notExercised with the reason", name)
		}
	}
	for name := range notExercised {
		if !builtinStepNames()[name] {
			t.Errorf("notExercised names %s, which is no built-in step", name)
		}
	}
}

// runAndCompare runs st on draft and reports whether it ran (no error, no
// panic), failing the test on a panic or an undeclared write.
func runAndCompare(t *testing.T, name string, st pipeline.PipelineStep, draft *storage.KnowledgeObject) (ran bool) {
	t.Helper()
	ctx, scratch := pipeline.WithScratch(context.Background())
	before := fieldSnapshot(draft)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("step %s panicked: %v", name, r)
			ran = false
		}
	}()
	out, err := st.Run(ctx, draft)
	scratch.Cleanup(out)
	if err != nil || out == nil {
		return false
	}
	after := fieldSnapshot(out)
	for field, b := range before {
		if after[field] != b && !declares(st.Contract().Produces, field) {
			t.Errorf("step %s changes %s but its Produces %v does not declare it", name, field, st.Contract().Produces)
		}
	}
	return true
}

func contractCodeDraft() *storage.KnowledgeObject {
	d := contractDraft()
	d.Source, d.RawContent, d.ContentType = "main.go", "package main\n\n// Foo does a thing.\nfunc Foo() {}\n", "text/x-go"
	return d
}

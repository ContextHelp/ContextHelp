package service

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

type invKind string

const (
	invUnchanged invKind = "unchanged" // re-derived from stored content, nothing changes
	invRefused   invKind = "refused"   // ErrCannotReanalyze, row unchanged
	invUncovered invKind = "uncovered" // not exercised here, with the reason
)

type invOutcome struct {
	kind   invKind
	reason string
}

// invTable classifies every built-in pipeline definition by name. A
// definition missing from it fails TestReanalyzeInvariant_EveryDefinition
// IsClassified, so a new pipeline cannot pass silently.
var invTable = map[string]invOutcome{
	"text.short":         {invUnchanged, ""},
	"text.long":          {invUnchanged, ""},
	"doc.markdown":       {invUnchanged, ""},
	"doc.code":           {invUnchanged, ""},
	"image.ocr":          {invUnchanged, ""},
	"url.generic":        {invUnchanged, ""},
	"url.github.issue":   {invUnchanged, ""},
	"url.github.pr":      {invUnchanged, ""},
	"url.github.profile": {invUnchanged, ""},
	"url.github.release": {invUnchanged, ""},
	"url.repo":           {invUnchanged, ""},
	// dependency_enricher no longer makes it a container: without the
	// fetch it runs on the stored text like url.generic.
	"url.github.starred": {invUnchanged, ""},

	"batch.csv":                {invRefused, "stages items"},
	"batch.jsonl":              {invRefused, "stages items"},
	"batch.tsv":                {invRefused, "stages items"},
	"dropbox.sync":             {invRefused, "stages items"},
	"email.billing":            {invRefused, "stages items"},
	"email.general":            {invRefused, "stages items"},
	"email.newsletter":         {invRefused, "stages items"},
	"feed.sync":                {invRefused, "stages items"},
	"import.discord":           {invRefused, "stages items"},
	"import.linkedin.articles": {invRefused, "stages items"},
	"import.linkedin.posts":    {invRefused, "stages items"},
	"import.slack":             {invRefused, "stages items"},
	"import.twitter":           {invRefused, "stages items"},
	// Refused on stored content only: github_repo_mapper needs the fetch.
	// It runs with AllowNetwork (see TestReanalyzeRepo_*).
	"url.github.repo":  {invRefused, "needs the fetched repository snapshot"},
	"doc.pdf":          {invRefused, "extractor reads the source file"},
	"doc.office":       {invRefused, "extractor reads the source file"},
	"audio.transcribe": {invRefused, "transcriber reads the source file"},
	"video.audio_only": {invRefused, "audio extractor reads the source file"},
	"video.full":       {invRefused, "audio extractor and frame sampler read the source file"},

	"image.analysis":    {invUncovered, "needs a vision provider with a model; its steps read the stored original like image.ocr (probed there)"},
	"url.authenticated": {invUncovered, "needs a browser client; after the skipped ibr_fetcher it runs the url.* chain"},
	"url.interactive":   {invUncovered, "needs a browser client; after the skipped ibr_fetcher it runs the url.* chain"},
}

// invLLMSteps are the definition step names that call the LLM.
var invLLMSteps = []string{"sectioner", "tagger", "entity_extractor", "graph_extractor", "structured_metadata"}

func invNames(kind invKind) []string {
	var out []string
	for n, o := range invTable {
		if o.kind == kind {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// Every built-in pipeline definition is classified, and no entry is stale.
func TestReanalyzeInvariant_EveryDefinitionIsClassified(t *testing.T) {
	defs := builtins.Defs()
	for name := range defs {
		o, ok := invTable[name]
		assert.True(t, ok, "pipeline %q is in no outcome: classify it as unchanged, refused or uncovered", name)
		if ok && o.kind == invUncovered {
			assert.NotEmpty(t, o.reason, "uncovered pipeline %q needs a reason", name)
		}
	}
	for name := range invTable {
		_, ok := defs[name]
		assert.True(t, ok, "classified pipeline %q is no longer a definition", name)
	}
}

// An uncovered pipeline is one the harness cannot build; if it can, it
// must be classified properly.
func TestReanalyzeInvariant_UncoveredAreNotRegistered(t *testing.T) {
	reg := newRealEnv(t).reg
	for _, name := range invNames(invUncovered) {
		_, err := reg.Get(name)
		assert.Error(t, err, "%s is buildable here now: move it out of uncovered", name)
	}
}

// A pipeline classified unchanged is re-derived from stored content with
// the content, user edits and every derived field as they were, and no
// network request. The LLM is called: the run is not vacuous.
func TestReanalyzeInvariant_Unchanged(t *testing.T) {
	for _, name := range invNames(invUnchanged) {
		t.Run(name, func(t *testing.T) {
			e := newRealEnv(t) // a store of its own: one content hash per profile
			fx, ok := invFixtures[name]
			require.True(t, ok, "no fixture for %s", name)
			id := invIngest(t, e, name, fx(t, e))
			before := e.get(t, id)
			hits, llmCalls := e.hits.Load(), e.llm.calls
			_, jobsBefore, err := e.driver.Jobs().List(ownerCtx(), storage.JobFilter{Limit: 1})
			require.NoError(t, err)
			if name == "url.github.starred" {
				// The enricher has something to record, so the comparison
				// below is not vacuous.
				require.EqualValues(t, 3, before.Metadata["deps_enqueued"], "%v", before.Metadata)
			}

			_, _, err = e.svc.ReanalyzeObject(ownerCtx(), id)
			require.NoError(t, err, "a pipeline classified unchanged must re-derive")
			invAssertUnchanged(t, before, e.get(t, id))
			_, jobsAfter, err := e.driver.Jobs().List(ownerCtx(), storage.JobFilter{Limit: 1})
			require.NoError(t, err)
			assert.Equal(t, jobsBefore, jobsAfter, "a reanalysis enqueued a job")
			assert.Equal(t, hits, e.hits.Load(), "a reanalysis made a network request")
			// The starred fixture is a bare manifest (what the enricher acts
			// on): it has no prose for the language model to be asked about.
			if invHasLLMStep(name) && name != "url.github.starred" {
				assert.Greater(t, e.llm.calls, llmCalls, "the LLM was never called: the comparison is vacuous")
			}
		})
	}
}

func invHasLLMStep(name string) bool {
	return slices.ContainsFunc(builtins.Defs()[name].Steps, func(s string) bool { return slices.Contains(invLLMSteps, s) })
}

// invStepNames maps the few definition step keys that differ from the
// step's own Name() (the refusal rules read Name()).
var invStepNames = map[string]string{
	"filereader": "file_reader", "formatdetector": "format_detector", "typedetector": "typedetect",
	"textcleaner": "text_cleaner", "embedding": "embedding_generator",
}

// A pipeline classified refused is refused before any step runs, with the
// row unchanged, built from the definition's own step names so it holds
// for pipelines whose providers the harness lacks.
func TestReanalyzeInvariant_Refused(t *testing.T) {
	for _, name := range invNames(invRefused) {
		t.Run(name, func(t *testing.T) {
			e := newRealEnv(t)
			runs := new(int)
			var steps []pipeline.PipelineStep
			for _, key := range builtins.Defs()[name].Steps {
				if real, ok := invStepNames[key]; ok {
					key = real
				}
				steps = append(steps, countingStep{pipeline.NewBaseContract(pipeline.StepContract{}), key, runs})
			}
			e.reg.Upsert(name, &pipeline.Pipeline{PipelineName: name, Steps: steps})
			obj := &storage.KnowledgeObject{ID: "ref-" + name, Type: "text", RawContent: invText, Pipeline: name + "@v0", Source: "/gone/file"}
			require.NoError(t, e.driver.Objects().Create(ownerCtx(), obj))
			before := e.get(t, obj.ID)

			_, _, err := e.svc.ReanalyzeObject(ownerCtx(), obj.ID)
			require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
			assert.Zero(t, *runs, "a step ran before the refusal")
			assert.Equal(t, before, e.get(t, obj.ID))
		})
	}
}

func invIngest(t *testing.T, e *realEnv, name string, fx invFixture) string {
	t.Helper()
	var obj *storage.KnowledgeObject
	if fx.binary != nil {
		// The original is stored, and the file stays where it was read.
		require.NoError(t, os.WriteFile(fx.source, fx.binary, 0o600))
		_, err := blob.PutOriginal(ownerCtx(), e.driver.Blobs(), string(fx.binary), fx.contentType, filepath.Base(fx.source))
		require.NoError(t, err)
		obj = e.ingestJob(t, name, fx.source, base64.StdEncoding.EncodeToString(fx.binary), true)
	} else {
		obj = e.ingestJob(t, name, fx.source, fx.payload, false)
	}
	// A person edits the type after ingest (`ctxt edit --type --subtype`).
	stored := e.get(t, obj.ID)
	stored.Type, stored.Subtype, stored.ContentType = "decision", "adr", "text/x-edited"
	require.NoError(t, e.driver.Objects().Update(ownerCtx(), stored))
	return obj.ID
}

func invAssertUnchanged(t *testing.T, before, after *storage.KnowledgeObject) {
	t.Helper()
	assert.Equal(t, before.RawContent, after.RawContent, "RawContent changed")
	assert.Equal(t, before.TextContent, after.TextContent, "TextContent inconsistent with RawContent")
	assert.Equal(t, "decision", after.Type, "the edited type was reverted")
	assert.Equal(t, "adr", after.Subtype, "the edited subtype was reverted")
	assert.Equal(t, "text/x-edited", after.ContentType, "the edited content type was reverted")
	assert.Equal(t, before.Metadata["content_type"], after.Metadata["content_type"], "content type metadata")
	assert.Equal(t, before.ContentHash, after.ContentHash, "content hash")
	assert.Equal(t, "my note", after.InboxNote)
	assert.Equal(t, map[string]any{"k": "v"}, after.Metadata["data"])
	assert.Equal(t, before.Metadata, after.Metadata, "metadata")
	assert.Equal(t, before.Summaries, after.Summaries, "summaries")
	assert.Equal(t, before.Decisions, after.Decisions, "decisions")
	assert.Equal(t, before.Tasks, after.Tasks, "tasks")
	assert.Equal(t, graphSet(before.Graph), graphSet(after.Graph), "graph")
	assert.Equal(t, sectionSet(before.Sections), sectionSet(after.Sections), "sections")
	assert.Equal(t, labelSet(before.Tags), labelSet(after.Tags), "tags")
	assert.Equal(t, mentionSet(before.Mentions), mentionSet(after.Mentions), "mentions")
}

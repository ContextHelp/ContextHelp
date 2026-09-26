package eval_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/eval"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// providerLabel returns a human-readable label for the active LLM provider config.
func providerLabel(cfg config.ProvidersConfig) string {
	switch cfg.LLM.Backend {
	case "openai":
		return "openai:" + cfg.LLM.Model
	case "anthropic":
		return "anthropic:" + cfg.LLM.Model
	case "ollama":
		return "ollama:" + cfg.LLM.Model
	default:
		return "stub"
	}
}

// activeLLMConfigs returns the set of LLM configs to run the eval suite against.
// A config is included only when the required env var is present.
func activeLLMConfigs() []config.ProvidersConfig {
	var cfgs []config.ProvidersConfig

	if os.Getenv("OPENAI_API_KEY") != "" {
		cfgs = append(cfgs, config.ProvidersConfig{
			LLM: config.ProviderBackendConfig{Backend: "openai", Model: "gpt-4o-mini"},
		})
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		cfgs = append(cfgs, config.ProvidersConfig{
			LLM: config.ProviderBackendConfig{Backend: "anthropic", Model: "claude-haiku-4-5-20251001"},
		})
	}
	if os.Getenv("EVAL_OLLAMA") != "" {
		endpoint := os.Getenv("OLLAMA_HOST")
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		cfgs = append(cfgs, config.ProvidersConfig{
			LLM: config.ProviderBackendConfig{
				Backend:  "ollama",
				Model:    "llama3",
				Endpoint: endpoint,
			},
		})
	}

	return cfgs
}

// TestEvalSuite runs the eval fixture suite against every configured LLM provider.
// Skips automatically when no provider API keys are set.
func TestEvalSuite(t *testing.T) {
	cfgs := activeLLMConfigs()
	if len(cfgs) == 0 {
		t.Skip("no LLM provider configured: set OPENAI_API_KEY, ANTHROPIC_API_KEY, or EVAL_OLLAMA=1")
	}

	fixtureFiles := map[string]string{
		"text.short":  filepath.Join(eval.FixturesDir(), "text_short.json"),
		"url.generic": filepath.Join(eval.FixturesDir(), "url_generic.json"),
		"image.ocr":   filepath.Join(eval.FixturesDir(), "image_ocr.json"),
	}

	for _, provCfg := range cfgs {
		provCfg := provCfg
		label := providerLabel(provCfg)
		t.Run(label, func(t *testing.T) {
			factory := providers.NewFactory(provCfg, nil)
			reg := builtins.ConfiguredRegistry(factory)

			for pipelineName, fixturePath := range fixtureFiles {
				pipelineName, fixturePath := pipelineName, fixturePath
				t.Run(pipelineName, func(t *testing.T) {
					fixtures, err := eval.LoadFixtures(fixturePath)
					require.NoError(t, err, "load fixtures from %s", fixturePath)

					for _, fx := range fixtures {
						fx := fx
						t.Run(fx.ID, func(t *testing.T) {
							if pipelineName == "text.short" {
								runTextShortFixture(t, factory, reg, fx)
							} else {
								// Non-text pipelines: validate selector only.
								runSelectorFixture(t, reg, fx)
							}
						})
					}
				})
			}
		})
	}
}

// TestEvalPipelineSelector validates pipeline selector routing without LLM keys.
func TestEvalPipelineSelector(t *testing.T) {
	reg := builtins.Registry()

	fixtureFiles := []string{
		filepath.Join(eval.FixturesDir(), "text_short.json"),
		filepath.Join(eval.FixturesDir(), "url_generic.json"),
		filepath.Join(eval.FixturesDir(), "image_ocr.json"),
	}

	for _, fixturePath := range fixtureFiles {
		fixtures, err := eval.LoadFixtures(fixturePath)
		require.NoError(t, err)

		for _, fx := range fixtures {
			if fx.Expect.PipelineSelected == "" {
				continue
			}
			fx := fx
			t.Run(fx.ID+"/selector", func(t *testing.T) {
				got := selectFixture(reg, fx)
				assert.Equal(t, fx.Expect.PipelineSelected, got,
					"fixture %s: pipeline selector mismatch", fx.ID)
			})
		}
	}
}

// TestEvalDedup validates that the content hash used for dedup is stable and correct.
// Does not require LLM keys.
func TestEvalDedup(t *testing.T) {
	const content = "GraphQL is a query language for APIs."
	const src = "cli"

	h1 := storageutil.ContentHash(content, src)
	h2 := storageutil.ContentHash(content, src)
	assert.Equal(t, h1, h2, "content hash must be deterministic")
	assert.NotEmpty(t, h1)

	h3 := storageutil.ContentHash("Different content.", src)
	assert.NotEqual(t, h1, h3, "different content must yield different hash")

	// Normalization: case + whitespace.
	h4 := storageutil.ContentHash("Hello   World\n", "")
	h5 := storageutil.ContentHash("hello world", "")
	assert.Equal(t, h4, h5, "hash must normalize case and whitespace")
}

// TestEvalTextShortStub runs the text.short tagger with stub LLM (no API key needed).
// Verifies fixture expectations against heuristic output only.
func TestEvalTextShortStub(t *testing.T) {
	ctx := context.Background()
	fixtures, err := eval.LoadFixtures(filepath.Join(eval.FixturesDir(), "text_short.json"))
	require.NoError(t, err)

	tagger := steps.NewTagger()
	extractor := steps.NewEntityExtractor()

	for _, fx := range fixtures {
		fx := fx
		t.Run(fx.ID, func(t *testing.T) {
			if fx.IsFile {
				t.Skip("file-based fixture; skipped in stub mode")
			}

			draft := &storage.KnowledgeObject{
				ID:         fmt.Sprintf("eval-%s", fx.ID),
				Type:       "text",
				RawContent: fx.Input,
			}

			out, err := tagger.Run(ctx, draft)
			require.NoError(t, err)
			out, err = extractor.Run(ctx, out)
			require.NoError(t, err)

			if fx.Expect.MinTags > 0 {
				assert.GreaterOrEqual(t, len(out.Tags), fx.Expect.MinTags,
					"expected >= %d tags", fx.Expect.MinTags)
			}
			for _, want := range fx.Expect.TagContains {
				assert.True(t, tagExists(out.Tags, want),
					"expected tag %q in %v", want, tagLabels(out.Tags))
			}
			if fx.Expect.MinMentions > 0 {
				assert.GreaterOrEqual(t, len(out.Mentions), fx.Expect.MinMentions,
					"expected >= %d mentions", fx.Expect.MinMentions)
			}
			for _, want := range fx.Expect.MentionContains {
				assert.True(t, mentionExists(out, want),
					"expected mention %q in %v", want, out.Mentions)
			}
		})
	}
}

// TestEvalAgnosticism runs the same text.short fixtures across all available
// providers and flags any regression in tag overlap between providers.
func TestEvalAgnosticism(t *testing.T) {
	cfgs := activeLLMConfigs()
	if len(cfgs) < 2 {
		t.Skip("need >= 2 LLM providers configured to run agnosticism check")
	}

	fixtures, err := eval.LoadFixtures(filepath.Join(eval.FixturesDir(), "text_short.json"))
	require.NoError(t, err)

	ctx := context.Background()

	type provResult struct {
		provider string
		tagMap   map[string][]string
	}

	var allResults []provResult
	for _, provCfg := range cfgs {
		provCfg := provCfg
		label := providerLabel(provCfg)
		factory := providers.NewFactory(provCfg, nil)
		llm := factory.LLM()
		tagger := steps.NewTaggerWithLLM(llm)

		tagMap := make(map[string][]string)
		for _, fx := range fixtures {
			if fx.IsFile || fx.Pipeline != "text.short" {
				continue
			}
			draft := &storage.KnowledgeObject{
				ID:         "eval-" + fx.ID,
				Type:       "text",
				RawContent: fx.Input,
			}
			out, err := tagger.Run(ctx, draft)
			if err != nil {
				t.Logf("[%s] fixture %s: tagger error: %v", label, fx.ID, err)
				continue
			}
			tagMap[fx.ID] = tagLabels(out.Tags)
		}
		allResults = append(allResults, provResult{provider: label, tagMap: tagMap})
	}

	if len(allResults) < 2 {
		return
	}
	base := allResults[0]
	for _, other := range allResults[1:] {
		for id, baseTags := range base.tagMap {
			otherTags := other.tagMap[id]
			overlap := jaccardTagOverlap(baseTags, otherTags)
			t.Logf("agnosticism [%s vs %s] fixture %s: jaccard=%.2f base=%v other=%v",
				base.provider, other.provider, id, overlap, baseTags, otherTags)
			if len(baseTags) > 0 && len(otherTags) > 0 {
				assert.Greater(t, overlap, 0.0,
					"provider regression: %s vs %s for fixture %s have zero tag overlap",
					base.provider, other.provider, id)
			}
		}
	}
}

// runTextShortFixture drives a single text.short fixture through the tagger and
// entity extractor with the real LLM provider, then validates expectations.
func runTextShortFixture(
	t *testing.T,
	factory *providers.Factory,
	reg pipelineSelector,
	fx eval.Fixture,
) {
	t.Helper()

	if fx.IsFile {
		t.Skip("file-based fixture; skipped (OCR requires provider setup)")
	}

	// Selector assertion.
	if fx.Expect.PipelineSelected != "" {
		got := selectFixture(reg, fx)
		assert.Equal(t, fx.Expect.PipelineSelected, got, "selector mismatch")
	}

	ctx := context.Background()
	llm := factory.LLM()
	tagger := steps.NewTaggerWithLLM(llm)
	extractor := steps.NewEntityExtractorWithLLM(llm)

	draft := &storage.KnowledgeObject{
		ID:         fmt.Sprintf("eval-%s", fx.ID),
		Type:       "text",
		RawContent: fx.Input,
	}

	out, err := tagger.Run(ctx, draft)
	require.NoError(t, err)
	out, err = extractor.Run(ctx, out)
	require.NoError(t, err)

	if fx.Expect.MinTags > 0 {
		assert.GreaterOrEqual(t, len(out.Tags), fx.Expect.MinTags,
			"fixture %s: expected >= %d tags, got %d", fx.ID, fx.Expect.MinTags, len(out.Tags))
	}
	for _, want := range fx.Expect.TagContains {
		assert.True(t, tagExists(out.Tags, want),
			"fixture %s: expected tag %q not found in %v", fx.ID, want, tagLabels(out.Tags))
	}
	if fx.Expect.MinMentions > 0 {
		assert.GreaterOrEqual(t, len(out.Mentions), fx.Expect.MinMentions,
			"fixture %s: expected >= %d mentions, got %d", fx.ID, fx.Expect.MinMentions, len(out.Mentions))
	}
	for _, want := range fx.Expect.MentionContains {
		assert.True(t, mentionExists(out, want),
			"fixture %s: expected mention %q", fx.ID, want)
	}
}

// runSelectorFixture validates only the pipeline selector for non-text fixtures.
func runSelectorFixture(t *testing.T, reg pipelineSelector, fx eval.Fixture) {
	t.Helper()
	if fx.IsFile {
		t.Skip("file-based fixture; skipped (requires OCR provider)")
	}
	if fx.Expect.PipelineSelected != "" {
		got := selectFixture(reg, fx)
		assert.Equal(t, fx.Expect.PipelineSelected, got,
			"fixture %s: pipeline selector mismatch", fx.ID)
	}
}

// pipelineSelector is the registry method the selector assertions drive.
type pipelineSelector interface {
	SelectPipeline(source, content string) string
}

// selectFixture routes a fixture the way a capture arrives: a file path or
// URL is the source, anything else is content under no source.
func selectFixture(reg pipelineSelector, fx eval.Fixture) string {
	if fx.IsFile || strings.HasPrefix(fx.Input, "http://") || strings.HasPrefix(fx.Input, "https://") {
		return reg.SelectPipeline(fx.Input, "")
	}
	return reg.SelectPipeline("", fx.Input)
}

func tagExists(tags []storage.Tag, label string) bool {
	lower := strings.ToLower(label)
	for _, tag := range tags {
		if strings.ToLower(tag.Label) == lower {
			return true
		}
	}
	return false
}

func tagLabels(tags []storage.Tag) []string {
	labels := make([]string, len(tags))
	for i, tag := range tags {
		labels[i] = tag.Label
	}
	return labels
}

// mentionExists checks whether a @namespace.slug mention appears in the object's mentions.
// It matches dot-notation slugs (e.g. "project.ctxt") against the URI ID field
// (stored as "project/ctxt") by normalizing separators.
func mentionExists(obj *storage.KnowledgeObject, slug string) bool {
	// Normalize slug: strip leading @, replace dots with slashes for URI comparison.
	normalized := strings.ReplaceAll(strings.TrimPrefix(slug, "@"), ".", "/")
	for _, m := range obj.Mentions {
		ms := m.String()
		// Match against full URI string (ctxt://entity/project/ctxt)
		// or against just the ID portion (project/ctxt).
		if strings.EqualFold(ms, slug) ||
			strings.EqualFold(ms, "@"+slug) ||
			strings.HasSuffix(strings.ToLower(ms), "/"+strings.ToLower(normalized)) ||
			strings.EqualFold(m.ID, normalized) {
			return true
		}
	}
	return false
}

// jaccardTagOverlap returns Jaccard similarity between two tag label sets.
func jaccardTagOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(a))
	for _, v := range a {
		setA[strings.ToLower(v)] = true
	}
	intersection := 0
	union := len(setA)
	for _, v := range b {
		k := strings.ToLower(v)
		if setA[k] {
			intersection++
		} else {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

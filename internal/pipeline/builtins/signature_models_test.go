package builtins

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

var errNoTool = errors.New("tool not found")

// testRegistry builds the registry for cfg with every tool lookup refused,
// so no pipeline's availability depends on what the machine has installed.
func testRegistry(t *testing.T, cfg config.ProvidersConfig, opts BuildOpts, o map[string]config.PipelineOverride) pipeline.Registry {
	t.Helper()
	ctx := providers.WithToolLookup(context.Background(), func(string) (string, error) { return "", errNoTool })
	opts.Factory = providers.NewFactory(cfg, nil).WithContext(ctx)
	reg, err := ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{Overrides: o})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// modelCfg configures a local LLM and vision model.
func modelCfg() config.ProvidersConfig {
	return config.ProvidersConfig{
		LLM:    config.ProviderBackendConfig{Backend: "ollama", Model: "m1"},
		Vision: config.ProviderBackendConfig{Backend: "ollama", Model: "v1"},
	}
}

// withOCR makes the ocr role available, so image.analysis is built.
func withOCR() BuildOpts { return BuildOpts{OCR: providers.NewStubOCRProvider()} }

// sigs maps every pipeline definition to its current signature: the
// registered pipeline's own, the baseline of one left out.
func sigs(t *testing.T, reg pipeline.Registry) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, base := range pipeline.Baselines(reg) {
		out[name] = base
		if p, err := reg.Get(name); err == nil {
			out[name] = p.Signature
		}
	}
	return out
}

// changed lists the pipelines whose signature differs between a and b.
func changed(a, b map[string]string) []string {
	var out []string
	for name, sa := range a {
		if sb, ok := b[name]; !ok || sa != sb {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// A pipeline with no step declaring the resource never has its signature
// moved by that resource's model; these three have no llm and no vision.
var noModelPipelines = []string{"batch.csv", "batch.jsonl", "import.discord"}

func TestSignature_LLMModelChangeMovesOnlyLLMPipelines(t *testing.T) {
	for name, edit := range map[string]func(*config.ProvidersConfig){
		"model":   func(c *config.ProvidersConfig) { c.LLM.Model = "m2" },
		"backend": func(c *config.ProvidersConfig) { c.LLM.Backend = "stub" },
	} {
		t.Run(name, func(t *testing.T) {
			before := sigs(t, testRegistry(t, modelCfg(), withOCR(), nil))
			cfg := modelCfg()
			edit(&cfg)
			after := sigs(t, testRegistry(t, cfg, withOCR(), nil))
			got := changed(before, after)
			for _, want := range []string{"text.short", "text.long", "doc.markdown", "image.analysis", "audio.transcribe"} {
				if !slices.Contains(got, want) {
					t.Errorf("pipeline %q has an LLM step but its signature did not change", want)
				}
			}
			for _, n := range noModelPipelines {
				if slices.Contains(got, n) {
					t.Errorf("pipeline %q has no LLM step but its signature changed", n)
				}
			}
			// Every pipeline definition, built or not, moves exactly when
			// one of its steps declares llm.
			for n, d := range Defs() {
				declares := false
				for _, s := range d.Steps {
					if z, ok := zeroStep(s); ok && slices.Contains(z.Contract().Resources, pipeline.ResourceLLM) {
						declares = true
					}
				}
				if declares != slices.Contains(got, n) {
					t.Errorf("pipeline %q: declares llm = %v, signature changed = %v", n, declares, slices.Contains(got, n))
				}
			}
		})
	}
}

func TestSignature_VisionModelChangeMovesOnlyVisionPipelines(t *testing.T) {
	before := sigs(t, testRegistry(t, modelCfg(), withOCR(), nil))
	cfg := modelCfg()
	cfg.Vision.Model = "v2"
	after := sigs(t, testRegistry(t, cfg, withOCR(), nil))
	if got := changed(before, after); !slices.Equal(got, []string{"image.analysis"}) {
		t.Errorf("a vision model change moved %v, want only image.analysis", got)
	}
}

func TestSignature_PipelineProviderOverrideMovesOnlyThatPipeline(t *testing.T) {
	before := sigs(t, testRegistry(t, modelCfg(), withOCR(), nil))
	after := sigs(t, testRegistry(t, modelCfg(), withOCR(), map[string]config.PipelineOverride{
		"text.short": {Providers: map[string]config.ProviderBackendConfig{"llm": {Model: "m9"}}},
	}))
	if got := changed(before, after); !slices.Equal(got, []string{"text.short"}) {
		t.Errorf("a per-pipeline llm override moved %v, want only text.short", got)
	}
}

// "auto" and an empty model are signed as written: nothing is resolved.
func TestSignature_ConfiguredStringsAreSignedLiterally(t *testing.T) {
	cfg := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "auto"}}
	reg := testRegistry(t, cfg, BuildOpts{}, nil)
	p, err := reg.Get("text.short")
	if err != nil {
		t.Fatal(err)
	}
	var refs []pipeline.StepRef
	for _, n := range signedNames(mustSpecs(t, "text.short")) {
		refs = append(refs, pipeline.StepRef{Name: n, Version: 1})
	}
	if want := pipeline.SignatureOf("text.short", refs, "llm=auto/"); p.Signature != want {
		t.Errorf("signature %q, want the one over the steps and llm=auto/ (%q)", p.Signature, want)
	}
}

func mustSpecs(t *testing.T, name string) []stepSpec {
	t.Helper()
	specs, err := definitionSpecs(name, Defs()[name], config.DuplicatesConfig{}, "", config.PipelineOverride{}, false)
	if err != nil {
		t.Fatal(err)
	}
	return specs
}

// Skipping every llm step removes the model entry: the model no longer
// matters to the pipeline.
func TestSignature_SkippingLLMStepsRemovesTheModelEntry(t *testing.T) {
	const name = "doc.markdown"
	skip := map[string]config.PipelineOverride{name: {SkipSteps: []string{"sectioner", "tagger"}}}
	a := sigs(t, testRegistry(t, modelCfg(), BuildOpts{}, skip))[name]
	cfg := modelCfg()
	cfg.LLM.Model = "m2"
	b := sigs(t, testRegistry(t, cfg, BuildOpts{}, skip))[name]
	if a != b {
		t.Errorf("a pipeline with no llm step moved with the llm model: %q -> %q", a, b)
	}
	plain := sigs(t, testRegistry(t, modelCfg(), BuildOpts{}, nil))[name]
	if plain == a {
		t.Fatal("precondition: skipping steps did not change the signature")
	}
}

// A pipeline left out for a missing provider is baselined like the same
// pipeline built: the same steps (policy injections and overrides applied
// the same way) and the same models. image.analysis is built when ocr is
// available and left out when it is not; the config is identical.
func TestSignature_UnavailableBaselineEqualsAvailableSignature(t *testing.T) {
	const name = "image.analysis"
	dedup := BuildOpts{Duplicates: config.DuplicatesConfig{CheckSimilar: true}}
	for label, o := range map[string]map[string]config.PipelineOverride{
		"no overrides": nil,
		"skip dedup":   {name: {SkipSteps: []string{"dedup", "textcleaner"}}},
		"insert after dedup": {name: {InsertSteps: []config.StepInsert{
			{Step: "entity_extractor", After: "dedup"},
		}}},
	} {
		t.Run(label, func(t *testing.T) {
			up := dedup
			up.OCR = providers.NewStubOCRProvider()
			available := testRegistry(t, modelCfg(), up, o)
			p, err := available.Get(name)
			if err != nil {
				t.Fatalf("precondition: %s not built: %v", name, err)
			}
			down := testRegistry(t, modelCfg(), dedup, o)
			if _, err := down.Get(name); err == nil {
				t.Fatalf("precondition: %s built without ocr", name)
			}
			if got := pipeline.Baselines(down)[name]; got != p.Signature {
				t.Errorf("unavailable baseline %q, available signature %q", got, p.Signature)
			}
		})
	}
}

// Every constructor map registers steps; each step's resources come from
// the closed vocabulary.
func TestStepResources_AreKnown(t *testing.T) {
	known := []string{pipeline.ResourceLLM, pipeline.ResourceEmbedding, pipeline.ResourceVision, pipeline.ResourceNetwork}
	var names []string
	add := func(n string) { names = append(names, n) }
	for n := range stepConstructors {
		add(n)
	}
	for n := range providerStepConstructors {
		add(n)
	}
	for n := range fetchStepConstructors {
		add(n)
	}
	for n := range githubStepConstructors {
		add(n)
	}
	for n := range browserStepConstructors {
		add(n)
	}
	for n := range embeddingStepConstructors {
		add(n)
	}
	for n := range blobStepConstructors {
		add(n)
	}
	for _, n := range names {
		step, ok := zeroStep(n)
		if !ok {
			t.Errorf("step %s is registered but zeroStep cannot build it", n)
			continue
		}
		for _, r := range step.Contract().Resources {
			if !slices.Contains(known, r) {
				t.Errorf("step %s declares unknown resource %q", n, r)
			}
		}
	}
}

// noTools is a context whose tool lookups all fail, so no test depends on
// what the machine has installed.
func noTools() context.Context {
	return providers.WithToolLookup(context.Background(), func(string) (string, error) { return "", errNoTool })
}

package builtins

import (
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// A provider that is down at boot prunes its steps; the signature covers
// the configured steps, so it must not move.
func TestSignature_UnchangedByCapabilityPruning(t *testing.T) {
	d := Def{Steps: []string{"graph_extractor", "tagger"}}
	build := func(caps pipeline.CapabilitySet) *pipeline.Pipeline {
		t.Helper()
		p, err := buildPipelineSpecs("sigtest", d, specsOf(d.Steps), BuildOpts{Factory: providers.NewFactory(config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "ollama", Model: "m1"}}, nil).WithContext(noTools())}, caps, false)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	up := build(pipeline.CapabilitySet{"llm": true})
	down := build(pipeline.CapabilitySet{})
	if len(down.Steps) >= len(up.Steps) {
		t.Fatalf("precondition: provider down kept %d steps, up has %d; nothing was pruned", len(down.Steps), len(up.Steps))
	}
	if up.Signature == "" || up.Signature != down.Signature {
		t.Errorf("signature with provider up %q, down %q; want equal and non-empty", up.Signature, down.Signature)
	}
	if want := pipeline.SignatureOf("sigtest", []pipeline.StepRef{{Name: "graph_extractor", Version: 1}, {Name: "tagger", Version: 1}}, "llm=ollama/m1"); up.Signature != want {
		t.Errorf("signature %q, want the one over the configured steps %q", up.Signature, want)
	}
	if up.BaselineSignature == "" || up.BaselineSignature != down.BaselineSignature {
		t.Errorf("baseline with provider up %q, down %q; want equal and non-empty", up.BaselineSignature, down.BaselineSignature)
	}
}

// Every step starts at version 1, so no object is stale until a step is bumped.
func TestSignature_BuiltinStepsAtVersionOne(t *testing.T) {
	reg := Registry()
	for _, name := range reg.List() {
		p, err := reg.Get(name)
		if err != nil {
			continue
		}
		if p.Signature != p.BaselineSignature {
			t.Errorf("pipeline %q: signature %q differs from baseline %q with every step at version 1", name, p.Signature, p.BaselineSignature)
		}
	}
}

func TestStepResources(t *testing.T) {
	cases := []struct {
		step pipeline.PipelineStep
		want string
	}{
		{steps.NewURLFetcher(), "network"},
		{steps.NewIBRFetcher(nil), "network"},
		{steps.NewFeedFetcher(), "network"},
		{steps.NewDropboxFetcher(), "network"},
		{steps.NewEmbeddingGenerator(nil, nil), "embedding"},
		{steps.NewTagger(), "llm"},
		{steps.NewSectioner(), "llm"},
		{steps.NewEntityExtractor(), "llm"},
		{steps.NewGraphExtractor(), "llm"},
		{steps.NewStructuredMetadataExtractor(), "llm"},
		{steps.NewVisionAnalyzer(), "vision"},
	}
	for _, c := range cases {
		if got := c.step.Contract().Resources; !slices.Equal(got, []string{c.want}) {
			t.Errorf("step %s resources = %v, want [%s]", c.step.Name(), got, c.want)
		}
	}
}

// Every pipeline definition has a baseline signature, available or not:
// the baseline stamp covers rows of a pipeline whose provider is down at
// boot.
func TestSignature_EveryPipelineDefinitionHasABaseline(t *testing.T) {
	reg := testRegistry(t, config.ProvidersConfig{}, BuildOpts{}, nil) // no tools: several pipelines are unavailable
	base := pipeline.Baselines(reg)
	unavailable := 0
	for name, d := range Defs() {
		got, ok := base[name]
		if !ok || got == "" {
			t.Errorf("pipeline %q has no baseline signature", name)
			continue
		}
		if want := pipeline.BaselineSignatureOf(name, d.Steps, modelEntries(specsOf(d.Steps), config.ProvidersConfig{})...); got != want {
			t.Errorf("pipeline %q baseline %q, want %q", name, got, want)
		}
		if _, err := reg.Get(name); err != nil {
			unavailable++
		} else if p, _ := reg.Get(name); p.Signature == "" {
			t.Errorf("available pipeline %q has no signature", name)
		}
	}
	if unavailable == 0 {
		t.Fatal("precondition: every pipeline is available; the unavailable path is not covered")
	}
}

func overrideRegistry(t *testing.T, opts BuildOpts, o map[string]config.PipelineOverride) pipeline.Registry {
	return testRegistry(t, config.ProvidersConfig{}, opts, o)
}

// Toggling a policy injection changes what runs, not which derivation a
// pipeline is: signatures stay put. A user's skip_steps does change them.
func TestSignature_IgnoresPolicyInjections(t *testing.T) {
	plain := overrideRegistry(t, BuildOpts{}, nil)
	for name, opts := range map[string]BuildOpts{
		"dedup":         {Duplicates: config.DuplicatesConfig{CheckSimilar: true}},
		"class rules":   {Access: config.AccessProtected},
		"both together": {Duplicates: config.DuplicatesConfig{CheckSimilar: true}, Access: config.AccessPublic},
	} {
		t.Run(name, func(t *testing.T) {
			injected := overrideRegistry(t, opts, nil)
			grew := false
			for _, n := range plain.List() {
				a, _ := plain.Get(n)
				b, err := injected.Get(n)
				if err != nil {
					t.Fatalf("%s: %v", n, err)
				}
				grew = grew || len(b.Steps) > len(a.Steps)
				if a.Signature != b.Signature || a.BaselineSignature != b.BaselineSignature {
					t.Errorf("pipeline %q: signature %q -> %q with the injection on", n, a.Signature, b.Signature)
				}
			}
			if !grew {
				t.Fatal("precondition: the injection added no step to any pipeline")
			}
		})
	}
}

func TestSignature_SkipStepsOverrideChangesIt(t *testing.T) {
	const name = "doc.markdown"
	if !slices.Contains(Defs()[name].Steps, "tagger") {
		t.Fatalf("precondition: %s has no tagger step", name)
	}
	plain := overrideRegistry(t, BuildOpts{}, nil)
	skipped := overrideRegistry(t, BuildOpts{}, map[string]config.PipelineOverride{name: {SkipSteps: []string{"tagger"}}})
	a, err := plain.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	b, err := skipped.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	if a.Signature == b.Signature {
		t.Errorf("skip_steps kept the signature %q", a.Signature)
	}
}

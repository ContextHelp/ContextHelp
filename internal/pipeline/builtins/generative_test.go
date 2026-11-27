package builtins

import (
	"context"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
)

type namedLLM struct{ name string }

func (n namedLLM) Name() string { return n.name }
func (namedLLM) Generate(context.Context, string) (string, error) {
	return "", nil
}

type namedVision struct{ name string }

func (n namedVision) Name() string { return n.name }
func (namedVision) Analyze(context.Context, []byte, string) (*vision.Result, error) {
	return &vision.Result{}, nil
}

func prunedNames(r pipeline.Registry, name string) []string {
	var out []string
	p, err := r.Get(name)
	if err != nil {
		return nil
	}
	for _, p := range p.Pruned {
		out = append(out, p.Name)
	}
	slices.Sort(out)
	return out
}

// A pipeline built without the llm capability records the steps it
// pruned; with the capability, none. Both registry builders.
func TestPruned_RecordedByBothBuilders(t *testing.T) {
	wantPruned := []string{"graph_extractor", "structured_metadata"}
	noLLM := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "stub"}}
	withLLM := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "ollama", Model: "m1"}}
	ctx := providers.WithToolLookup(context.Background(), func(string) (string, error) { return "", errNoTool })

	build := map[string]func(cfg config.ProvidersConfig) pipeline.Registry{
		"ConfiguredRegistryWithOpts": func(cfg config.ProvidersConfig) pipeline.Registry {
			f := providers.NewFactory(cfg, nil).WithContext(ctx)
			reg, err := ConfiguredRegistryWithOpts(BuildOpts{Factory: f})
			if err != nil {
				t.Fatal(err)
			}
			return reg
		},
		"ConfiguredRegistryWithPipelineOverrides": func(cfg config.ProvidersConfig) pipeline.Registry {
			return testRegistry(t, cfg, BuildOpts{}, nil)
		},
	}
	for name, mk := range build {
		t.Run(name, func(t *testing.T) {
			if got := prunedNames(mk(noLLM), "text.long"); !slices.Equal(got, wantPruned) {
				t.Errorf("without llm: pruned %v, want %v", got, wantPruned)
			}
			nl, _ := mk(noLLM).Get("text.long")
			for _, p := range nl.Pruned {
				if p.Generative() != pipeline.ResourceLLM {
					t.Errorf("pruned %s: generative %q, want llm", p.Name, p.Generative())
				}
			}
			if got := prunedNames(mk(withLLM), "text.long"); len(got) != 0 {
				t.Errorf("with llm: pruned %v, want none", got)
			}
		})
	}
}

// Every built-in step that declares llm, vision or embedding implements
// pipeline.ProviderStep; the llm and vision ones report the backend they
// were built with. A step that declares a provider resource and does not
// implement the interface fails here.
func TestProviderStep_EveryDeclaringStepImplementsIt(t *testing.T) {
	llm, vis := namedLLM{"llm-x"}, namedVision{"vis-x"}
	res := &providers.Resolved{LLM: llm, Vision: vis}
	built := map[string]pipeline.PipelineStep{}
	for _, names := range []func(func(string)){
		func(f func(string)) {
			for n := range stepConstructors {
				f(n)
			}
		},
		func(f func(string)) {
			for n := range embeddingStepConstructors {
				f(n)
			}
		},
		func(f func(string)) {
			for n := range fetchStepConstructors {
				f(n)
			}
		},
		func(f func(string)) {
			for n := range githubStepConstructors {
				f(n)
			}
		},
		func(f func(string)) {
			for n := range browserStepConstructors {
				f(n)
			}
		},
		func(f func(string)) {
			for n := range blobStepConstructors {
				f(n)
			}
		},
	} {
		names(func(n string) { built[n], _ = zeroStep(n) })
	}
	for name, ctor := range providerStepConstructors {
		built[name] = ctor(res)
	}

	seen := 0
	for name, st := range built {
		declares := pipeline.GenerativeResourceOf(st.Contract()) != "" || slices.Contains(st.Contract().Resources, pipeline.ResourceEmbedding)
		ps, ok := st.(pipeline.ProviderStep)
		if !declares {
			if ok {
				t.Errorf("step %s declares no provider resource but implements ProviderStep", name)
			}
			continue
		}
		seen++
		if !ok {
			t.Errorf("step %s declares a provider resource and does not implement pipeline.ProviderStep", name)
			continue
		}
		want := pipeline.GenerativeResourceOf(st.Contract())
		if want == "" {
			continue // embedding: judged by its models, see TestLocality_Embedding
		}
		uses := ps.ProviderUses(context.Background())
		wantBackend := map[string]string{pipeline.ResourceLLM: "llm-x", pipeline.ResourceVision: "vis-x"}[want]
		if len(uses) != 1 || uses[0].Resource != want || uses[0].Backend != wantBackend || !uses[0].Usable {
			t.Errorf("step %s built with %s reports %+v", name, wantBackend, uses)
		}
	}
	if seen < 7 {
		t.Errorf("only %d provider-declaring steps found, want at least 7", seen)
	}
}

// A step built with no provider, or the stub, is not usable; the stub is
// told apart.
func TestProviderStep_UnavailableProviders(t *testing.T) {
	cfg := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "stub"}}
	tagger := providerStepConstructors["tagger"](hermeticFactory(t, cfg).Resolve())
	u := tagger.(pipeline.ProviderStep).ProviderUses(context.Background())[0]
	if u.Usable || !u.Stub || u.Backend != "stub" {
		t.Errorf("stub llm: %+v, want unusable stub", u)
	}
	nilStep := providerStepConstructors["sectioner"](&providers.Resolved{})
	u = nilStep.(pipeline.ProviderStep).ProviderUses(context.Background())[0]
	if u.Usable || u.Stub {
		t.Errorf("no llm: %+v, want unusable, not stub", u)
	}
}

// A contract override wraps the step; the wrapper still reports what the
// step holds.
func TestProviderStep_SeenThroughContractOverride(t *testing.T) {
	st := providerStepConstructors["tagger"](&providers.Resolved{LLM: namedLLM{"llm-x"}})
	wrapped := pipeline.ApplyOverride(st, pipeline.StepOverride{})
	ps, ok := pipeline.AsProviderStep(wrapped)
	if !ok {
		t.Fatal("wrapped step is not a ProviderStep")
	}
	if u := ps.ProviderUses(context.Background()); u[0].Backend != "llm-x" {
		t.Errorf("backend %q, want llm-x", u[0].Backend)
	}
}

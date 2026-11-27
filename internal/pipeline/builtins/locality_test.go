package builtins

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// hermeticFactory is a factory over cfg whose tool lookups are all
// refused: building roles never runs or stats anything on the machine.
func hermeticFactory(t *testing.T, cfg config.ProvidersConfig) *providers.Factory {
	t.Helper()
	ctx := providers.WithToolLookup(context.Background(), func(string) (string, error) { return "", errNoTool })
	return providers.NewFactory(cfg, nil).WithContext(ctx)
}

func fakeKeys(t *testing.T) {
	t.Helper()
	for _, k := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"} {
		t.Setenv(k, "test-key")
	}
}

func classify(p *pipeline.Pipeline) pipeline.LocalityReport {
	return pipeline.ClassifyPipeline(context.Background(), p, steps.IsBuiltin)
}

func localityOf(t *testing.T, reg pipeline.Registry, name string) pipeline.LocalityReport {
	t.Helper()
	rep, err := pipeline.ProviderLocality(context.Background(), reg, name, steps.IsBuiltin)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func visionPipeline(t *testing.T, backend, endpoint string) *pipeline.Pipeline {
	t.Helper()
	f := hermeticFactory(t, config.ProvidersConfig{Vision: config.ProviderBackendConfig{Backend: backend, Model: "v", Endpoint: endpoint}})
	return &pipeline.Pipeline{Steps: []pipeline.PipelineStep{providerStepConstructors["vision_analyzer"](f.Resolve())}}
}

// The locality of a pipeline follows the endpoint of a local backend, not
// its name: a remote Ollama is hosted. Both llm and vision.
func TestLocality_FollowsTheEndpoint(t *testing.T) {
	clearProxyEnv(t)
	cases := []struct {
		name, backend, endpoint string
		want                    pipeline.Locality
	}{
		{"default endpoint", "ollama", "", pipeline.LocalityLocal},
		{"auto default", "auto", "", pipeline.LocalityLocal},
		{"localhost", "ollama", "http://localhost:11434", pipeline.LocalityLocal},
		{"127.0.0.1", "ollama", "http://127.0.0.1:11434", pipeline.LocalityLocal},
		{"ipv6 loopback", "ollama", "http://[::1]:11434", pipeline.LocalityLocal},
		{"private", "ollama", "http://192.168.1.20:11434", pipeline.LocalityLocal},
		{"hostname", "ollama", "https://gpu.example.net:11434", pipeline.LocalityHosted},
		{"docker name", "ollama", "http://ollama:11434", pipeline.LocalityHosted},
		{"auto with remote endpoint", "auto", "https://gpu.example.net:11434", pipeline.LocalityHosted},
		{"tailnet CGNAT", "ollama", "http://100.64.0.7:11434", pipeline.LocalityHosted},
		{"not a url", "ollama", "gpu.example.net", pipeline.LocalityHosted},
	}
	for _, c := range cases {
		t.Run("llm/"+c.name, func(t *testing.T) {
			cfg := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: c.backend, Model: "m", Endpoint: c.endpoint}}
			rep := localityOf(t, testRegistry(t, cfg, BuildOpts{}, nil), "text.long")
			if rep.Locality != c.want {
				t.Errorf("locality %q, want %q (%+v)", rep.Locality, c.want, rep.Uses)
			}
		})
		t.Run("vision/"+c.name, func(t *testing.T) {
			if c.backend == "auto" {
				t.Skip("auto vision probes the local endpoint")
			}
			if rep := classify(visionPipeline(t, c.backend, c.endpoint)); rep.Locality != c.want {
				t.Errorf("locality %q, want %q (%+v)", rep.Locality, c.want, rep.Uses)
			}
		})
	}
}

func TestLocality_HostedBackends(t *testing.T) {
	clearProxyEnv(t)
	fakeKeys(t)
	for _, b := range []string{"openai", "anthropic"} {
		cfg := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: b, Model: "m"}}
		if rep := localityOf(t, testRegistry(t, cfg, BuildOpts{}, nil), "text.long"); rep.Locality != pipeline.LocalityHosted {
			t.Errorf("llm %s: %q (%+v)", b, rep.Locality, rep.Uses)
		}
	}
	for _, b := range []string{"gemini", "openrouter", "openai", "anthropic"} {
		if rep := classify(visionPipeline(t, b, "")); rep.Locality != pipeline.LocalityHosted {
			t.Errorf("vision %s: %q (%+v)", b, rep.Locality, rep.Uses)
		}
	}
}

// A provider that forwards Name but not Endpoint cannot be judged: an
// empty endpoint is hosted, not local.
type endpointlessOllama struct{ namedLLM }

func TestLocality_EmptyEndpointIsNotLocal(t *testing.T) {
	clearProxyEnv(t)
	st := providerStepConstructors["tagger"](&providers.Resolved{LLM: endpointlessOllama{namedLLM{"ollama"}}})
	rep := classify(&pipeline.Pipeline{Steps: []pipeline.PipelineStep{st}})
	if rep.Locality.Automatic() {
		t.Errorf("an ollama-named provider with no endpoint: %q, want not automatic", rep.Locality)
	}
}

// A private endpoint that the environment sends through a proxy is hosted;
// loopback never goes through one.
func TestLocality_PrivateEndpointBehindProxyIsHosted(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTP_PROXY", "http://proxy.example.net:3128")
	t.Setenv("http_proxy", "http://proxy.example.net:3128")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	if rep := classify(visionPipeline(t, "ollama", "http://192.168.1.20:11434")); rep.Locality != pipeline.LocalityHosted {
		t.Errorf("private behind proxy: %q, want hosted", rep.Locality)
	}
	if rep := classify(visionPipeline(t, "ollama", "http://127.0.0.1:11434")); rep.Locality != pipeline.LocalityLocal {
		t.Errorf("loopback behind proxy: %q, want local", rep.Locality)
	}
	t.Setenv("NO_PROXY", "192.168.1.20")
	t.Setenv("no_proxy", "192.168.1.20")
	if rep := classify(visionPipeline(t, "ollama", "http://192.168.1.20:11434")); rep.Locality != pipeline.LocalityLocal {
		t.Errorf("private excluded by NO_PROXY: %q, want local", rep.Locality)
	}
}

func embeddingPipeline(t *testing.T, models ...string) *pipeline.Pipeline {
	t.Helper()
	var ms embeddingtest.Models
	for i, cfgJSON := range models {
		id := "m" + string(rune('a'+i)) + "@1"
		ms = append(ms, registry.Model{ModelID: id, Provider: "ollama", Dimension: 3, IsDefault: i == 0, ConfigJSON: cfgJSON})
	}
	resolver := embeddings.NewProviderResolver(&embeddings.Resolver{LookupEnv: func(string) (string, bool) { return "", false }})
	return &pipeline.Pipeline{Steps: []pipeline.PipelineStep{steps.NewEmbeddingGenerator(ms, resolver)}}
}

// The embedding step counts, with the same endpoint rule.
func TestLocality_Embedding(t *testing.T) {
	clearProxyEnv(t)
	for endpoint, want := range map[string]pipeline.Locality{
		"http://127.0.0.1:11434":        pipeline.LocalityLocal,
		"https://embed.example.net:443": pipeline.LocalityHosted,
	} {
		rep := classify(embeddingPipeline(t, `{"backend":"ollama","model":"m","endpoint":"`+endpoint+`"}`))
		if rep.Locality != want {
			t.Errorf("embedding at %s: %q, want %q (%+v)", endpoint, rep.Locality, want, rep.Uses)
		}
		if len(rep.Uses) != 1 || rep.Uses[0].Resource != pipeline.ResourceEmbedding || rep.Uses[0].Endpoint != endpoint {
			t.Errorf("embedding uses %+v", rep.Uses)
		}
	}
}

// The embedding step skips a model it cannot build a provider for; so
// does the classification: that model is listed as unusable and does not
// make the pipeline unavailable. With no usable model the step embeds
// nothing and the pipeline is judged by its other steps (none here).
func TestLocality_EmbeddingSkipsUnusableModels(t *testing.T) {
	clearProxyEnv(t)
	good := `{"backend":"ollama","model":"m","endpoint":"http://127.0.0.1:11434"}`
	bad := `{"backend":"nosuch","model":"m"}`
	rep := classify(embeddingPipeline(t, good, bad))
	if rep.Locality != pipeline.LocalityLocal {
		t.Errorf("one stale model: %q, want local (%+v)", rep.Locality, rep.Uses)
	}
	unusable := 0
	for _, u := range rep.Uses {
		if !u.Usable {
			unusable++
		}
	}
	if unusable != 1 {
		t.Errorf("uses %+v: want the stale model listed unusable", rep.Uses)
	}
	if rep := classify(embeddingPipeline(t, bad)); rep.Locality != pipeline.LocalityNone || len(rep.Uses) != 1 {
		t.Errorf("no usable model: %q %+v, want none with the model listed", rep.Locality, rep.Uses)
	}
}

// A per-pipeline provider override changes only that pipeline's answer;
// a pipeline with no provider step is none; an unknown pipeline is an
// error.
func TestLocality_PerPipelineOverrideAndNone(t *testing.T) {
	clearProxyEnv(t)
	fakeKeys(t)
	cfg := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "ollama", Model: "m"}}
	o := map[string]config.PipelineOverride{
		"text.long": {Providers: map[string]config.ProviderBackendConfig{"llm": {Backend: "openai", Model: "m"}}},
	}
	reg := testRegistry(t, cfg, BuildOpts{}, o)
	if rep := localityOf(t, reg, "text.long"); rep.Locality != pipeline.LocalityHosted {
		t.Errorf("text.long: %q, want hosted (%v)", rep.Locality, rep.Uses)
	}
	if rep := localityOf(t, reg, "text.short"); rep.Locality != pipeline.LocalityLocal {
		t.Errorf("text.short: %q, want local (%v)", rep.Locality, rep.Uses)
	}
	o["text.short"] = config.PipelineOverride{SkipSteps: []string{"tagger", "entity_extractor", "entity_resolver", "structured_metadata", "c12n_classify"}}
	reg = testRegistry(t, cfg, BuildOpts{}, o)
	if rep := localityOf(t, reg, "text.short"); rep.Locality != pipeline.LocalityNone || len(rep.Uses) != 0 {
		t.Errorf("text.short without provider steps: %q %v, want none", rep.Locality, rep.Uses)
	}
	if _, err := pipeline.ProviderLocality(context.Background(), reg, "no.such.pipeline", steps.IsBuiltin); err == nil {
		t.Error("unknown pipeline: no error")
	}
}

// A registered-but-unavailable pipeline is unavailable with its reason,
// not an error.
func TestLocality_LeftOutPipelineIsUnavailable(t *testing.T) {
	clearProxyEnv(t)
	reg := testRegistry(t, config.ProvidersConfig{}, BuildOpts{}, nil)
	if _, err := reg.Get("image.analysis"); err == nil {
		t.Fatal("fixture: image.analysis is built")
	}
	for _, name := range []string{"image.analysis", "image.analysis@v0"} {
		rep := localityOf(t, reg, name)
		if rep.Locality != pipeline.LocalityUnavailable || rep.Reason == "" || rep.Locality.Automatic() {
			t.Errorf("%s: %+v, want unavailable with a reason", name, rep)
		}
	}
}

// A stub, a missing provider and a pruned step each make the pipeline
// unavailable, never local.
func TestLocality_Unavailable(t *testing.T) {
	clearProxyEnv(t)
	stubTagger := providerStepConstructors["tagger"](hermeticFactory(t, config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "stub"}}).Resolve())
	if rep := classify(&pipeline.Pipeline{Steps: []pipeline.PipelineStep{stubTagger}}); rep.Locality != pipeline.LocalityUnavailable || rep.Locality.Automatic() {
		t.Errorf("stub-holding tagger alone: %q (%+v)", rep.Locality, rep.Uses)
	}
	nilP := &pipeline.Pipeline{Steps: []pipeline.PipelineStep{steps.NewTagger()}}
	if rep := classify(nilP); rep.Locality != pipeline.LocalityUnavailable {
		t.Errorf("nil llm: %q", rep.Locality)
	}
	pruned := &pipeline.Pipeline{Pruned: []pipeline.PrunedStep{{Name: "graph_extractor", Resources: []string{pipeline.ResourceLLM}}}}
	if rep := classify(pruned); rep.Locality != pipeline.LocalityUnavailable || !rep.Uses[0].Pruned {
		t.Errorf("pruned: %q %+v", rep.Locality, rep.Uses)
	}
}

// A step that is not a built-in is unknown, with or without the marker.
type fakeStep struct {
	pipeline.BaseContract
	name string
}

func (f fakeStep) Name() string { return f.name }
func (fakeStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return d, nil
}

func TestLocality_NonBuiltinStepIsUnknown(t *testing.T) {
	clearProxyEnv(t)
	st := fakeStep{BaseContract: pipeline.NewBaseContract(pipeline.StepContract{}), name: "mystery"}
	rep := classify(&pipeline.Pipeline{Steps: []pipeline.PipelineStep{st}})
	if rep.Locality != pipeline.LocalityUnknown || rep.Locality.Automatic() {
		t.Errorf("a step that is not a built-in: %q, want unknown", rep.Locality)
	}
	// even under a built-in name's wrapper-free twin: a name is not enough
	// without the predicate
	if rep := pipeline.ClassifyPipeline(context.Background(), &pipeline.Pipeline{Steps: []pipeline.PipelineStep{st}}, nil); rep.Locality != pipeline.LocalityUnknown {
		t.Errorf("no predicate: %q, want unknown", rep.Locality)
	}
}

// Every step of every registered built-in pipeline is a built-in by the
// predicate, so none comes out unknown for that reason.
func TestLocality_EveryBuiltinStepIsKnown(t *testing.T) {
	clearProxyEnv(t)
	cfg := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "ollama", Model: "m"}}
	reg := testRegistry(t, cfg, withOCR(), nil)
	for _, name := range reg.List() {
		p, _ := reg.Get(name)
		for _, st := range p.Steps {
			if !steps.IsBuiltin(st) {
				t.Errorf("pipeline %s: step %s is not known as a built-in", name, st.Name())
			}
		}
	}
}

func TestLocality_Automatic(t *testing.T) {
	for l, want := range map[pipeline.Locality]bool{
		pipeline.LocalityNone: true, pipeline.LocalityLocal: true,
		pipeline.LocalityHosted: false, pipeline.LocalityUnknown: false, pipeline.LocalityUnavailable: false,
	} {
		if l.Automatic() != want {
			t.Errorf("%s.Automatic() = %v", l, !want)
		}
	}
}

// clearProxyEnv makes a locality test independent of the developer's proxy
// settings; a test about proxies sets its own after calling it.
func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy"} {
		t.Setenv(k, "")
	}
}

// A type that embeds a built-in step and runs something else is not a
// built-in: it is unknown whatever the embedded step reports.
type wrappedTagger struct{ *steps.Tagger }

func (w wrappedTagger) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return d, nil
}

func TestLocality_EmbeddingWrapperOfABuiltinIsUnknown(t *testing.T) {
	clearProxyEnv(t)
	inner := providerStepConstructors["tagger"](&providers.Resolved{LLM: namedLLM{"ollama"}}).(*steps.Tagger)
	st := wrappedTagger{inner}
	if steps.IsBuiltin(st) {
		t.Fatal("fixture: the wrapper is a built-in")
	}
	if rep := classify(&pipeline.Pipeline{Steps: []pipeline.PipelineStep{st}}); rep.Locality != pipeline.LocalityUnknown || rep.Locality.Automatic() {
		t.Errorf("wrapper of a built-in: %q, want unknown", rep.Locality)
	}
}

// Credentials in an endpoint never leave the classifier; the endpoint is
// still judged by its real host.
func TestLocality_EndpointCredentialsAreRedacted(t *testing.T) {
	clearProxyEnv(t)
	rep := classify(visionPipeline(t, "ollama", "http://user:secret@192.168.1.5:11434"))
	// kit refuses an endpoint with userinfo, so it is judged hosted: fail closed
	if rep.Locality != pipeline.LocalityHosted {
		t.Errorf("locality %q, want hosted", rep.Locality)
	}
	if len(rep.Uses) == 0 {
		t.Fatal("no uses")
	}
	for _, u := range rep.Uses {
		if strings.Contains(u.Endpoint, "secret") || strings.Contains(u.Endpoint, "user") {
			t.Errorf("endpoint %q leaks its credentials", u.Endpoint)
		}
	}
}

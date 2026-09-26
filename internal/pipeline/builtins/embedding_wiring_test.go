package builtins

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Ollama calls replay cassettes recorded against a real Ollama serving
// snowflake-arctic-embed2 (1024 dimensions). Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run Embedding ./internal/pipeline/builtins/
const embeddingCassettes = "testdata/cassettes/embedding-ollama"

func wiringModels() embeddingtest.Models {
	cfg := `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://127.0.0.1:11434"}`
	return embeddingtest.Models{
		{ModelID: "snowflake-arctic-embed2@default", Provider: "ollama", Dimension: 1024, IsDefault: true, ConfigJSON: cfg},
		{ModelID: "snowflake-arctic-embed2@candidate", Provider: "ollama", Dimension: 1024, ConfigJSON: cfg},
	}
}

func wiringResolver(t *testing.T) embeddings.ProviderResolver {
	t.Helper()
	client, _ := providertest.OllamaClient(t, embeddingCassettes)
	return embeddings.NewProviderResolver(&embeddings.Resolver{
		LookupEnv:  func(string) (string, bool) { return "", false },
		HTTPClient: client,
	})
}

func runSteps(t *testing.T, p *pipeline.Pipeline, draft *storage.KnowledgeObject) *storage.KnowledgeObject {
	t.Helper()
	for _, s := range p.Steps {
		out, err := s.Run(context.Background(), draft)
		if err != nil {
			if errors.Is(err, pipeline.ErrDelegate) {
				continue
			}
			t.Fatalf("step %s: %v", s.Name(), err)
		}
		draft = out
	}
	return draft
}

func modelIDs(vs []storage.ObjectVector) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.ModelID)
	}
	return out
}

// A registry built with the embedding dependencies runs a real pipeline
// whose embedding step writes one vector per populating model.
func TestConfiguredRegistryEmbeddingStepEmbedsPerModel(t *testing.T) {
	reg := ConfiguredRegistryWithOpts(BuildOpts{
		Models:     wiringModels(),
		Resolver:   wiringResolver(t),
		Embeddings: embeddingtest.NewMemStore(),
	})
	p, err := reg.Get("text.long")
	if err != nil {
		t.Fatal(err)
	}
	draft := runSteps(t, p, &storage.KnowledgeObject{RawContent: "numbat sightings near the dryandra woodland"})

	got := modelIDs(draft.Vectors)
	if len(got) != 2 || got[0] != "snowflake-arctic-embed2@default" || got[1] != "snowflake-arctic-embed2@candidate" {
		t.Fatalf("vectors for %v, want both populating models", got)
	}
}

// The per-pipeline override path carries the embedding dependencies too,
// and a pipeline's providers.embedding override is ignored with a warning.
func TestPipelineOverridesKeepRegistryEmbeddingProvider(t *testing.T) {
	var logs bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })

	base := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "stub"}}
	reg := ConfiguredRegistryWithPipelineOverrides(BuildOpts{
		Factory:  providers.NewFactory(base, nil),
		Models:   wiringModels(),
		Resolver: wiringResolver(t),
	}, base, config.PipelinesConfig{Overrides: map[string]config.PipelineOverride{
		"text.long": {Providers: map[string]config.ProviderBackendConfig{
			"embedding": {Backend: "stub"},
		}},
	}})
	p, err := reg.Get("text.long")
	if err != nil {
		t.Fatal(err)
	}
	draft := runSteps(t, p, &storage.KnowledgeObject{RawContent: "numbat sightings near the dryandra woodland"})
	if len(draft.Vectors) != 2 {
		t.Fatalf("vectors for %v, want both registry models despite the pipeline override", modelIDs(draft.Vectors))
	}
	if !strings.Contains(logs.String(), `provider role "embedding" cannot be overridden per pipeline`) {
		t.Errorf("override not reported as ignored; logs:\n%s", logs.String())
	}
}

// Without the dependencies the embedding step is a no-op, never a stub
// provider writing empty vectors.
func TestRegistryWithoutEmbeddingDependenciesWritesNoVectors(t *testing.T) {
	p, err := Registry().Get("text.long")
	if err != nil {
		t.Fatal(err)
	}
	draft := runSteps(t, p, &storage.KnowledgeObject{RawContent: "numbat sightings near the dryandra woodland"})
	if len(draft.Vectors) != 0 {
		t.Errorf("vectors=%v, want none without a registry", modelIDs(draft.Vectors))
	}
}

// dedup composes after embedding on the Vectors contract.
func TestInjectedDedupComposesAfterEmbedding(t *testing.T) {
	opts := BuildOpts{Models: embeddingtest.Models{registry.Model{ModelID: "m@1", Dimension: 3, IsDefault: true}}, Embeddings: embeddingtest.NewMemStore()}
	names := InjectDedupStep(Defs()["text.short"].Steps, config.DuplicatesConfig{CheckSimilar: true})
	if _, err := buildPipeline("text.short", Def{Steps: names}, opts, false); err != nil {
		t.Fatalf("dedup after embedding does not compose: %v", err)
	}
	s, err := resolveStep("dedup", opts)
	if err != nil {
		t.Fatal(err)
	}
	if req := s.Contract().Requires; len(req) != 1 || req[0] != "Vectors" {
		t.Errorf("dedup requires %v, want [Vectors]", req)
	}
}

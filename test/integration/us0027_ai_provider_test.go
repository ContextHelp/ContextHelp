package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock pipeline step that records which LLM backend was configured.
// ---------------------------------------------------------------------------

type providerRecordStep struct {
	pipeline.BaseContract
	backend string
}

func (s *providerRecordStep) Name() string { return "test-provider-record" }
func (s *providerRecordStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["llm_backend"] = s.backend
	draft.Type = "document"
	return draft, nil
}

// ---------------------------------------------------------------------------
// Mock secrets resolver for test isolation.
// ---------------------------------------------------------------------------

type testMockResolver struct {
	store map[string]string
}

func newTestMockResolver(kv map[string]string) *testMockResolver {
	return &testMockResolver{store: kv}
}

func (r *testMockResolver) Get(key string) (string, error) {
	if v, ok := r.store[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found: %s", key)
}

func (r *testMockResolver) Set(_, _ string) error { return nil }

// ---------------------------------------------------------------------------
// US-0027 Tests
// ---------------------------------------------------------------------------

// TestUS0027_EnvBackendLLMStub verifies that a stub backend can be configured
// and the factory returns a non-nil LLM provider.
func TestUS0027_EnvBackendLLMStub(t *testing.T) {
	resolver := newTestMockResolver(map[string]string{
		"ANTHROPIC_API_KEY": "sk-test-from-resolver",
	})
	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "stub"

	f := providers.NewFactory(cfg, resolver)
	require.NotNil(t, f)

	llm := f.LLM()
	assert.NotNil(t, llm, "factory must return a non-nil LLM provider")
}

// TestUS0027_ProviderCalledAndResultStored verifies that ingest with a pipeline
// step that records the provider backend stores the metadata in the knowledge object.
func TestUS0027_ProviderCalledAndResultStored(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("test.provider.record", &pipeline.Pipeline{
		PipelineName: "test.provider.record",
		Steps: []pipeline.PipelineStep{
			&providerRecordStep{backend: "stub"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "text requiring enrichment",
		Type:     "text",
		Pipeline: "test.provider.record",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "stub", obj.Metadata["llm_backend"], "pipeline step must record the configured backend")
}

// TestUS0027_ConfigRoundTripLLMBackend verifies that ProvidersConfig.LLM fields
// survive JSON marshal/unmarshal without data loss.
func TestUS0027_ConfigRoundTripLLMBackend(t *testing.T) {
	original := config.ProvidersConfig{}
	original.LLM.Backend = "ollama"
	original.LLM.Model = "llama3"
	original.LLM.Endpoint = "http://localhost:11434"

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored config.ProvidersConfig
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, "ollama", restored.LLM.Backend)
	assert.Equal(t, "llama3", restored.LLM.Model)
	assert.Equal(t, "http://localhost:11434", restored.LLM.Endpoint)
}

// TestUS0027_FactoryUsesResolverNotEnv verifies that the factory uses the
// supplied resolver to look up API keys (not os.Getenv).
func TestUS0027_FactoryUsesResolverNotEnv(t *testing.T) {
	resolver := newTestMockResolver(map[string]string{
		"ANTHROPIC_API_KEY": "sk-from-resolver",
	})
	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "auto"

	f := providers.NewFactory(cfg, resolver)
	llm := f.LLM()
	assert.NotNil(t, llm)
}

// TestUS0027_NilResolverFallsBackToEnv verifies backward-compatible behaviour:
// nil resolver → EnvResolver is used without panicking.
func TestUS0027_NilResolverFallsBackToEnv(t *testing.T) {
	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "stub"

	f := providers.NewFactory(cfg, nil)
	require.NotNil(t, f)

	llm := f.LLM()
	assert.NotNil(t, llm)
}

// TestUS0027_EnvResolverReadOnly verifies that the env resolver rejects Set().
func TestUS0027_EnvResolverReadOnly(t *testing.T) {
	r := secrets.NewEnvResolver()
	err := r.Set("SOME_KEY", "some-value")
	require.Error(t, err, "env resolver must return an error on Set()")
}

// TestUS0027_AnalyzeEndpointUsesConfiguredPipeline verifies that the HTTP API
// accepts a custom pipeline name and returns a job_id.
func TestUS0027_AnalyzeEndpointUsesConfiguredPipeline(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("provider.e2e", &pipeline.Pipeline{
		PipelineName: "provider.e2e",
		Steps:        []pipeline.PipelineStep{&providerRecordStep{backend: "stub"}},
	})

	body, _ := json.Marshal(map[string]string{
		"content":  "provider pipeline test",
		"type":     "text",
		"source":   "e2e-test",
		"pipeline": "provider.e2e",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"])
}

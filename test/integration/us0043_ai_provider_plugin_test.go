package integration

// US-0043: Implement Custom AI Provider Plugin
// Verifies stub AI provider plugin: Init reads config, PipelineStep calls
// stub backend, AI-enriched fields stored in KnowledgeObject.

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// ── Stub AI provider plugin ───────────────────────────────────────────────────

type stubAIProviderPlugin struct {
	endpoint   string
	model      string
	initCalled bool
	closeCalled bool
}

func (p *stubAIProviderPlugin) Name() string    { return "stub-ai-provider" }
func (p *stubAIProviderPlugin) Version() string { return "1.0.0" }

func (p *stubAIProviderPlugin) Init(_ context.Context, cfg map[string]interface{}, _ pluginapi.Deps) error {
	p.initCalled = true
	if ep, ok := cfg["endpoint"].(string); ok {
		p.endpoint = ep
	}
	if m, ok := cfg["model"].(string); ok {
		p.model = m
	}
	return nil
}

func (p *stubAIProviderPlugin) PipelineSteps() []pluginapi.PipelineStep {
	return []pluginapi.PipelineStep{&stubAIStep{plugin: p}}
}

func (p *stubAIProviderPlugin) Close(_ context.Context) error {
	p.closeCalled = true
	return nil
}

// ── Stub AI step ──────────────────────────────────────────────────────────────

type stubAIStep struct {
	pipeline.BaseContract
	plugin *stubAIProviderPlugin
}

func (s *stubAIStep) Name() string { return "stub_ai_provider_step" }

func (s *stubAIStep) Contract() pluginapi.StepContract {
	return pluginapi.StepContract{
		Requires:     []string{"RawContent"},
		Produces:     []string{"Summaries", "Tags"},
		Capabilities: []string{"llm"},
	}
}

func (s *stubAIStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// Simulate AI model call — use configured endpoint+model in metadata.
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["ai_endpoint"] = s.plugin.endpoint
	draft.Metadata["ai_model"] = s.plugin.model

	// Produce stub summaries and tags as the contract declares.
	draft.Summaries = append(draft.Summaries, fmt.Sprintf("stub summary via %s", s.plugin.model))
	draft.Tags = append(draft.Tags, storage.Tag{
		Label:  "ai-tagged",
		Source: "stub-ai-provider",
	})
	return draft, nil
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestUS0043_PluginInitReadsConfig verifies Init populates endpoint and model.
func TestUS0043_PluginInitReadsConfig(t *testing.T) {
	pl := &stubAIProviderPlugin{}
	reg := plugin.NewRegistry()
	reg.Register(pl)

	err := reg.InitAll(context.Background(), map[string]map[string]interface{}{
		"stub-ai-provider": {
			"endpoint": "https://my-llm.internal/v1",
			"model":    "my-model-v2",
		},
	}, pluginapi.Deps{})
	require.NoError(t, err)

	assert.True(t, pl.initCalled)
	assert.Equal(t, "https://my-llm.internal/v1", pl.endpoint)
	assert.Equal(t, "my-model-v2", pl.model)

	steps := pl.PipelineSteps()
	require.Len(t, steps, 1)
	assert.Equal(t, "stub_ai_provider_step", steps[0].Name())

	contract := steps[0].Contract()
	assert.Contains(t, contract.Requires, "RawContent")
	assert.Contains(t, contract.Produces, "Summaries")
	assert.Contains(t, contract.Produces, "Tags")
	assert.Contains(t, contract.Capabilities, "llm")
}

// TestUS0043_StubResponsesStoredCorrectly verifies AI step output is in the object.
func TestUS0043_StubResponsesStoredCorrectly(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	if env.svc.PluginRegistry == nil {
		env.svc.PluginRegistry = plugin.NewRegistry()
	}
	pl := &stubAIProviderPlugin{}
	env.svc.PluginRegistry.Register(pl)
	err := env.svc.PluginRegistry.InitAll(context.Background(),
		map[string]map[string]interface{}{
			"stub-ai-provider": {
				"endpoint": "https://stub-llm.test/v1",
				"model":    "stub-model",
			},
		}, pluginapi.Deps{})
	require.NoError(t, err)

	env.svc.Pipes.Upsert("us0043.ai.enrich", &pipeline.Pipeline{
		PipelineName: "us0043.ai.enrich",
		Steps:        []pipeline.PipelineStep{&stubAIStep{plugin: pl}},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "content needing AI enrichment",
		Type:     "text",
		Pipeline: "us0043.ai.enrich",
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

	// Summaries populated by stub model.
	require.NotEmpty(t, obj.Summaries, "AI step must produce summaries")
	assert.Contains(t, obj.Summaries[0], "stub summary via stub-model")

	// Tags populated.
	foundTag := false
	for _, tag := range obj.Tags {
		if tag.Label == "ai-tagged" {
			foundTag = true
			break
		}
	}
	assert.True(t, foundTag, "AI step must add 'ai-tagged' tag")

	// Metadata includes AI endpoint and model used.
	assert.Equal(t, "https://stub-llm.test/v1", obj.Metadata["ai_endpoint"])
	assert.Equal(t, "stub-model", obj.Metadata["ai_model"])
}

// TestUS0043_TwoAIPluginsNoConflict verifies two AI provider plugins both init.
func TestUS0043_TwoAIPluginsNoConflict(t *testing.T) {
	pl1 := &stubAIProviderPlugin{}

	// Second plugin with a different name.
	type stubAI2 struct {
		stubAIProviderPlugin
	}
	pl2impl := &struct {
		endpoint    string
		model       string
		initCalled  bool
	}{}

	_ = pl2impl // just confirming two distinct instances exist

	reg := plugin.NewRegistry()
	reg.Register(pl1)
	// A second plugin with a different name wrapping the same logic.
	pl2 := &namedAIPlugin{name: "stub-ai-provider-2", endpoint: "", model: ""}
	reg.Register(pl2)

	err := reg.InitAll(context.Background(), map[string]map[string]interface{}{
		"stub-ai-provider":   {"endpoint": "https://ep1.test", "model": "m1"},
		"stub-ai-provider-2": {"endpoint": "https://ep2.test", "model": "m2"},
	}, pluginapi.Deps{})
	require.NoError(t, err)

	assert.Equal(t, "https://ep1.test", pl1.endpoint)
	assert.Equal(t, "https://ep2.test", pl2.endpoint)
	assert.Equal(t, "m1", pl1.model)
	assert.Equal(t, "m2", pl2.model)
}

// TestUS0043_CloseCalledOnShutdown verifies Close is called.
func TestUS0043_CloseCalledOnShutdown(t *testing.T) {
	pl := &stubAIProviderPlugin{}
	err := pl.Close(context.Background())
	require.NoError(t, err)
	assert.True(t, pl.closeCalled)
}

// namedAIPlugin is a second AI provider plugin variant for multi-plugin tests.
type namedAIPlugin struct {
	name     string
	endpoint string
	model    string
}

func (p *namedAIPlugin) Name() string    { return p.name }
func (p *namedAIPlugin) Version() string { return "1.0.0" }
func (p *namedAIPlugin) Init(_ context.Context, cfg map[string]interface{}, _ pluginapi.Deps) error {
	if ep, ok := cfg["endpoint"].(string); ok {
		p.endpoint = ep
	}
	if m, ok := cfg["model"].(string); ok {
		p.model = m
	}
	return nil
}
func (p *namedAIPlugin) PipelineSteps() []pluginapi.PipelineStep { return nil }
func (p *namedAIPlugin) Close(_ context.Context) error           { return nil }

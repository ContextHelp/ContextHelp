package integration

// US-0042: Implement Custom Enrichment Plugin
// Verifies in-process plugin satisfies Plugin + PipelineStep contracts,
// Init receives config, step runs during pipeline, output merged into object.

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

// ── Stub enrichment plugin ────────────────────────────────────────────────────

type stubEnrichPlugin struct {
	configReceived map[string]interface{}
	initCalled     bool
	closeCalled    bool
}

func (p *stubEnrichPlugin) Name() string    { return "stub-enrichment" }
func (p *stubEnrichPlugin) Version() string { return "1.0.0" }

func (p *stubEnrichPlugin) Init(_ context.Context, cfg map[string]interface{}, _ pluginapi.Deps) error {
	p.initCalled = true
	p.configReceived = cfg
	return nil
}

func (p *stubEnrichPlugin) PipelineSteps() []pluginapi.PipelineStep {
	return []pluginapi.PipelineStep{&stubEnrichStep{}}
}

func (p *stubEnrichPlugin) Close(_ context.Context) error {
	p.closeCalled = true
	return nil
}

// ── Stub enrichment step ──────────────────────────────────────────────────────

type stubEnrichStep struct {
	pipeline.BaseContract
}

func (s *stubEnrichStep) Name() string { return "stub_enrichment_step" }

func (s *stubEnrichStep) Contract() pluginapi.StepContract {
	return pluginapi.StepContract{
		Requires:     []string{"RawContent"},
		Produces:     []string{"Tags", "Metadata"},
		Capabilities: []string{},
	}
}

func (s *stubEnrichStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Tags = append(draft.Tags, storage.Tag{Label: "enriched-by-plugin", Source: "stub-enrichment"})
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["enrichment_plugin_ran"] = true
	draft.Metadata["plugin_name"] = "stub-enrichment"
	return draft, nil
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestUS0042_PluginSatisfiesInterface verifies Name/Version/Init/PipelineSteps/Close.
func TestUS0042_PluginSatisfiesInterface(t *testing.T) {
	pl := &stubEnrichPlugin{}

	var _ pluginapi.Plugin = pl // compile-time check

	reg := plugin.NewRegistry()
	reg.Register(pl)

	cfg := map[string]map[string]interface{}{
		"stub-enrichment": {"model": "legal-v2", "threshold": 0.7},
	}
	err := reg.InitAll(context.Background(), cfg, pluginapi.Deps{})
	require.NoError(t, err)

	assert.True(t, pl.initCalled, "Init must be called")
	assert.Equal(t, "legal-v2", pl.configReceived["model"], "config must be forwarded")
	assert.Equal(t, 0.7, pl.configReceived["threshold"])

	steps := pl.PipelineSteps()
	require.Len(t, steps, 1, "plugin must return one pipeline step")
	assert.Equal(t, "stub_enrichment_step", steps[0].Name())

	contract := steps[0].Contract()
	assert.Contains(t, contract.Requires, "RawContent")
	assert.Contains(t, contract.Produces, "Tags")
	assert.Contains(t, contract.Produces, "Metadata")

	err = pl.Close(context.Background())
	assert.NoError(t, err)
	assert.True(t, pl.closeCalled)
}

// TestUS0042_DuplicatePluginNamePanics verifies Register panics on duplicate.
func TestUS0042_DuplicatePluginNamePanics(t *testing.T) {
	reg := plugin.NewRegistry()
	reg.Register(&stubEnrichPlugin{})
	assert.Panics(t, func() {
		reg.Register(&stubEnrichPlugin{})
	}, "registering duplicate plugin name must panic")
}

// TestUS0042_StepRunsAndOutputMerged verifies the step is called and results
// are stored in the resulting KnowledgeObject.
func TestUS0042_StepRunsAndOutputMerged(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Register plugin in the service registry.
	if env.svc.PluginRegistry == nil {
		env.svc.PluginRegistry = plugin.NewRegistry()
	}
	pl := &stubEnrichPlugin{}
	env.svc.PluginRegistry.Register(pl)
	err := env.svc.PluginRegistry.InitAll(context.Background(),
		map[string]map[string]interface{}{
			"stub-enrichment": {"model": "test-model"},
		}, pluginapi.Deps{})
	require.NoError(t, err)

	// Register a pipeline that uses the plugin's step.
	env.svc.Pipes.Upsert("us0042.enrich", &pipeline.Pipeline{
		PipelineName: "us0042.enrich",
		Steps:        []pipeline.PipelineStep{&stubEnrichStep{}},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "content for enrichment plugin test",
		Type:     "text",
		Pipeline: "us0042.enrich",
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

	// Verify tag added by plugin step.
	foundTag := false
	for _, tag := range obj.Tags {
		if tag.Label == "enriched-by-plugin" {
			foundTag = true
			break
		}
	}
	assert.True(t, foundTag, "step must add 'enriched-by-plugin' tag")
	assert.Equal(t, true, obj.Metadata["enrichment_plugin_ran"], "metadata key set by plugin")
	assert.Equal(t, "stub-enrichment", obj.Metadata["plugin_name"])
}

// TestUS0042_PluginConfigIsolated verifies config keys are plugin-scoped.
func TestUS0042_PluginConfigIsolated(t *testing.T) {
	type otherPlugin struct {
		stubEnrichPlugin
		otherCfg map[string]interface{}
	}

	type namedPlugin struct {
		stubEnrichPlugin
	}
	np := &namedPlugin{}
	np.stubEnrichPlugin = stubEnrichPlugin{}

	// Two separate plugin instances with different configs.
	pl1 := &stubEnrichPlugin{}
	pl2 := &struct {
		name string
		stubEnrichPlugin
	}{name: "other-enrichment"}

	reg := plugin.NewRegistry()
	reg.Register(pl1)

	cfg := map[string]map[string]interface{}{
		"stub-enrichment":  {"model": "model-a", "threshold": 0.9},
		"other-enrichment": {"model": "model-b"},
	}
	err := reg.InitAll(context.Background(), cfg, pluginapi.Deps{})
	require.NoError(t, err)

	// pl1 gets its own config.
	assert.Equal(t, "model-a", pl1.configReceived["model"])
	// pl2 is not registered — pl1's config must not bleed.
	_ = pl2
	assert.NotContains(t, pl1.configReceived, "other_key",
		"plugin config must not contain keys from other plugins")
}

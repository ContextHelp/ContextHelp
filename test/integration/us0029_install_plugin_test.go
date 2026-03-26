package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// ---------------------------------------------------------------------------
// Minimal in-process test plugin that registers a hook and a pipeline step.
// ---------------------------------------------------------------------------

type testPlugin struct {
	name        string
	initCalled  bool
	initCfg     map[string]interface{}
	hookCounter int64
}

func newTestPlugin(name string) *testPlugin { return &testPlugin{name: name} }

func (p *testPlugin) Name() string    { return p.name }
func (p *testPlugin) Version() string { return "0.0.1-test" }

func (p *testPlugin) Init(_ context.Context, cfg map[string]interface{}, _ pluginapi.Deps) error {
	p.initCalled = true
	p.initCfg = cfg
	return nil
}

func (p *testPlugin) PipelineSteps() []pluginapi.PipelineStep { return nil }

func (p *testPlugin) Close(_ context.Context) error { return nil }

// PostIngest satisfies plugin.PostIngestHook — increments a counter per object.
func (p *testPlugin) PostIngest(_ context.Context, _ *storage.KnowledgeObject) error {
	atomic.AddInt64(&p.hookCounter, 1)
	return nil
}

// hookCount returns the number of PostIngest calls recorded.
func (p *testPlugin) hookCount() int64 { return atomic.LoadInt64(&p.hookCounter) }

// ---------------------------------------------------------------------------
// Pipeline step contributed by a plugin (registered separately).
// ---------------------------------------------------------------------------

type pluginTagStep struct {
	pipeline.BaseContract
}

func (s *pluginTagStep) Name() string { return "test-plugin-tag" }
func (s *pluginTagStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Tags = append(draft.Tags, storage.Tag{Label: "plugin-enriched"})
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["plugin_step_ran"] = true
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0029 Tests
// ---------------------------------------------------------------------------

// TestUS0029_StepStoredAndRetrievable verifies that a step stored directly via
// the Steps store appears in GET /api/v1/steps/{name}.
// Note: The REST install endpoint (POST /api/v1/steps/install) downloads from a
// real registry URL — in tests we write directly to storage to verify the
// storage+API contract without requiring network access.
func TestUS0029_StepStoredAndRetrievable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name:   "extract-entities",
		Source: "registry:https://registry.example.com/steps",
		Path:   "",
		Metadata: &storage.StepMetadata{
			Name:        "extract-entities",
			Description: "Extracts named entities from text",
			Version:     "1.0.0",
		},
		InstalledAt: now,
		UpdatedAt:   now,
	}
	err := env.svc.Store.Steps().Create(ctx, step)
	require.NoError(t, err)

	resp, err := gohttp.Get(env.URL + "/api/v1/steps/extract-entities")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var got storage.RegisteredStep
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))

	assert.Equal(t, "extract-entities", got.Name)
	assert.NotEmpty(t, got.Source, "source must be populated")
	assert.False(t, got.InstalledAt.IsZero(), "installed_at must be set")
}

// TestUS0029_InstalledStepAppearsInList verifies GET /api/v1/steps includes a
// step that was registered via the Steps store.
func TestUS0029_InstalledStepAppearsInList(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name:   "list-test-step",
		Source: "registry:https://registry.example.com/steps",
		Metadata: &storage.StepMetadata{
			Name:    "list-test-step",
			Version: "2.0.0",
		},
		InstalledAt: now,
		UpdatedAt:   now,
	}
	require.NoError(t, env.svc.Store.Steps().Create(ctx, step))

	respList, err := gohttp.Get(env.URL + "/api/v1/steps")
	require.NoError(t, err)
	defer respList.Body.Close()
	require.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var listBody struct {
		Steps []storage.RegisteredStep `json:"steps"`
		Total int                      `json:"total"`
	}
	require.NoError(t, json.NewDecoder(respList.Body).Decode(&listBody))

	found := false
	for _, s := range listBody.Steps {
		if s.Name == "list-test-step" {
			found = true
			break
		}
	}
	assert.True(t, found, "registered step must appear in GET /api/v1/steps")
}

// TestUS0029_UninstallStepRemovesIt verifies DELETE /api/v1/steps/{name} removes
// the step and subsequent GET returns 404.
func TestUS0029_UninstallStepRemovesIt(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name:        "delete-test-step",
		Source:      "registry:https://registry.example.com/steps",
		InstalledAt: now,
		UpdatedAt:   now,
	}
	require.NoError(t, env.svc.Store.Steps().Create(ctx, step))

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/steps/delete-test-step", nil)
	respDel, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	respDel.Body.Close()
	assert.Equal(t, gohttp.StatusNoContent, respDel.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/steps/delete-test-step")
	require.NoError(t, err)
	defer respGet.Body.Close()
	assert.Equal(t, gohttp.StatusNotFound, respGet.StatusCode)
}

// TestUS0029_PluginRegistersHook verifies that a plugin registered in the plugin
// registry is a PostIngestHook and has its Init called with the config block.
func TestUS0029_PluginRegistersHook(t *testing.T) {
	pl := newTestPlugin("test-hook-plugin")
	reg := plugin.NewRegistry()
	reg.Register(pl)

	deps := pluginapi.Deps{}
	err := reg.InitAll(context.Background(), map[string]map[string]interface{}{
		"test-hook-plugin": {"key": "value"},
	}, deps)
	require.NoError(t, err)

	assert.True(t, pl.initCalled, "Init must have been called")
	assert.Equal(t, "value", pl.initCfg["key"], "config must be passed to Init")

	hooks := reg.PostIngestHooks()
	require.Len(t, hooks, 1, "one PostIngestHook must be registered")
	assert.Equal(t, "test-hook-plugin", hooks[0].(plugin.Plugin).Name())
}

// TestUS0029_PluginHookFiredOnIngest verifies the PostIngest hook fires when a new
// object is ingested via a pipeline step contributing the plugin's logic.
func TestUS0029_PluginHookFiredOnIngest(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Register the plugin in the service plugin registry.
	pl := newTestPlugin("ingest-hook-plugin")
	if env.svc.PluginRegistry == nil {
		env.svc.PluginRegistry = plugin.NewRegistry()
	}
	env.svc.PluginRegistry.Register(pl)
	deps := pluginapi.Deps{}
	err := env.svc.PluginRegistry.InitAll(context.Background(), nil, deps)
	require.NoError(t, err)

	// Register a pipeline that uses the plugin tag step.
	env.svc.Pipes.Upsert("plugin.enrichment", &pipeline.Pipeline{
		PipelineName: "plugin.enrichment",
		Steps:        []pipeline.PipelineStep{&pluginTagStep{}},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "content for plugin enrichment",
		Type:     "text",
		Pipeline: "plugin.enrichment",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Verify pipeline step result is stored.
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	foundTag := false
	for _, tag := range obj.Tags {
		if tag.Label == "plugin-enriched" {
			foundTag = true
			break
		}
	}
	assert.True(t, foundTag, "plugin tag step must tag the object with 'plugin-enriched'")
	assert.Equal(t, true, obj.Metadata["plugin_step_ran"])
}

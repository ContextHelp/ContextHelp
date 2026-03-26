package markdownexport_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	markdownexport "github.com/ideacrafterslabs/ctxt-plugin-markdown-export"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestPlugin_NameVersion(t *testing.T) {
	p := markdownexport.New()
	assert.Equal(t, "markdown-export", p.Name())
	assert.Equal(t, "1.0.0", p.Version())
}

func TestPlugin_GeneratorName(t *testing.T) {
	p := markdownexport.New()
	require.NoError(t, p.Init(context.Background(), nil, pluginapi.Deps{}))
	assert.Equal(t, "obsidian-md", p.GeneratorName())
}

func TestPlugin_Accepts(t *testing.T) {
	p := markdownexport.New()
	require.NoError(t, p.Init(context.Background(), nil, pluginapi.Deps{}))

	obj := pluginapi.KnowledgeObject{ID: "obj_1", Type: "url", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	assert.True(t, p.Accepts(obj))
}

func TestPlugin_Accepts_EmptyID(t *testing.T) {
	p := markdownexport.New()
	require.NoError(t, p.Init(context.Background(), nil, pluginapi.Deps{}))

	obj := pluginapi.KnowledgeObject{Type: "url"}
	assert.False(t, p.Accepts(obj))
}

func TestPlugin_Accepts_Disabled(t *testing.T) {
	p := markdownexport.New()
	require.NoError(t, p.Init(context.Background(), map[string]interface{}{"enabled": false}, pluginapi.Deps{}))

	obj := pluginapi.KnowledgeObject{ID: "obj_1", Type: "url", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	assert.False(t, p.Accepts(obj))
}

func TestPlugin_Generate(t *testing.T) {
	p := markdownexport.New()
	require.NoError(t, p.Init(context.Background(), nil, pluginapi.Deps{}))

	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	obj := pluginapi.KnowledgeObject{
		ID:        "obj_gen1",
		Type:      "text",
		Summaries: []string{"Test summary."},
		Tags:      []pluginapi.Tag{{Label: "test"}},
		CreatedAt: now,
		UpdatedAt: now,
	}

	out, err := p.Generate(context.Background(), obj, pluginapi.OutputOptions{})
	require.NoError(t, err)
	assert.Contains(t, string(out), "id: obj_gen1")
	assert.Contains(t, string(out), "# Test summary.")
}

func TestPlugin_Generate_Disabled(t *testing.T) {
	p := markdownexport.New()
	require.NoError(t, p.Init(context.Background(), map[string]interface{}{"enabled": false}, pluginapi.Deps{}))

	obj := pluginapi.KnowledgeObject{ID: "obj_1", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_, err := p.Generate(context.Background(), obj, pluginapi.OutputOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestPlugin_PipelineSteps_Empty(t *testing.T) {
	p := markdownexport.New()
	require.NoError(t, p.Init(context.Background(), nil, pluginapi.Deps{}))
	assert.Empty(t, p.PipelineSteps())
}

func TestPlugin_Close(t *testing.T) {
	p := markdownexport.New()
	require.NoError(t, p.Init(context.Background(), nil, pluginapi.Deps{}))
	assert.NoError(t, p.Close(context.Background()))
}

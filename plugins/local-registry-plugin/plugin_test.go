package localregistry_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/require"

	localregistry "github.com/ideacrafterslabs/ctxt-plugin-local-registry"
)

const testdataDir = "testdata"

func newInitialised(t *testing.T) *localregistry.Plugin {
	t.Helper()
	p := localregistry.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"dir":  testdataDir,
		"name": "test-local",
	}, pluginapi.Deps{})
	require.NoError(t, err)
	return p
}

func TestPlugin_Name(t *testing.T) {
	p := localregistry.New()
	require.Equal(t, "local-registry", p.Name())
}

func TestPlugin_Init_MissingDir(t *testing.T) {
	p := localregistry.New()
	err := p.Init(context.Background(), map[string]interface{}{}, pluginapi.Deps{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "dir")
}

func TestPlugin_Metadata(t *testing.T) {
	p := newInitialised(t)
	meta, err := p.Metadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, "test-local", meta.Name)
	require.Contains(t, meta.Namespaces, "ai")
	require.Contains(t, meta.Namespaces, "infra")
}

func TestPlugin_FetchTaxonomy(t *testing.T) {
	p := newInitialised(t)
	tax, err := p.FetchTaxonomy(context.Background())
	require.NoError(t, err)
	require.Len(t, tax, 2)
	require.Equal(t, "ai", tax[0].Namespace)
	require.Equal(t, "Artificial Intelligence", tax[0].Title)
}

func TestPlugin_FetchEntities(t *testing.T) {
	p := newInitialised(t)
	entities, cursor, err := p.FetchEntities(context.Background(), "")
	require.NoError(t, err)
	require.Empty(t, cursor, "local provider always returns empty cursor (single page)")
	require.Len(t, entities, 2)

	slugs := make(map[string]bool)
	for _, e := range entities {
		slugs[e.Slug] = true
		require.Equal(t, "ai", e.Namespace)
	}
	require.True(t, slugs["bert"])
	require.True(t, slugs["gpt4"])
}

func TestPlugin_FetchEntities_DefaultNamespace(t *testing.T) {
	p := localregistry.New()
	err := p.Init(context.Background(), map[string]interface{}{
		"dir":       testdataDir,
		"namespace": "default-ns",
	}, pluginapi.Deps{})
	require.NoError(t, err)

	// Entities in testdata already have explicit namespace "ai"; default not applied.
	entities, _, err := p.FetchEntities(context.Background(), "")
	require.NoError(t, err)
	for _, e := range entities {
		require.NotEmpty(t, e.Namespace)
	}
}

func TestPlugin_ResolveEntity_Found(t *testing.T) {
	p := newInitialised(t)
	e, err := p.ResolveEntity(context.Background(), "ai", "bert")
	require.NoError(t, err)
	require.NotNil(t, e)
	require.Equal(t, "BERT", e.Title)
}

func TestPlugin_ResolveEntity_NotFound(t *testing.T) {
	p := newInitialised(t)
	e, err := p.ResolveEntity(context.Background(), "ai", "nonexistent")
	require.NoError(t, err)
	require.Nil(t, e)
}

func TestPlugin_PipelineSteps_Empty(t *testing.T) {
	p := localregistry.New()
	require.Empty(t, p.PipelineSteps())
}

func TestPlugin_Close(t *testing.T) {
	p := localregistry.New()
	require.NoError(t, p.Close(context.Background()))
}

// Compile-time interface satisfaction checks.
var _ pluginapi.Plugin = (*localregistry.Plugin)(nil)
var _ pluginapi.PluginRegistryProvider = (*localregistry.Plugin)(nil)

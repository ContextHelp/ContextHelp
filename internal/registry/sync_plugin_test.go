package registry_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/require"
)

// stubRegistryProvider is a minimal PluginRegistryProvider for testing.
type stubRegistryProvider struct {
	name     string
	entities []pluginapi.Entity
}

func (s *stubRegistryProvider) Name() string    { return s.name }
func (s *stubRegistryProvider) Version() string { return "0.0.1" }
func (s *stubRegistryProvider) Init(_ context.Context, _ map[string]interface{}, _ pluginapi.Deps) error {
	return nil
}
func (s *stubRegistryProvider) PipelineSteps() []pluginapi.PipelineStep { return nil }
func (s *stubRegistryProvider) Close(_ context.Context) error           { return nil }

func (s *stubRegistryProvider) Metadata(_ context.Context) (pluginapi.RegistryDescriptor, error) {
	return pluginapi.RegistryDescriptor{Name: s.name, Version: "0.0.1"}, nil
}

func (s *stubRegistryProvider) FetchTaxonomy(_ context.Context) (pluginapi.Taxonomy, error) {
	return nil, nil
}

func (s *stubRegistryProvider) FetchEntities(_ context.Context, _ string) ([]pluginapi.Entity, string, error) {
	return s.entities, "", nil
}

func (s *stubRegistryProvider) ResolveEntity(_ context.Context, ns, slug string) (*pluginapi.Entity, error) {
	for i := range s.entities {
		if s.entities[i].Namespace == ns && s.entities[i].Slug == slug {
			return &s.entities[i], nil
		}
	}
	return nil, nil
}

// Compile-time interface check.
var _ pluginapi.PluginRegistryProvider = (*stubRegistryProvider)(nil)

func TestSyncer_SyncPluginProvider_Full(t *testing.T) {
	store := &stubEntityStore{}
	syncer := registry.New(store)

	provider := &stubRegistryProvider{
		name: "test-plugin",
		entities: []pluginapi.Entity{
			{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
			{Slug: "ai.gpt", Title: "GPT", Namespace: "ai", VersionHash: "v2"},
		},
	}

	result, err := syncer.SyncPluginProvider(context.Background(), provider, config.RegistrySyncModeFull)
	require.NoError(t, err)
	require.Equal(t, 2, result.Upserted)
	require.Equal(t, "plugin://test-plugin", result.RegistryURL)
	require.Len(t, store.full, 2)
	require.Empty(t, store.thin)
}

func TestSyncer_SyncPluginProvider_Thin(t *testing.T) {
	store := &stubEntityStore{}
	syncer := registry.New(store)

	provider := &stubRegistryProvider{
		name: "test-plugin",
		entities: []pluginapi.Entity{
			{Slug: "ai.bert", Title: "BERT", Namespace: "ai", VersionHash: "v1"},
		},
	}

	result, err := syncer.SyncPluginProvider(context.Background(), provider, config.RegistrySyncModeThin)
	require.NoError(t, err)
	require.Equal(t, 1, result.Upserted)
	require.Len(t, store.thin, 1)
	require.Empty(t, store.full)
}

func TestSyncer_MultiSyncWithPlugins_AddsProviderResults(t *testing.T) {
	// HTTP side: single HTTP registry with 1 entity.
	httpEntry := registry.EntityIndexEntry{Slug: "ai.http", Title: "HTTP", Namespace: "ai", VersionHash: "h1"}
	srv := makeTestServer([]registry.EntityIndexEntry{httpEntry})
	defer srv.Close()

	// Plugin side: 1 entity.
	provider := &stubRegistryProvider{
		name: "plug",
		entities: []pluginapi.Entity{
			{Slug: "ai.plug", Title: "Plug", Namespace: "ai", VersionHash: "p1"},
		},
	}

	store := &stubEntityStore{}
	syncer := registry.New(store)

	cfg := registry.MultiSyncConfig{
		Registries: []config.RegistryConfig{{URL: srv.URL, SyncMode: config.RegistrySyncModeFull}},
	}

	result, err := syncer.MultiSyncWithPlugins(context.Background(), cfg,
		[]pluginapi.PluginRegistryProvider{provider})
	require.NoError(t, err)
	// 1 from HTTP + 1 from plugin.
	require.Equal(t, 2, result.Merged)
	require.Len(t, result.PerRegistry, 2)
}

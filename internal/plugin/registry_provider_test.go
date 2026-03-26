package plugin_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/require"
)

// stubRegistryProvider implements pluginapi.PluginRegistryProvider for testing.
type stubRegistryProvider struct{ name string }

func (s *stubRegistryProvider) Name() string    { return s.name }
func (s *stubRegistryProvider) Version() string { return "0.0.1" }
func (s *stubRegistryProvider) Init(_ context.Context, _ map[string]interface{}, _ plugin.Deps) error {
	return nil
}
func (s *stubRegistryProvider) PipelineSteps() []pipeline.PipelineStep { return nil }
func (s *stubRegistryProvider) Close(_ context.Context) error          { return nil }

func (s *stubRegistryProvider) Metadata(_ context.Context) (pluginapi.RegistryDescriptor, error) {
	return pluginapi.RegistryDescriptor{Name: s.name, Version: "0.0.1"}, nil
}
func (s *stubRegistryProvider) FetchTaxonomy(_ context.Context) (pluginapi.Taxonomy, error) {
	return nil, nil
}
func (s *stubRegistryProvider) FetchEntities(_ context.Context, _ string) ([]pluginapi.Entity, string, error) {
	return nil, "", nil
}
func (s *stubRegistryProvider) ResolveEntity(_ context.Context, _, _ string) (*pluginapi.Entity, error) {
	return nil, nil
}

// Compile-time interface satisfaction.
var _ pluginapi.PluginRegistryProvider = (*stubRegistryProvider)(nil)

func TestRegistry_RegistryProviders_Empty(t *testing.T) {
	r := plugin.NewRegistry()
	r.Register(&stubPlugin{name: "plain"})
	require.Empty(t, r.RegistryProviders())
}

func TestRegistry_RegistryProviders_Single(t *testing.T) {
	r := plugin.NewRegistry()
	r.Register(&stubRegistryProvider{name: "reg-prov"})
	providers := r.RegistryProviders()
	require.Len(t, providers, 1)
	require.Equal(t, "reg-prov", providers[0].Name())
}

func TestRegistry_RegistryProviders_Mixed(t *testing.T) {
	r := plugin.NewRegistry()
	r.Register(&stubPlugin{name: "plain"})
	r.Register(&stubRegistryProvider{name: "rp1"})
	r.Register(&stubRegistryProvider{name: "rp2"})
	require.Len(t, r.RegistryProviders(), 2)
}

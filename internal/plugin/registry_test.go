package plugin_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/require"
)

type stubPlugin struct{ name string }

func (s *stubPlugin) Name() string    { return s.name }
func (s *stubPlugin) Version() string { return "0.0.1" }
func (s *stubPlugin) Init(_ context.Context, _ map[string]interface{}, _ plugin.Deps) error {
	return nil
}
func (s *stubPlugin) PipelineSteps() []pipeline.PipelineStep { return nil }
func (s *stubPlugin) Close(_ context.Context) error          { return nil }

func TestRegistry_Register(t *testing.T) {
	r := plugin.NewRegistry()
	r.Register(&stubPlugin{name: "test"})
	require.Panics(t, func() { r.Register(&stubPlugin{name: "test"}) })
}

func TestRegistry_InitAll(t *testing.T) {
	r := plugin.NewRegistry()
	r.Register(&stubPlugin{name: "a"})
	r.Register(&stubPlugin{name: "b"})
	err := r.InitAll(context.Background(), nil, plugin.Deps{})
	require.NoError(t, err)
}

func TestRegistry_RegisterWithManifest(t *testing.T) {
	r := plugin.NewRegistry()
	m := &pluginapi.PluginManifest{
		APIVersion:  "v1",
		Name:        "myplugin",
		Version:     "1.0.0",
		Permissions: []string{pluginapi.PermReadObjects},
	}
	r.RegisterWithManifest(&stubPlugin{name: "myplugin"}, m)

	got := r.Manifest("myplugin")
	require.NotNil(t, got)
	require.Equal(t, "myplugin", got.Name)
	require.True(t, got.HasPermission(pluginapi.PermReadObjects))
}

func TestRegistry_Manifest_notFound(t *testing.T) {
	r := plugin.NewRegistry()
	require.Nil(t, r.Manifest("nonexistent"))
}

// stubOutputGenerator is a minimal OutputGenerator for registry tests.
type stubOutputGenerator struct {
	stubPlugin
	generatorName string
}

func (g *stubOutputGenerator) GeneratorName() string                    { return g.generatorName }
func (g *stubOutputGenerator) Accepts(_ pluginapi.KnowledgeObject) bool { return true }
func (g *stubOutputGenerator) Generate(_ context.Context, _ pluginapi.KnowledgeObject, _ pluginapi.OutputOptions) ([]byte, error) {
	return []byte("rendered"), nil
}

func TestRegistry_OutputGenerators_Empty(t *testing.T) {
	r := plugin.NewRegistry()
	r.Register(&stubPlugin{name: "plain"})
	require.Empty(t, r.OutputGenerators())
	require.Nil(t, r.FindOutputGenerator("any"))
}

func TestRegistry_FindOutputGenerator(t *testing.T) {
	r := plugin.NewRegistry()
	gen := &stubOutputGenerator{
		stubPlugin:    stubPlugin{name: "mdexport"},
		generatorName: "obsidian-md",
	}
	r.Register(gen)

	found := r.FindOutputGenerator("obsidian-md")
	require.NotNil(t, found)
	require.Equal(t, "obsidian-md", found.GeneratorName())

	out, err := found.Generate(context.Background(), pluginapi.KnowledgeObject{
		ID: "obj_1", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, pluginapi.OutputOptions{})
	require.NoError(t, err)
	require.Equal(t, []byte("rendered"), out)

	require.Nil(t, r.FindOutputGenerator("notion-export"))
}

func TestRegistry_Enforcer(t *testing.T) {
	r := plugin.NewRegistry()
	m := &pluginapi.PluginManifest{
		APIVersion:  "v1",
		Name:        "ep",
		Version:     "1.0.0",
		Permissions: []string{pluginapi.PermNetwork},
	}
	r.RegisterWithManifest(&stubPlugin{name: "ep"}, m)

	enforcer := r.Enforcer("ep")
	require.NotNil(t, enforcer)

	// network is allowed
	// filesystem is not in manifest → denied
	_, err := enforcer.ReadFile("/tmp/x")
	require.Error(t, err)
	var pde *pluginapi.PermissionDeniedError
	require.ErrorAs(t, err, &pde)
}

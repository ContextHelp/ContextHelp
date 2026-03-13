package plugin_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/plugin"
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

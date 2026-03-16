package builtins_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/stretchr/testify/require"
)

func TestPipelineProviderOverride(t *testing.T) {
	// Base config uses stub LLM everywhere.
	baseCfg := config.ProvidersConfig{}
	baseCfg.LLM.Backend = "stub"
	baseFactory := providers.NewFactory(baseCfg, nil)

	// Override url.generic to use anthropic LLM.
	pipelinesCfg := config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"url.generic": {
				Providers: map[string]config.ProviderBackendConfig{
					"llm": {Backend: "anthropic", Model: "claude-3-haiku-20240307"},
				},
			},
		},
	}

	reg := builtins.ConfiguredRegistryWithPipelineOverrides(baseFactory, baseCfg, pipelinesCfg, nil, 0)
	require.NotNil(t, reg)

	pipe, err := reg.Get("url.generic")
	require.NoError(t, err)
	require.NotNil(t, pipe)
	// Structural check: pipeline must have steps.
	require.NotEmpty(t, pipe.Steps)
}

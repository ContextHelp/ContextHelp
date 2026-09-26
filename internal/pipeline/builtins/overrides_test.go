package builtins_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/stretchr/testify/require"
)

func TestPipelineStructuralOverride(t *testing.T) {
	baseCfg := config.ProvidersConfig{}
	baseFactory := providers.NewFactory(baseCfg, nil)

	// "text.short" normally has: typedetector, sectioner, tagger, formatdetector, textcleaner, embedding, entity_extractor, entity_resolver
	// Let's skip 'tagger' and add 'noop' as an extra step.
	pipelinesCfg := config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"text.short": {
				SkipSteps:  []string{"tagger"},
				ExtraSteps: []string{"noop"},
			},
		},
	}

	reg := builtins.ConfiguredRegistryWithPipelineOverrides(builtins.BuildOpts{Factory: baseFactory}, baseCfg, pipelinesCfg)
	pipe, err := reg.Get("text.short")
	require.NoError(t, err)

	// Verify 'tagger' is gone.
	hasTagger := false
	for _, s := range pipe.Steps {
		if s.Name() == "tagger" {
			hasTagger = true
		}
	}
	require.False(t, hasTagger, "tagger should have been skipped")

	// Verify 'noop' was added at the end.
	require.NotEmpty(t, pipe.Steps)
	require.Equal(t, "noop", pipe.Steps[len(pipe.Steps)-1].Name())
}

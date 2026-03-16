package aliasing_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt-plugin-aliasing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAliasConfig_Defaults(t *testing.T) {
	cfg := aliasing.DefaultConfig()
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "global", cfg.DefaultScope)
}

func TestAliasConfig_FromMap(t *testing.T) {
	raw := map[string]interface{}{
		"enabled":       false,
		"default_scope": "profile",
	}
	cfg, err := aliasing.ConfigFromMap(raw)
	require.NoError(t, err)
	assert.False(t, cfg.Enabled)
	assert.Equal(t, "profile", cfg.DefaultScope)
}

package autosuggest_test

import (
	"testing"

	autosuggest "github.com/ideacrafterslabs/ctxt/plugins/autosuggest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfigDefaults(t *testing.T) {
	cfg := autosuggest.DefaultConfig()
	assert.Equal(t, "select", cfg.Mode)
	assert.Equal(t, 5, cfg.MaxTags)
	assert.Equal(t, 3, cfg.MaxMentions)
	assert.True(t, cfg.Enabled)
}

func TestConfigFromMap(t *testing.T) {
	raw := map[string]interface{}{
		"mode":            "generate",
		"max_tags":        10,
		"max_mentions":    5,
		"enabled":         true,
		"vocabulary_hint": []interface{}{"golang", "architecture", "decision"},
	}
	cfg, err := autosuggest.ConfigFromMap(raw)
	require.NoError(t, err)
	assert.Equal(t, "generate", cfg.Mode)
	assert.Equal(t, 10, cfg.MaxTags)
	assert.Equal(t, []string{"golang", "architecture", "decision"}, cfg.VocabularyHint)
}

func TestConfigFromYAML(t *testing.T) {
	src := `
mode: select
max_tags: 3
max_mentions: 2
vocabulary_hint:
  - go
  - plugin
`
	var raw map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(src), &raw))
	cfg, err := autosuggest.ConfigFromMap(raw)
	require.NoError(t, err)
	assert.Equal(t, "select", cfg.Mode)
	assert.Equal(t, 3, cfg.MaxTags)
}

package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_EmbeddingsDefaults(t *testing.T) {
	home := hermeticHome(t)
	withChdir(t, home)
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.InDelta(t, 0.99, cfg.Embeddings.MinCoverage, 1e-12)
	assert.Equal(t, 30*24*time.Hour, cfg.Embeddings.GracePeriod)
	assert.NoError(t, cfg.Embeddings.Validate())
}

func TestLoad_EmbeddingsFromFileAndOverride(t *testing.T) {
	home := hermeticHome(t)
	withChdir(t, home)
	writeYAML(t, filepath.Join(home, ".config", "contexthelp", "ctxt.yaml"), `
embeddings:
  min_coverage: 0.95
  grace_period: 168h
`)
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.InDelta(t, 0.95, cfg.Embeddings.MinCoverage, 1e-12)
	assert.Equal(t, 7*24*time.Hour, cfg.Embeddings.GracePeriod)

	cfg = layeredLoad(t, "", "", "embeddings.min_coverage=0.5", "embeddings.grace_period=1h")
	assert.InDelta(t, 0.5, cfg.Embeddings.MinCoverage, 1e-12)
	assert.Equal(t, time.Hour, cfg.Embeddings.GracePeriod)
}

func TestEmbeddingsConfig_Validate(t *testing.T) {
	for _, c := range []EmbeddingsConfig{
		{MinCoverage: 0, GracePeriod: 0},
		{MinCoverage: 1, GracePeriod: time.Hour},
	} {
		assert.NoError(t, c.Validate(), c)
	}
	for key, c := range map[string]EmbeddingsConfig{
		"embeddings.min_coverage": {MinCoverage: 1.01},
		"embeddings.grace_period": {MinCoverage: 0.99, GracePeriod: -time.Hour},
	} {
		err := c.Validate()
		require.Error(t, err, c)
		assert.Contains(t, err.Error(), key)
	}
	assert.ErrorContains(t, EmbeddingsConfig{MinCoverage: -0.1}.Validate(), "embeddings.min_coverage")
}

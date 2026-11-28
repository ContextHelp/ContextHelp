package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmptyYAMLDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(""), 0644)

	cfg, err := Load("ctxt", cfgPath)
	require.NoError(t, err)

	assert.Equal(t, 500*time.Millisecond, cfg.Jobs.PollInterval)
	assert.Equal(t, 30*time.Minute, cfg.Jobs.StaleTimeout)
	assert.Equal(t, 3, cfg.Jobs.MaxRetries)
	assert.Equal(t, 5, cfg.Jobs.MaxHops)
	assert.Equal(t, "env", cfg.Secrets.Backend)
	assert.Equal(t, "off", cfg.Conventions.EnforceMentionNamespaces)
	assert.Equal(t, "text.short", cfg.Inbox.Pipeline)
}

func TestWriteBackRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := &Config{
		Jobs: JobsConfig{
			PollInterval: 200 * time.Millisecond,
			StaleTimeout: 15 * time.Minute,
			MaxRetries:   5,
			MaxHops:      3,
		},
		Secrets: SecretsConfig{Backend: "env"},
	}

	err := WriteBack(original, path)
	require.NoError(t, err)

	loaded, err := Load("ctxt", path)
	require.NoError(t, err)

	assert.Equal(t, original.Jobs.PollInterval, loaded.Jobs.PollInterval)
	assert.Equal(t, original.Jobs.MaxRetries, loaded.Jobs.MaxRetries)
}

func TestRederiveDefaultsAndValidation(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(""), 0644))
	cfg, err := Load("ctxt", cfgPath)
	require.NoError(t, err)
	assert.True(t, cfg.Upgrade.Rederive.Enabled)
	assert.Equal(t, 2*time.Second, cfg.Upgrade.Rederive.Pause)
	assert.Empty(t, Validate(cfg))

	cfg.Upgrade.Rederive.Pause = -time.Second
	errs := Validate(cfg)
	require.Len(t, errs, 1)
	assert.Equal(t, "upgrade.rederive.pause", errs[0].Field)

	require.NoError(t, os.WriteFile(cfgPath, []byte("upgrade:\n  rederive:\n    enabled: false\n    pause: 500ms\n"), 0644))
	cfg, err = Load("ctxt", cfgPath)
	require.NoError(t, err)
	assert.False(t, cfg.Upgrade.Rederive.Enabled)
	assert.Equal(t, 500*time.Millisecond, cfg.Upgrade.Rederive.Pause)
}

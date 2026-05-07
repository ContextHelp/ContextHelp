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
		Version: 1,
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

	assert.Equal(t, original.Version, loaded.Version)
	assert.Equal(t, original.Jobs.PollInterval, loaded.Jobs.PollInterval)
	assert.Equal(t, original.Jobs.MaxRetries, loaded.Jobs.MaxRetries)
}

func TestMigrateV0ToV1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write a v0 config (no version field).
	v0yaml := []byte("storage:\n  type: sqlite\n")
	require.NoError(t, os.WriteFile(path, v0yaml, 0644))

	cfg, err := Load("ctxt", path)
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Version, "version should be bumped to 1")

	// Reload from disk to verify write-back.
	reloaded, err := Load("ctxt", path)
	require.NoError(t, err)
	assert.Equal(t, 1, reloaded.Version, "version should be persisted after migration")
}

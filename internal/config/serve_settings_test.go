package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadServe loads the dpkms config against an empty XDG tree, with body
// (when non-empty) as a -c config file and DPKMS_WORKERS unset.
func loadServe(t *testing.T, body string) *Config {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv(EnvConfigPath, "")
	t.Setenv(EnvDPKMSWorkers, "")
	var paths []string
	if body != "" {
		path := filepath.Join(xdg, "serve.yaml")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		paths = append(paths, path)
	}
	cfg, err := LoadWithOverrides("dpkms", "", paths, nil)
	require.NoError(t, err)
	return cfg
}

func TestServeSettingsDefaults(t *testing.T) {
	cfg := loadServe(t, "")
	assert.Equal(t, DefaultServerWorkers, cfg.Server.Workers)
	assert.False(t, cfg.Server.Dev)
	assert.Equal(t, DefaultReminderInterval, cfg.Server.ReminderInterval)
	assert.Empty(t, cfg.Steps.Path)
}

func TestServeSettingsFromFile(t *testing.T) {
	cfg := loadServe(t, "server:\n  workers: 6\n  dev: true\n  reminder_interval: 30s\nsteps:\n  path: /srv/steps\n")
	assert.Equal(t, 6, cfg.Server.Workers)
	assert.True(t, cfg.Server.Dev)
	assert.Equal(t, 30*time.Second, cfg.Server.ReminderInterval)
	assert.Equal(t, "/srv/steps", cfg.Steps.Path)
}

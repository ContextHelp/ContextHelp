package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_CaptureHistoryInitialLookbackDefault(t *testing.T) {
	home := hermeticHome(t)
	withChdir(t, home)
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, cfg.Capture.History.InitialLookback)
	assert.Equal(t, DefaultCaptureHistoryInitialLookback, cfg.Capture.History.InitialLookback)
}

func TestLoad_CaptureHistoryInitialLookbackFromFile(t *testing.T) {
	home := hermeticHome(t)
	withChdir(t, home)
	writeYAML(t, filepath.Join(home, ".config", "contexthelp", "ctxt.yaml"), `
capture:
  history:
    initial_lookback: 72h
`)
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Equal(t, 72*time.Hour, cfg.Capture.History.InitialLookback)
}

// Setting the history block must not disturb the url_filter sibling.
func TestLoad_CaptureHistoryKeepsURLFilter(t *testing.T) {
	cfg := layeredLoad(t, `
capture:
  url_filter:
    deny:
      - "*://crm.example.net/*"
  history:
    initial_lookback: 2h
`, "")
	assert.Equal(t, 2*time.Hour, cfg.Capture.History.InitialLookback)
	assert.Equal(t, "global", denyScope(t, cfg, "brave", "Work", "https://crm.example.net/x"))
}

func TestLoad_CaptureHistoryInitialLookbackOverride(t *testing.T) {
	cfg := layeredLoad(t, "", "", "capture.history.initial_lookback=90m")
	assert.Equal(t, 90*time.Minute, cfg.Capture.History.InitialLookback)
}

func TestCaptureHistoryConfig_Validate(t *testing.T) {
	for _, d := range []time.Duration{time.Minute, 24 * time.Hour} {
		assert.NoError(t, CaptureHistoryConfig{InitialLookback: d}.Validate(), d)
	}
	for _, d := range []time.Duration{0, -time.Hour} {
		err := CaptureHistoryConfig{InitialLookback: d}.Validate()
		require.Error(t, err, d)
		assert.Contains(t, err.Error(), "capture.history.initial_lookback")
	}
}

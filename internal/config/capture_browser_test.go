package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// capture.browser defaults to empty (auto-select); a file sets it and a
// later -c layer overrides it like any scalar key.
func TestLoad_CaptureBrowser(t *testing.T) {
	assert.Empty(t, layeredLoad(t, "", "").Capture.Browser, "empty by default: auto-select")

	user := "capture:\n  browser: brave\n"
	assert.Equal(t, "brave", layeredLoad(t, user, "").Capture.Browser)
	assert.Equal(t, "chrome", layeredLoad(t, user, "capture:\n  browser: chrome\n").Capture.Browser,
		"project layer wins over user")
	assert.Equal(t, "edge", layeredLoad(t, user, "", "capture.browser=edge").Capture.Browser,
		"-c key=value wins over files")

	withRules := "capture:\n  browser: vivaldi\n  url_filter:\n    deny: [\"*://x.example.net/*\"]\n"
	cfg := layeredLoad(t, withRules, "")
	assert.Equal(t, "vivaldi", cfg.Capture.Browser)
	assert.Equal(t, []string{"*://x.example.net/*"}, cfg.Capture.URLFilter.Deny, "sibling url_filter unaffected")
}

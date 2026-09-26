package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kitconfig "hop.top/kit/go/core/config"
)

func writeYAML(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

// denyScope returns the scope of the rule that dropped url, "" if allowed.
func denyScope(t *testing.T, cfg *Config, browser, profile, url string) string {
	t.Helper()
	f, err := cfg.Capture.URLFilter.For(browser, profile)
	require.NoError(t, err)
	d := f.Evaluate(url)
	if d.Allowed {
		return ""
	}
	return d.Rule.Scope
}

func TestLoad_CaptureURLFilterGlobalAndProfile(t *testing.T) {
	home := hermeticHome(t)
	withChdir(t, home)
	writeYAML(t, filepath.Join(home, ".config", "contexthelp", "ctxt.yaml"), `
capture:
  url_filter:
    deny:
      - "*://*.bank.example.com/*"
    browsers:
      brave:
        profiles:
          Work:
            deny:
              - "*://crm.example.net/*"
`)
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)

	assert.Equal(t, "global", denyScope(t, cfg, "brave", "Work", "https://bank.example.com/"))
	assert.Equal(t, "profile:brave/Work", denyScope(t, cfg, "brave", "Work", "https://crm.example.net/deals"))
	assert.Equal(t, "", denyScope(t, cfg, "brave", "Personal", "https://crm.example.net/deals"),
		"profile rules must not leak into other profiles")
	assert.Equal(t, "builtin", denyScope(t, cfg, "brave", "Personal", "http://localhost:8080/"),
		"builtin denies apply without any config")
}

func TestLoad_CaptureURLFilterDefaultsToBuiltinOnly(t *testing.T) {
	home := hermeticHome(t)
	withChdir(t, home)
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Empty(t, cfg.Capture.URLFilter.Deny, "no user rules ship in defaults")
	assert.Equal(t, "builtin", denyScope(t, cfg, "", "", "chrome://settings"))
	assert.Equal(t, "", denyScope(t, cfg, "", "", "https://example.com/"))
}

// Layer precedence follows kit config (yaml.v3 merge): a later layer
// replaces every list it sets, and replaces a browsers.<name> entry as a
// whole; map keys it does not set are left alone.
func TestLoad_CaptureURLFilterLayerPrecedence(t *testing.T) {
	home := hermeticHome(t)
	writeYAML(t, filepath.Join(home, ".config", "contexthelp", "ctxt.yaml"), `
capture:
  url_filter:
    deny:
      - "*://user-global.example.net/*"
    browsers:
      brave:
        profiles:
          Work:
            deny:
              - "*://user-work.example.net/*"
          Personal:
            deny:
              - "*://user-personal.example.net/*"
      chrome:
        deny:
          - "*://user-chrome.example.net/*"
`)
	project := filepath.Join(home, "work", "proj")
	writeYAML(t, filepath.Join(project, ".contexthelp", "ctxt.yaml"), `
capture:
  url_filter:
    browsers:
      brave:
        profiles:
          Work:
            deny:
              - "*://project-work.example.net/*"
`)
	withChdir(t, project)

	extra := filepath.Join(home, "extra.yaml")
	writeYAML(t, extra, `
capture:
  url_filter:
    deny:
      - "*://extra-global.example.net/*"
`)
	paths, overrides, err := kitconfig.ParseConfigArgs([]string{extra})
	require.NoError(t, err)
	cfg, err := LoadWithOverrides("ctxt", "", paths, overrides)
	require.NoError(t, err)

	// -c file replaced the global deny list.
	assert.Equal(t, "global", denyScope(t, cfg, "brave", "Work", "https://extra-global.example.net/"))
	assert.Equal(t, "", denyScope(t, cfg, "brave", "Work", "https://user-global.example.net/"))
	// Project replaced the whole browsers.brave entry: its Work rule
	// applies, the user file's brave rules (Work and Personal) do not.
	assert.Equal(t, "profile:brave/Work", denyScope(t, cfg, "brave", "Work", "https://project-work.example.net/"))
	assert.Equal(t, "", denyScope(t, cfg, "brave", "Work", "https://user-work.example.net/"))
	assert.Equal(t, "", denyScope(t, cfg, "brave", "Personal", "https://user-personal.example.net/"))
	// Browsers the project layer does not name keep the user file's rules.
	assert.Equal(t, "browser:chrome", denyScope(t, cfg, "chrome", "Default", "https://user-chrome.example.net/"))
}

func TestLoad_CaptureURLFilterKeyValueOverride(t *testing.T) {
	home := hermeticHome(t)
	withChdir(t, home)
	paths, overrides, err := kitconfig.ParseConfigArgs([]string{
		`capture.url_filter.deny=["*://crm.example.net/*"]`,
	})
	require.NoError(t, err)
	cfg, err := LoadWithOverrides("ctxt", "", paths, overrides)
	require.NoError(t, err)
	assert.Equal(t, []string{"*://crm.example.net/*"}, cfg.Capture.URLFilter.Deny)
	assert.Equal(t, "global", denyScope(t, cfg, "brave", "Work", "https://crm.example.net/x"))
}

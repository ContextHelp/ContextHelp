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

// layeredLoad writes a user file and a project file (either may be ""),
// then loads with the given -c tokens (files or key=value pairs).
func layeredLoad(t *testing.T, user, project string, args ...string) *Config {
	t.Helper()
	home := hermeticHome(t)
	if user != "" {
		writeYAML(t, filepath.Join(home, ".config", "contexthelp", "ctxt.yaml"), user)
	}
	proj := filepath.Join(home, "work", "proj")
	require.NoError(t, os.MkdirAll(proj, 0o755))
	if project != "" {
		writeYAML(t, filepath.Join(proj, ".contexthelp", "ctxt.yaml"), project)
	}
	withChdir(t, proj)
	paths, overrides, err := kitconfig.ParseConfigArgs(args)
	require.NoError(t, err)
	cfg, err := LoadWithOverrides("ctxt", "", paths, overrides)
	require.NoError(t, err)
	return cfg
}

// extraFile writes a file for a -c <file> token and returns its path.
func extraFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	writeYAML(t, p, body)
	return p
}

// Deny lists are a privacy control: every config layer (user file,
// project file, -c file, -c key=value) adds to them at global, browser
// and profile scope. A later layer never drops an earlier layer's rule.
func TestLoad_CaptureURLFilterDenyAccumulatesAcrossLayers(t *testing.T) {
	user := `
capture:
  url_filter:
    deny:
      - "*://user-global.example.net/*"
      - "*://shared.example.net/*"
    browsers:
      brave:
        deny:
          - "*://user-brave.example.net/*"
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
`
	project := `
capture:
  url_filter:
    deny:
      - "*://project-global.example.net/*"
      - "*://shared.example.net/*"
    browsers:
      brave:
        deny:
          - "*://project-brave.example.net/*"
        profiles:
          Work:
            deny:
              - "*://project-work.example.net/*"
`
	extra := extraFile(t, "extra.yaml", `
capture:
  url_filter:
    deny:
      - "*://extra-global.example.net/*"
    browsers:
      brave:
        deny:
          - "*://extra-brave.example.net/*"
        profiles:
          Work:
            deny:
              - "*://extra-work.example.net/*"
`)
	cfg := layeredLoad(t, user, project, extra,
		`capture.url_filter.deny=["*://kv-global.example.net/*"]`,
		`capture.url_filter.browsers.brave.deny=["*://kv-brave.example.net/*"]`,
		`capture.url_filter.browsers.brave.profiles.Work.deny=["*://kv-work.example.net/*"]`,
	)

	for _, tc := range []struct {
		browser, profile, url, scope string
	}{
		{"brave", "Work", "https://user-global.example.net/", "global"},
		{"brave", "Work", "https://project-global.example.net/", "global"},
		{"brave", "Work", "https://extra-global.example.net/", "global"},
		{"brave", "Work", "https://kv-global.example.net/", "global"},
		{"brave", "Work", "https://user-brave.example.net/", "browser:brave"},
		{"brave", "Work", "https://project-brave.example.net/", "browser:brave"},
		{"brave", "Work", "https://extra-brave.example.net/", "browser:brave"},
		{"brave", "Work", "https://kv-brave.example.net/", "browser:brave"},
		{"brave", "Work", "https://user-work.example.net/", "profile:brave/Work"},
		{"brave", "Work", "https://project-work.example.net/", "profile:brave/Work"},
		{"brave", "Work", "https://extra-work.example.net/", "profile:brave/Work"},
		{"brave", "Work", "https://kv-work.example.net/", "profile:brave/Work"},
		// Profiles and browsers no later layer names keep their rules.
		{"brave", "Personal", "https://user-personal.example.net/", "profile:brave/Personal"},
		{"chrome", "Default", "https://user-chrome.example.net/", "browser:chrome"},
		// Scoped rules still stay in their scope.
		{"brave", "Personal", "https://extra-work.example.net/", ""},
		{"chrome", "Default", "https://kv-brave.example.net/", ""},
	} {
		assert.Equal(t, tc.scope, denyScope(t, cfg, tc.browser, tc.profile, tc.url),
			"%s/%s %s", tc.browser, tc.profile, tc.url)
	}

	assert.Equal(t, []string{
		"*://user-global.example.net/*",
		"*://shared.example.net/*",
		"*://project-global.example.net/*",
		"*://extra-global.example.net/*",
		"*://kv-global.example.net/*",
	}, cfg.Capture.URLFilter.Deny, "global deny is the ordered union, duplicates removed")
}

// No layer can take a deny rule away: empty lists, nulls and a bare
// url_filter key all leave earlier rules in place.
func TestLoad_CaptureURLFilterNoLayerRemovesDeny(t *testing.T) {
	user := `
capture:
  url_filter:
    deny:
      - "*://crm.example.net/*"
    browsers:
      brave:
        deny:
          - "*://brave.example.net/*"
        profiles:
          Work:
            deny:
              - "*://work.example.net/*"
`
	project := `
capture:
  url_filter:
    deny: []
    browsers:
      brave:
        deny: []
        profiles:
          Work: {}
`
	nulls := extraFile(t, "nulls.yaml", `
capture:
  url_filter:
    deny: null
    browsers:
      brave: null
`)
	bare := extraFile(t, "bare.yaml", `
capture:
  url_filter:
`)
	cfg := layeredLoad(t, user, project, nulls, bare,
		`capture.url_filter.deny=[]`,
		`capture.url_filter.browsers.brave.profiles.Work.deny=[]`,
	)
	assert.Equal(t, "global", denyScope(t, cfg, "brave", "Work", "https://crm.example.net/x"))
	assert.Equal(t, "browser:brave", denyScope(t, cfg, "brave", "Work", "https://brave.example.net/x"))
	assert.Equal(t, "profile:brave/Work", denyScope(t, cfg, "brave", "Work", "https://work.example.net/x"))

	for _, kv := range []string{`capture.url_filter=null`, `capture=null`, `capture.url_filter.browsers=null`} {
		cfg = layeredLoad(t, user, "", kv)
		assert.Equal(t, "global", denyScope(t, cfg, "brave", "Work", "https://crm.example.net/x"), kv)
		assert.Equal(t, "profile:brave/Work", denyScope(t, cfg, "brave", "Work", "https://work.example.net/x"), kv)
	}
}

type filterOutcome struct {
	Allowed bool
	Scope   string
}

func outcome(t *testing.T, cfg *Config, browser, profile, url string) filterOutcome {
	t.Helper()
	f, err := cfg.Capture.URLFilter.For(browser, profile)
	require.NoError(t, err)
	d := f.Evaluate(url)
	return filterOutcome{d.Allowed, d.Rule.Scope}
}

// allow_only keeps its narrowing meaning across layers: each layer's
// non-empty list at a scope is one more list the URL must match.
func TestLoad_CaptureURLFilterAllowOnlyEveryLayerMustPass(t *testing.T) {
	user := `
capture:
  url_filter:
    allow_only:
      - "*://a.example.com/*"
      - "*://b.example.com/*"
    browsers:
      brave:
        profiles:
          Work:
            allow_only:
              - "*://a.example.com/*"
`
	project := `
capture:
  url_filter:
    allow_only:
      - "*://b.example.com/*"
      - "*://c.example.com/*"
    browsers:
      brave:
        profiles:
          Work:
            allow_only:
              - "*://b.example.com/*"
`
	cfg := layeredLoad(t, user, project)
	assert.Equal(t, filterOutcome{false, "global"}, outcome(t, cfg, "brave", "Personal", "https://a.example.com/"),
		"the user list alone no longer suffices")
	assert.Equal(t, filterOutcome{true, ""}, outcome(t, cfg, "brave", "Personal", "https://b.example.com/"))
	assert.Equal(t, filterOutcome{false, "global"}, outcome(t, cfg, "brave", "Personal", "https://c.example.com/"),
		"the project list does not widen the user list")
	assert.Equal(t, filterOutcome{false, "profile:brave/Work"}, outcome(t, cfg, "brave", "Work", "https://b.example.com/"),
		"profile lists from both layers apply")

	// A layer that sets no allow_only leaves the earlier list in force.
	cfg = layeredLoad(t, user, `
capture:
  url_filter:
    allow_only: []
`, `capture.url_filter.deny=["*://d.example.com/*"]`)
	assert.Equal(t, filterOutcome{true, ""}, outcome(t, cfg, "brave", "Personal", "https://a.example.com/"))
	assert.Equal(t, filterOutcome{false, "global"}, outcome(t, cfg, "brave", "Personal", "https://z.example.com/"))
}

// Accumulation is specific to capture.url_filter; every other key keeps
// kit's layer precedence (later layer wins, lists replaced).
func TestLoad_CaptureURLFilterOtherKeysKeepPrecedence(t *testing.T) {
	cfg := layeredLoad(t, `
server:
  port: 1111
i18n:
  preferred_languages: [en, fr]
capture:
  url_filter:
    deny: ["*://crm.example.net/*"]
`, `
server:
  port: 2222
i18n:
  preferred_languages: [de]
`, `server.port=3333`)
	assert.Equal(t, 3333, cfg.Server.Port)
	assert.Equal(t, []string{"de"}, cfg.I18n.PreferredLanguages)
	assert.Equal(t, []string{"*://crm.example.net/*"}, cfg.Capture.URLFilter.Deny)

	cfg = layeredLoad(t, `
i18n:
  preferred_languages: [en, fr]
`, "", `i18n.preferred_languages=[it]`)
	assert.Equal(t, []string{"it"}, cfg.I18n.PreferredLanguages)
}

// A -c key=value list override adds to the file layers' deny list.
func TestLoad_CaptureURLFilterKeyValueOverrideAdds(t *testing.T) {
	cfg := layeredLoad(t, `
capture:
  url_filter:
    deny: ["*://*.bank.example.com/*"]
`, "", `capture.url_filter.deny=["*://crm.example.net/*"]`)
	assert.Equal(t, []string{"*://*.bank.example.com/*", "*://crm.example.net/*"}, cfg.Capture.URLFilter.Deny)
	assert.Equal(t, "global", denyScope(t, cfg, "brave", "Work", "https://crm.example.net/x"))
	assert.Equal(t, "global", denyScope(t, cfg, "brave", "Work", "https://www.bank.example.com/x"))
}

// A bare scalar (-c capture.url_filter.deny=<rule>) is not a list: Load
// fails instead of dropping or replacing the configured rules.
func TestLoad_CaptureURLFilterScalarOverrideFailsLoad(t *testing.T) {
	for _, kv := range []string{
		`capture.url_filter.deny=*://crm.example.net/*`,
		`capture.url_filter.deny=crm.example.net`,
		`capture.url_filter.browsers.brave.profiles.Work.deny=crm.example.net`,
	} {
		home := hermeticHome(t)
		writeYAML(t, filepath.Join(home, ".config", "contexthelp", "ctxt.yaml"), `
capture:
  url_filter:
    deny: ["*://*.bank.example.com/*"]
`)
		withChdir(t, home)
		paths, overrides, err := kitconfig.ParseConfigArgs([]string{kv})
		require.NoError(t, err, kv)
		_, err = LoadWithOverrides("ctxt", "", paths, overrides)
		require.Error(t, err, kv)
		assert.Contains(t, err.Error(), "apply CLI overrides", kv)
	}
}

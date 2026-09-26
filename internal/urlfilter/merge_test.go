package urlfilter

import (
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// decodeLayers decodes each document onto the same Config, the way a
// layered config loader does.
func decodeLayers(t *testing.T, docs ...string) Config {
	t.Helper()
	var c Config
	for _, d := range docs {
		if err := yaml.Unmarshal([]byte(d), &c); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func scopeOf(t *testing.T, c Config, browser, profile, url string) string {
	t.Helper()
	f, err := c.For(browser, profile)
	if err != nil {
		t.Fatal(err)
	}
	d := f.Evaluate(url)
	if d.Allowed {
		return ""
	}
	return d.Rule.Scope
}

func TestUnmarshalYAMLAccumulatesDeny(t *testing.T) {
	t.Parallel()
	c := decodeLayers(t, `
deny: ["*://a.example.net/*", "*://shared.example.net/*"]
browsers:
  brave:
    deny: ["*://brave-a.example.net/*"]
    profiles:
      Work: {deny: ["*://work-a.example.net/*"]}
      Personal: {deny: ["*://personal-a.example.net/*"]}
`, `
deny: ["*://b.example.net/*", "*://shared.example.net/*"]
browsers:
  brave:
    deny: ["*://brave-b.example.net/*"]
    profiles:
      Work: {deny: ["*://work-b.example.net/*"]}
`, `
deny: []
browsers:
  brave: null
`)
	if want := []string{"*://a.example.net/*", "*://shared.example.net/*", "*://b.example.net/*"}; !slices.Equal(c.Deny, want) {
		t.Fatalf("global deny = %v, want %v", c.Deny, want)
	}
	for _, tc := range []struct{ profile, url, scope string }{
		{"Work", "https://a.example.net/", ScopeGlobal},
		{"Work", "https://b.example.net/", ScopeGlobal},
		{"Work", "https://brave-a.example.net/", "browser:brave"},
		{"Work", "https://brave-b.example.net/", "browser:brave"},
		{"Work", "https://work-a.example.net/", "profile:brave/Work"},
		{"Work", "https://work-b.example.net/", "profile:brave/Work"},
		{"Personal", "https://personal-a.example.net/", "profile:brave/Personal"},
		{"Personal", "https://work-b.example.net/", ""},
	} {
		if got := scopeOf(t, c, "brave", tc.profile, tc.url); got != tc.scope {
			t.Errorf("brave/%s %s: scope %q, want %q", tc.profile, tc.url, got, tc.scope)
		}
	}
}

func TestUnmarshalYAMLIntoZeroConfigIsPlainDecode(t *testing.T) {
	t.Parallel()
	c := decodeLayers(t, `
deny: ["*://a.example.net/*"]
allow_only: ["*://b.example.net/*"]
`)
	if !slices.Equal(c.Deny, []string{"*://a.example.net/*"}) ||
		!slices.Equal(c.AllowOnly, []string{"*://b.example.net/*"}) ||
		c.moreAllowOnly != nil || c.Browsers != nil {
		t.Fatalf("got %+v", c)
	}
}

func TestMergeAllowOnlyEveryListMustMatch(t *testing.T) {
	t.Parallel()
	c := decodeLayers(t,
		`allow_only: ["*://a.example.com/*", "*://b.example.com/*"]`,
		`allow_only: ["*://b.example.com/*", "*://c.example.com/*"]`,
		// Same set in another order: no extra gate.
		`allow_only: ["*://c.example.com/*", "*://b.example.com/*"]`,
		`allow_only: []`,
	)
	if len(c.moreAllowOnly) != 1 {
		t.Fatalf("moreAllowOnly = %v, want one extra list", c.moreAllowOnly)
	}
	for url, allowed := range map[string]bool{
		"https://a.example.com/": false,
		"https://b.example.com/": true,
		"https://c.example.com/": false,
		"https://z.example.com/": false,
	} {
		f, err := c.For("")
		if err != nil {
			t.Fatal(err)
		}
		if got := f.Evaluate(url); got.Allowed != allowed {
			t.Errorf("Filter %s: %v, want allowed=%v", url, got, allowed)
		}
	}
}

func TestNewRejectsBadRuleInExtraAllowOnlyList(t *testing.T) {
	t.Parallel()
	c := decodeLayers(t,
		`allow_only: ["*://a.example.com/*"]`,
		`allow_only: ["*://[bad/*"]`,
	)
	if _, err := c.For(""); err == nil {
		t.Fatal("want compile error for the second allow_only list")
	}
}

func TestMergeDoesNotAliasInputs(t *testing.T) {
	t.Parallel()
	base := Config{
		Rules: Rules{Deny: make([]string, 1, 8)},
		Browsers: map[string]BrowserConfig{
			"brave": {Profiles: map[string]Rules{"Work": {Deny: []string{"*://w.example.net/*"}}}},
		},
	}
	base.Deny[0] = "*://a.example.net/*"
	snapshot := base // shares slices and maps with base
	layer := Config{
		Rules:    Rules{Deny: []string{"*://b.example.net/*"}},
		Browsers: map[string]BrowserConfig{"brave": {Profiles: map[string]Rules{"Work": {Deny: []string{"*://x.example.net/*"}}}}},
	}
	base.Merge(layer)

	if got := snapshot.Deny[:cap(snapshot.Deny)][1]; got != "" {
		t.Fatalf("Merge wrote into the old backing array: %q", got)
	}
	if got := snapshot.Browsers["brave"].Profiles["Work"].Deny; !slices.Equal(got, []string{"*://w.example.net/*"}) {
		t.Fatalf("Merge mutated the old browsers map: %v", got)
	}
	if got := base.Browsers["brave"].Profiles["Work"].Deny; !slices.Equal(got, []string{"*://w.example.net/*", "*://x.example.net/*"}) {
		t.Fatalf("merged profile deny = %v", got)
	}
}

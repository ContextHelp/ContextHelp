package urlfilter

import (
	"testing"

	"gopkg.in/yaml.v3"
)

const sampleConfig = `
deny:
  - "*://*.bank.example.com/*"
browsers:
  brave:
    deny:
      - "*://ads.example.com/*"
    profiles:
      Work:
        deny:
          - "*://crm.example.net/*"
          - "*://drive.example.com/*"
      Personal:
        allow_only:
          - "*://*.example.org/*"
  chrome:
    profiles:
      Work:
        deny:
          - "*://chrome-only.example.net/*"
`

func loadSample(t *testing.T) Config {
	t.Helper()
	var c Config
	if err := yaml.Unmarshal([]byte(sampleConfig), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConfigForMergesScopes(t *testing.T) {
	t.Parallel()
	c := loadSample(t)

	cases := []struct {
		name     string
		browser  string
		profiles []string
		url      string
		scope    string // "" = allowed
	}{
		{"builtin applies everywhere", "brave", []string{"Work"}, "http://localhost:3000/", ScopeBuiltin},
		{"builtin with no browser", "", nil, "chrome://settings", ScopeBuiltin},
		{"global applies to profile", "brave", []string{"Work"}, "https://bank.example.com/", ScopeGlobal},
		{"global applies with no browser", "", nil, "https://www.bank.example.com/", ScopeGlobal},
		{"browser scope", "brave", []string{"Personal"}, "https://ads.example.com/x", "browser:brave"},
		{"profile scope", "brave", []string{"Work"}, "https://crm.example.net/deals", "profile:brave/Work"},
		{"profile rules stay in their profile", "brave", []string{"Personal"}, "https://crm.example.net/deals", "profile:brave/Personal"},
		{"other browser's profile not applied", "brave", []string{"Work"}, "https://chrome-only.example.net/", ""},
		{"browser key case-insensitive", "Brave", []string{"work"}, "https://crm.example.net/", "profile:brave/Work"},
		{"any profile alias applies", "brave", []string{"Profile 3", "Work"}, "https://drive.example.com/d/1", "profile:brave/Work"},
		{"profile allow_only narrows", "brave", []string{"Personal"}, "https://news.example.org/", ""},
		{"no scoped rules for unknown profile", "brave", []string{"Other"}, "https://crm.example.net/", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := c.For(tc.browser, tc.profiles...)
			if err != nil {
				t.Fatal(err)
			}
			d := f.Evaluate(tc.url)
			if tc.scope == "" {
				if !d.Allowed {
					t.Errorf("want allowed, got %v", d)
				}
				return
			}
			if d.Allowed || d.Rule.Scope != tc.scope {
				t.Errorf("want denied in scope %q, got %v", tc.scope, d)
			}
		})
	}
}

func TestConfigForBuiltinDefaultsAlwaysOn(t *testing.T) {
	t.Parallel()
	// A user list never replaces the builtin list.
	c := Config{Rules: Rules{Deny: []string{"*://crm.example.net/*"}}}
	f, err := c.For("brave", "Work")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{
		"http://localhost/", "http://localhost:8080/x", "http://app.localhost:5173/",
		"http://127.0.0.1:9000/", "http://[::1]:3000/", "file:///Users/someone/notes.md",
		"about:blank", "chrome://settings", "brave://rewards", "chrome-extension://abc/popup.html",
		"edge://flags", "view-source:https://example.com/", "javascript:void(0)", "data:text/plain,hi",
	} {
		if d := f.Evaluate(u); d.Allowed || d.Rule.Scope != ScopeBuiltin {
			t.Errorf("%q: want builtin deny, got %v", u, d)
		}
	}
	if !f.Matches("https://news.example.org/") {
		t.Error("ordinary URL denied")
	}
}

func TestConfigForInvalidRuleIsError(t *testing.T) {
	t.Parallel()
	c := Config{Browsers: map[string]BrowserConfig{
		"brave": {Profiles: map[string]Rules{"Work": {Deny: []string{"*://user@crm.example.net/*"}}}},
	}}
	if _, err := c.For("brave", "Work"); err == nil {
		t.Error("For with an invalid profile rule: expected error")
	}
}

func TestBuiltinDenyIsACopy(t *testing.T) {
	t.Parallel()
	b := BuiltinDeny()
	b[0] = "mutated"
	if BuiltinDeny()[0] == "mutated" {
		t.Error("BuiltinDeny exposes the shared slice")
	}
}

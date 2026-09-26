package urlfilter

import (
	"bytes"
	"log/slog"
	"strings"
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
		"http://127.0.0.1:9000/", "http://[::1]:3000/", "https://localhost/",
		"file:///Users/someone/notes.md", "about:blank", "chrome://settings",
	} {
		if d := f.Evaluate(u); d.Allowed || d.Rule.Scope != ScopeBuiltin {
			t.Errorf("%q: want builtin deny, got %v", u, d)
		}
	}
	if !f.Matches("https://news.example.org/") {
		t.Error("ordinary URL denied")
	}
}

func TestConfigForAdmitsOnlyHTTPSchemes(t *testing.T) {
	t.Parallel()
	// allow_only everything: the scheme gate still runs first.
	c := Config{Rules: Rules{AllowOnly: []string{"*://*/*"}, Deny: []string{"*://*.example.com/*"}}}
	f, err := c.For("brave", "Work")
	if err != nil {
		t.Fatal(err)
	}
	for u, scheme := range map[string]string{
		"ws://news.example.org/socket":            "ws",
		"wss://news.example.org/socket":           "wss",
		"ftp://ftp.example.org/pub/":              "ftp",
		"file:///Users/someone/notes.md":          "file",
		"about:blank":                             "about",
		"chrome://settings/passwords":             "chrome",
		"brave://rewards":                         "brave",
		"edge://flags":                            "edge",
		"chrome-extension://abc/popup.html":       "chrome-extension",
		"devtools://devtools/bundled/inspector":   "devtools",
		"data:text/plain,hi":                      "data",
		"blob:https://news.example.org/uuid":      "blob",
		"javascript:void(0)":                      "javascript",
		"view-source:https://news.example.org/":   "view-source",
		"mailto:someone@example.org":              "mailto",
		"notion://www.notion.so/page":             "notion",
		"ws://localhost:8080/":                    "ws",
		"HTTPX://news.example.org/":               "httpx",
		"ftp://crm.example.com/":                  "ftp",
		"view-source:https://crm.example.com/x?a": "view-source",
	} {
		d := f.Evaluate(u)
		if d.Allowed || d.Reason != ReasonSchemeNotCaptured || d.Rule.Scope != ScopeBuiltin || d.Scheme != scheme {
			t.Errorf("%q: want scheme %q not captured, got %+v", u, scheme, d)
			continue
		}
		if got, want := d.String(), "builtin: scheme not captured ("+scheme+")"; got != want {
			t.Errorf("%q: String = %q, want %q", u, got, want)
		}
	}
	for _, u := range []string{"http://news.example.org/", "https://news.example.org/a?b=c", "HTTPS://news.example.org/"} {
		if d := f.Evaluate(u); !d.Allowed {
			t.Errorf("%q: want allowed, got %v", u, d)
		}
	}
	if d := f.Evaluate("https://crm.example.com/"); d.Reason != ReasonDenyRule {
		t.Errorf("http(s) URL still goes through the rules: got %v", d)
	}
}

func TestSchemeDecisionLogOmitsURL(t *testing.T) {
	t.Parallel()
	f, err := Config{}.For("")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("capture dropped", "decision", f.Evaluate("view-source:https://crm.example.net/deals/42"))
	out := buf.String()
	if strings.Contains(out, "deals/42") || strings.Contains(out, "crm.example.net") {
		t.Errorf("log leaked URL: %s", out)
	}
	for _, want := range []string{"reason=scheme_not_captured", "scope=builtin", "scheme=view-source"} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q missing %q", out, want)
		}
	}
}

func TestNewWithoutBuiltinHasNoSchemeGate(t *testing.T) {
	t.Parallel()
	f, err := New(Layer{Scope: ScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Matches("ftp://ftp.example.org/") {
		t.Error("the scheme gate belongs to the builtin layer of Config.For only")
	}
}

func TestBuiltinDenyIsURLForm(t *testing.T) {
	t.Parallel()
	for _, r := range BuiltinDeny() {
		if !strings.Contains(r, "://") {
			t.Errorf("builtin rule %q is not in the scheme://host form", r)
		}
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

package urlfilter

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestUnparseableURLIsDenied(t *testing.T) {
	t.Parallel()
	bad := []string{
		"http://[::1",            // unterminated IPv6 literal
		"http://a b.example.com", // space in host
		"%zz",                    // bad escape
		"not a url",              // no scheme
		"",                       // empty
	}
	for _, u := range bad {
		for name, ev := range map[string]*Filter{
			"nil Filter":   nil,
			"empty Filter": mustNew(t),
			"allow-all":    mustNew(t, Layer{Scope: ScopeGlobal, Rules: Rules{AllowOnly: []string{"*://*/*"}}}),
		} {
			d := ev.Evaluate(u)
			if d.Allowed || d.Reason != ReasonUnparseable {
				t.Errorf("%s.Evaluate(%q) = %+v, want denied %s", name, u, d, ReasonUnparseable)
			}
		}
	}
}

func TestInvalidInternationalHostIsDenied(t *testing.T) {
	t.Parallel()
	// U+2028 passes url.Parse but is not a valid IDNA code point.
	d := (*Filter)(nil).Evaluate("https://bad\u2028host.example/")
	if d.Allowed || d.Reason != ReasonUnparseable {
		t.Errorf("invalid IDN host allowed: %+v", d)
	}
}

func TestNewRejectsInvalidRules(t *testing.T) {
	t.Parallel()
	for _, r := range []string{
		"://example.com/*",          // empty scheme
		"*://user@example.com/*",    // userinfo
		"*://example.com:http/*",    // non-numeric port
		"*://[::1/*",                // unterminated IPv6
		"*://*.bad\u2028host.test/", // invalid IDN
		"*://ex*ämple.test/*",       // non-ASCII glob host
	} {
		if _, err := New(Layer{Scope: ScopeGlobal, Rules: Rules{Deny: []string{r}}}); err == nil {
			t.Errorf("New(deny %q): expected error", r)
		} else if !strings.Contains(err.Error(), ScopeGlobal) {
			t.Errorf("New(deny %q) error %q does not name the scope", r, err)
		}
		if _, err := New(Layer{Scope: ScopeGlobal, Rules: Rules{AllowOnly: []string{r}}}); err == nil {
			t.Errorf("New(allow_only %q): expected error", r)
		}
	}
}

func TestDecisionNamesMatchingRule(t *testing.T) {
	t.Parallel()
	f := mustNew(
		t,
		Layer{Scope: ScopeGlobal, Rules: Rules{Deny: []string{"*://*.bank.example.com/*"}}},
		Layer{Scope: "profile:brave/Work", Rules: Rules{Deny: []string{"*://crm.example.net/*"}}},
	)
	d := f.Evaluate("https://crm.example.net/deals/42?owner=someone")
	want := Rule{Pattern: "*://crm.example.net/*", List: ListDeny, Scope: "profile:brave/Work"}
	if d.Allowed || d.Reason != ReasonDenyRule || d.Rule != want {
		t.Fatalf("Evaluate = %+v, want deny by %+v", d, want)
	}
	if got := d.String(); got != `denied by deny rule "*://crm.example.net/*" (profile:brave/Work)` {
		t.Errorf("String = %q", got)
	}
	if ok := f.Evaluate("https://news.example.org/"); !ok.Allowed || ok.Reason != ReasonAllowed {
		t.Errorf("unrelated URL: %+v", ok)
	}
}

func TestDecisionLogOmitsURL(t *testing.T) {
	t.Parallel()
	const secret = "https://crm.example.net/deals/42?owner=someone"
	f := mustNew(t, Layer{Scope: ScopeGlobal, Rules: Rules{Deny: []string{"*://crm.example.net/*"}}})
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("capture dropped", "decision", f.Evaluate(secret))
	out := buf.String()
	if strings.Contains(out, "deals/42") || strings.Contains(out, "owner=") {
		t.Errorf("log leaked URL: %s", out)
	}
	for _, want := range []string{"reason=deny_rule", "scope=global", `rule=*://crm.example.net/*`} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q missing %q", out, want)
		}
	}
}

func TestLayersOnlyNarrow(t *testing.T) {
	t.Parallel()
	f := mustNew(
		t,
		Layer{Scope: ScopeGlobal, Rules: Rules{AllowOnly: []string{"*://*.example.org/*"}}},
		Layer{Scope: "profile:brave/Work", Rules: Rules{AllowOnly: []string{"*://news.example.org/*", "*://docs.example.com/*"}}},
	)
	cases := []struct {
		url     string
		allowed bool
		scope   string
	}{
		{"https://news.example.org/a", true, ""},
		// Passes the profile list but not the global one.
		{"https://docs.example.com/a", false, ScopeGlobal},
		// Passes the global list but not the profile one.
		{"https://blog.example.org/a", false, "profile:brave/Work"},
	}
	for _, c := range cases {
		d := f.Evaluate(c.url)
		if d.Allowed != c.allowed || (!c.allowed && (d.Reason != ReasonNotAllowListed || d.Rule.Scope != c.scope)) {
			t.Errorf("Evaluate(%q) = %+v, want allowed=%v scope=%q", c.url, d, c.allowed, c.scope)
		}
	}
}

func TestDenyBeatsAllowOnly(t *testing.T) {
	t.Parallel()
	f := mustNew(
		t,
		Layer{Scope: ScopeGlobal, Rules: Rules{AllowOnly: []string{"*://*.example.net/*"}}},
		Layer{Scope: "profile:brave/Work", Rules: Rules{Deny: []string{"*://crm.example.net/*"}}},
	)
	if d := f.Evaluate("https://crm.example.net/"); d.Allowed || d.Reason != ReasonDenyRule {
		t.Errorf("deny should win over allow_only: %+v", d)
	}
}

func TestOpaqueURLsOnlyMatchLegacyRules(t *testing.T) {
	t.Parallel()
	deny := func(rule string) *Filter {
		return mustNew(t, Layer{Scope: ScopeGlobal, Rules: Rules{Deny: []string{rule}}})
	}
	if !deny("*://*/*").Matches("about:blank") {
		t.Error("URL-form rule matched an opaque URL")
	}
	if deny("about:*").Matches("about:blank") {
		t.Error("legacy about:* did not match about:blank")
	}
}

func mustNew(t *testing.T, layers ...Layer) *Filter {
	t.Helper()
	f, err := New(layers...)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

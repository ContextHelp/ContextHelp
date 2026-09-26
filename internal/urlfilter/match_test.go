package urlfilter

import "testing"

// denyCases pins what a user writing the US-0214 style rules expects a
// single deny rule to do. Each case is one rule against one URL.
var denyCases = []struct {
	name   string
	rule   string
	url    string
	denied bool
}{
	// Ports: a rule without a port covers every port on that host.
	{"port/explicit port", "*://localhost/*", "http://localhost:8080/x", true},
	{"port/no port", "*://localhost/*", "http://localhost/x", true},
	{"port/ipv4 loopback with port", "*://127.0.0.1/*", "http://127.0.0.1:3000/", true},
	{"port/pinned port matches", "*://localhost:8080/*", "http://localhost:8080/x", true},
	{"port/pinned port other port", "*://localhost:8080/*", "http://localhost:9090/x", false},
	{"port/pinned default port", "*://example.com:443/*", "https://example.com/x", true},
	{"port/wildcard port", "*://localhost:*/*", "http://localhost:5173/", true},

	// Apex vs subdomain: *.d covers d itself and every subdomain.
	{"apex/apex host", "*://*.bank.example.com/*", "https://bank.example.com/account", true},
	{"apex/subdomain", "*://*.bank.example.com/*", "https://www.bank.example.com/account", true},
	{"apex/deep subdomain", "*://*.bank.example.com/*", "https://a.b.bank.example.com/", true},
	{"apex/lookalike suffix", "*://*.bank.example.com/*", "https://evilbank.example.com/", false},
	{"apex/lookalike prefix", "*://*.bank.example.com/*", "https://bank.example.com.evil.test/", false},
	{"apex/exact host only", "*://crm.example.net/*", "https://eu.crm.example.net/", false},

	// Paths and query strings.
	{"path/no path", "*://*.bank.example.com/*", "https://bank.example.com", true},
	{"path/query without path", "*://*.bank.example.com/*", "https://bank.example.com?session=1", true},
	{"path/query", "*://*.bank.example.com/*", "https://bank.example.com/a?b=c", true},
	{"path/fragment", "*://*.bank.example.com/*", "https://bank.example.com/#top", true},
	{"path/host in query only", "*://*.bank.example.com/*", "https://news.example.org/?ref=https://bank.example.com/", false},
	{"path/pattern without path", "*://crm.example.net", "https://crm.example.net/deals/42", true},
	{"path/prefix", "https://drive.example.com/shared", "https://drive.example.com/shared/doc?id=1", true},
	{"path/prefix other path", "https://drive.example.com/shared", "https://drive.example.com/mine", false},
	{"path/glob", "*://docs.example.com/*/edit", "https://docs.example.com/d/abc/edit", true},

	// Userinfo never moves the host.
	{"userinfo/on denied host", "*://*.bank.example.com/*", "https://user:pw@bank.example.com/", true},
	{"userinfo/denied host as userinfo", "*://*.bank.example.com/*", "https://bank.example.com@news.example.org/", false},

	// Case: scheme and host are case-insensitive; trailing root dot ignored.
	{"case/upper host", "*://*.bank.example.com/*", "https://WWW.BANK.EXAMPLE.COM/x", true},
	{"case/upper scheme", "https://crm.example.net/*", "HTTPS://crm.example.net/x", true},
	{"case/upper rule host", "*://CRM.Example.NET/*", "https://crm.example.net/x", true},
	{"case/trailing dot", "*://*.bank.example.com/*", "https://bank.example.com./x", true},

	// IDN: unicode rule vs punycode URL (how Chromium stores it) and back.
	{"idn/unicode rule punycode url", "*://*.bücher.example/*", "https://shop.xn--bcher-kva.example/", true},
	{"idn/punycode rule unicode url", "*://*.xn--bcher-kva.example/*", "https://bücher.example/", true},
	{"idn/unicode both", "*://bücher.example/*", "https://BÜCHER.example/x", true},

	// Scheme field.
	{"scheme/any scheme", "*://localhost/*", "ws://localhost:8080/socket", true},
	{"scheme/pinned scheme other", "http://localhost/*", "https://localhost/", false},
	{"scheme/internal", "chrome://*", "chrome://settings/passwords", true},
	{"scheme/ipv6 loopback", "*://[::1]/*", "http://[::1]:3000/", true},

	// Legacy rules (no "://") keep their raw-string semantics.
	{"legacy/substring", "bank.example.com", "https://www.bank.example.com/", true},
	{"legacy/glob", "*bank.example.com*", "https://bank.example.com/account", true},
	{"legacy/prefix glob", "about:*", "about:blank", true},
	{"legacy/file prefix", "file:*", "file:///Users/someone/notes.md", true},
	{"legacy/no match", "*bank.example.com*", "https://example.com/news", false},
}

func TestDenyRuleCases(t *testing.T) {
	t.Parallel()
	for _, c := range denyCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := mustNew(t, Layer{Scope: ScopeGlobal, Rules: Rules{Deny: []string{c.rule}}})
			if got := !f.Matches(c.url); got != c.denied {
				t.Errorf("deny %q vs %q: denied=%v, want %v", c.rule, c.url, got, c.denied)
			}
		})
	}
}

// allowOnlyCases: allow_only must not be satisfied by a host that only
// appears in the query, userinfo or a lookalike domain.
var allowOnlyCases = []struct {
	name    string
	rule    string
	url     string
	allowed bool
}{
	{"allow/host", "*://*.example.org/*", "https://news.example.org/a", true},
	{"allow/apex", "*://*.example.org/*", "https://example.org/", true},
	{"allow/host in query", "*://*.example.org/*", "https://tracker.example.net/?u=https://news.example.org/", false},
	{"allow/host as userinfo", "*://*.example.org/*", "https://news.example.org@tracker.example.net/", false},
	{"allow/lookalike", "*://*.example.org/*", "https://example.org.tracker.example.net/", false},
}

func TestAllowOnlyRuleCases(t *testing.T) {
	t.Parallel()
	for _, c := range allowOnlyCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := mustNew(t, Layer{Scope: ScopeGlobal, Rules: Rules{AllowOnly: []string{c.rule}}})
			if got := f.Matches(c.url); got != c.allowed {
				t.Errorf("allow_only %q vs %q: allowed=%v, want %v", c.rule, c.url, got, c.allowed)
			}
		})
	}
}

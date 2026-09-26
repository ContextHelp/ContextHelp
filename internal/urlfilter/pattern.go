package urlfilter

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// pattern is one compiled rule.
//
// Two forms exist, both matched field by field against the parsed URL,
// so ports, userinfo, query strings and letter case cannot move a URL in
// or out of a rule:
//
//   - URL form, any rule containing "://": scheme://host[:port][/path].
//   - Host form, anything else: a bare host pattern, the host part of the
//     URL form alone. It matches that host on any scheme, port and path.
//     Anything but a host (a port, userinfo, path, query, fragment or
//     whitespace) makes the rule invalid.
type pattern struct {
	raw string

	anyScheme bool
	scheme    string

	host hostMatcher

	// port is "" for any port, or the decimal port the URL must use
	// (explicitly or as its scheme default).
	port string

	// path is "" for any path. pathGlob marks a path containing `*`,
	// matched anchored; otherwise path is a prefix.
	path     string
	pathGlob bool
}

type hostKind int

const (
	hostAny       hostKind = iota // "*"
	hostExact                     // "crm.example.net"
	hostSubdomain                 // "*.example.net": the domain and its subdomains
	hostGlob                      // any other use of "*"
)

type hostMatcher struct {
	kind  hostKind
	value string
}

func compilePattern(raw string) (pattern, error) {
	i := strings.Index(raw, "://")
	if i < 0 {
		return compileBare(raw)
	}
	p := pattern{raw: raw}

	scheme := strings.ToLower(raw[:i])
	switch scheme {
	case "":
		return p, errors.New("empty scheme; use * for any scheme")
	case "*":
		p.anyScheme = true
	default:
		p.scheme = scheme
	}

	rest := raw[i+3:]
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority, path := rest[:end], rest[end:]
	if strings.Contains(authority, "@") {
		return p, errors.New("userinfo is not supported in rules")
	}

	host, port, err := splitHostPort(authority)
	if err != nil {
		return p, err
	}
	p.port = port
	if p.host, err = compileHost(host); err != nil {
		return p, err
	}

	p.path = path
	p.pathGlob = strings.Contains(path, "*")
	if path == "/*" || path == "*" {
		p.path, p.pathGlob = "", false
	}
	return p, nil
}

// compileBare compiles a host-form rule.
func compileBare(raw string) (pattern, error) {
	p := pattern{raw: raw, anyScheme: true}
	if raw == "*" {
		return p, errors.New("a bare * is not a host; use *://*/* to match every URL")
	}
	for _, r := range raw {
		if r < 0x80 && !isHostPatternRune(r) {
			return p, fmt.Errorf("%q is not allowed in a host pattern; use scheme://host[:port][/path] for ports and paths", r)
		}
	}
	h, err := compileHost(raw)
	if err != nil {
		return p, err
	}
	if h.value == "" {
		return p, errors.New("empty host")
	}
	p.host = h
	return p, nil
}

// isHostPatternRune reports whether an ASCII rune may appear in a bare
// host pattern. Non-ASCII runes are left to IDNA validation.
func isHostPatternRune(b rune) bool {
	switch {
	case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
		return true
	}
	return b == '.' || b == '-' || b == '_' || b == '*'
}

func splitHostPort(authority string) (host, port string, err error) {
	host = authority
	if strings.HasPrefix(authority, "[") {
		end := strings.Index(authority, "]")
		if end < 0 {
			return "", "", errors.New("missing ] in IPv6 host")
		}
		host = authority[1:end]
		tail := authority[end+1:]
		if tail != "" {
			if !strings.HasPrefix(tail, ":") {
				return "", "", fmt.Errorf("unexpected %q after IPv6 host", tail)
			}
			port = tail[1:]
		}
	} else if j := strings.LastIndex(authority, ":"); j >= 0 {
		host, port = authority[:j], authority[j+1:]
	}
	if port == "*" {
		port = ""
	} else if port != "" && strings.Trim(port, "0123456789") != "" {
		return "", "", fmt.Errorf("invalid port %q", port)
	}
	return host, port, nil
}

func compileHost(h string) (hostMatcher, error) {
	switch {
	case h == "*":
		return hostMatcher{kind: hostAny}, nil
	case strings.HasPrefix(h, "*.") && !strings.Contains(h[2:], "*"):
		d, err := normalizeHost(h[2:])
		if err != nil {
			return hostMatcher{}, err
		}
		return hostMatcher{kind: hostSubdomain, value: d}, nil
	case strings.Contains(h, "*"):
		if !isASCII(h) {
			return hostMatcher{}, errors.New("non-ASCII host with * is not supported; use the punycode form")
		}
		return hostMatcher{kind: hostGlob, value: strings.TrimSuffix(strings.ToLower(h), ".")}, nil
	default:
		d, err := normalizeHost(h)
		if err != nil {
			return hostMatcher{}, err
		}
		return hostMatcher{kind: hostExact, value: d}, nil
	}
}

func (m hostMatcher) match(host string) bool {
	switch m.kind {
	case hostAny:
		return true
	case hostExact:
		return host == m.value
	case hostSubdomain:
		return host == m.value || strings.HasSuffix(host, "."+m.value)
	default:
		return globAnchored(m.value, host)
	}
}

// normalizeHost lowercases, drops a trailing root dot and converts IDN
// labels to punycode, the form Chromium stores in history.
func normalizeHost(h string) (string, error) {
	h = strings.TrimSuffix(h, ".")
	if isASCII(h) || net.ParseIP(h) != nil {
		return strings.ToLower(h), nil
	}
	a, err := idna.Lookup.ToASCII(h)
	if err != nil {
		return "", fmt.Errorf("invalid internationalized host: %w", err)
	}
	return a, nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// target is a URL parsed once for matching against every rule.
type target struct {
	// authority is false for URLs without "//" (about:blank, mailto:...).
	// URL-form rules never match those.
	authority bool
	scheme    string
	host      string
	port      string
	path      string
}

var errUnparseable = errors.New("unparseable URL")

func parseTarget(raw string) (target, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return target{}, errUnparseable
	}
	host, err := normalizeHost(u.Hostname())
	if err != nil {
		return target{}, errUnparseable
	}
	t := target{
		authority: u.Opaque == "" && strings.HasPrefix(raw[len(u.Scheme)+1:], "//"),
		scheme:    u.Scheme,
		host:      host,
		port:      u.Port(),
		path:      u.Path,
	}
	if t.port == "" {
		t.port = defaultPorts[t.scheme]
	}
	if t.path == "" {
		t.path = "/"
	}
	if u.RawQuery != "" || u.ForceQuery {
		t.path += "?" + u.RawQuery
	}
	return t, nil
}

var defaultPorts = map[string]string{
	"http":  "80",
	"ws":    "80",
	"https": "443",
	"wss":   "443",
	"ftp":   "21",
}

func (p pattern) match(t target) bool {
	if !t.authority {
		return false
	}
	if !p.anyScheme && p.scheme != t.scheme {
		return false
	}
	if !p.host.match(t.host) {
		return false
	}
	if p.port != "" && p.port != t.port {
		return false
	}
	switch {
	case p.path == "":
		return true
	case p.pathGlob:
		return globAnchored(p.path, t.path)
	default:
		return strings.HasPrefix(t.path, p.path)
	}
}

// globAnchored matches s against pattern where `*` is any run of
// characters and every other byte is literal.
func globAnchored(pattern, s string) bool {
	segs := strings.Split(pattern, "*")
	if len(segs) == 1 {
		return pattern == s
	}
	first, last := segs[0], segs[len(segs)-1]
	if !strings.HasPrefix(s, first) || !strings.HasSuffix(s, last) ||
		len(s) < len(first)+len(last) {
		return false
	}
	mid := s[len(first) : len(s)-len(last)]
	for _, seg := range segs[1 : len(segs)-1] {
		i := strings.Index(mid, seg)
		if i < 0 {
			return false
		}
		mid = mid[i+len(seg):]
	}
	return true
}

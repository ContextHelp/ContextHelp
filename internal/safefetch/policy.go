// Package safefetch is the HTTP client for fetching URLs a user or a
// captured page supplied: captured pages, feeds, the links they carry
// and their redirects. It refuses to connect to private-network
// addresses so a capture cannot make dpkms reach the services beside it
// (SSRF).
//
// Enforcement sits in net.Dialer.Control, which runs for every socket
// just before connect, with the IP the socket is about to reach. The
// guard resolves the host name itself and dials each resolved IP
// literally, so the address checked is the address connected to: a DNS
// answer that changes between two lookups (rebinding) and every redirect
// hop (each one dials anew) are covered by the same check.
//
// Only fetches of user-supplied URLs go through this package. Calls to
// configured endpoints (providers, registries, federation peers, a
// dpkms instance's own API) keep their own clients and are unaffected.
package safefetch

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// ErrBlocked marks a fetch refused because its destination is a
// private-network address. Match it with errors.Is; the concrete error
// is a *BlockedError.
var ErrBlocked = errors.New("destination is a private-network address")

// ErrScheme marks a fetch refused because its URL is not http or https.
var ErrScheme = errors.New("only http and https URLs can be fetched")

// BlockedError names the refused address and the range it falls in.
type BlockedError struct {
	// Host is the host name the URL named, when it was not the address
	// itself.
	Host string
	// Addr is the refused address.
	Addr netip.Addr
	// Range names the blocked range Addr falls in, e.g. "link-local".
	Range string
}

func (e *BlockedError) Error() string {
	dest := e.Addr.String()
	if e.Host != "" && e.Host != dest {
		dest = fmt.Sprintf("%s (%s)", e.Host, e.Addr)
	}
	return fmt.Sprintf("fetch refused: %s is a %s address; "+
		"to allow it set fetch.allow_private, or list it under fetch.allowed_hosts or fetch.allowed_cidrs", dest, e.Range)
}

// Unwrap lets errors.Is match ErrBlocked.
func (e *BlockedError) Unwrap() error { return ErrBlocked }

// blockedRange is one address range a fetch may not reach.
type blockedRange struct {
	prefix netip.Prefix
	name   string
}

func mustRange(cidr, name string) blockedRange {
	return blockedRange{prefix: netip.MustParsePrefix(cidr), name: name}
}

// defaultBlocked lists the ranges refused unless the policy allows them.
// IPv6 forms that embed an IPv4 address (mapped, compatible, NAT64,
// 6to4) are reduced to that address first; see embeddedV4.
var defaultBlocked = []blockedRange{
	mustRange("0.0.0.0/8", "unspecified (this-network)"),
	mustRange("10.0.0.0/8", "private (RFC 1918)"),
	mustRange("100.64.0.0/10", "carrier-grade NAT (RFC 6598)"),
	mustRange("127.0.0.0/8", "loopback"),
	mustRange("169.254.0.0/16", "link-local"),
	mustRange("172.16.0.0/12", "private (RFC 1918)"),
	mustRange("192.168.0.0/16", "private (RFC 1918)"),
	mustRange("224.0.0.0/4", "multicast"),
	mustRange("240.0.0.0/4", "reserved"),
	mustRange("::/128", "unspecified"),
	mustRange("::1/128", "loopback"),
	mustRange("fc00::/7", "unique-local (ULA)"),
	mustRange("fe80::/10", "link-local"),
	mustRange("fec0::/10", "site-local"),
	mustRange("ff00::/8", "multicast"),
}

var (
	ipv4Compatible = netip.MustParsePrefix("::/96")
	nat64          = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour      = netip.MustParsePrefix("2002::/16")
)

// embeddedV4 returns the IPv4 address an IPv6 address carries in its
// IPv4-mapped (::ffff:a.b.c.d), IPv4-compatible (::a.b.c.d), NAT64
// (64:ff9b::a.b.c.d) or 6to4 (2002:aabb:ccdd::) form, so ::ffff:127.0.0.1
// is judged as 127.0.0.1. ok is false for any other address.
func embeddedV4(a netip.Addr) (v4 netip.Addr, ok bool) {
	if a.Is4In6() {
		return a.Unmap(), true
	}
	if !a.Is6() {
		return netip.Addr{}, false
	}
	b := a.As16()
	switch {
	case ipv4Compatible.Contains(a) && a != netip.IPv6Unspecified() && a != netip.IPv6Loopback():
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case nat64.Contains(a):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case sixToFour.Contains(a):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	return netip.Addr{}, false
}

// classify returns the name of the blocked range a falls in, or "" when
// a is a public address.
func classify(ranges []blockedRange, a netip.Addr) string {
	a = a.WithZone("")
	if v4, ok := embeddedV4(a); ok {
		a = v4
	}
	for _, r := range ranges {
		if r.prefix.Contains(a) {
			return r.name
		}
	}
	return ""
}

// Policy decides which addresses a fetch may reach. The zero value is
// the default: every blocked range refused, nothing allowlisted.
type Policy struct {
	// AllowPrivate turns the private-network block off entirely
	// (fetch.allow_private).
	AllowPrivate bool
	// AllowedHosts are host names a fetch may reach whatever they
	// resolve to (fetch.allowed_hosts). Matched case-insensitively and
	// exactly: "wiki.corp.example" does not cover "corp.example".
	AllowedHosts []string
	// AllowedCIDRs are blocked addresses a fetch may reach anyway
	// (fetch.allowed_cidrs), e.g. 10.20.0.0/16 for an intranet.
	AllowedCIDRs []netip.Prefix
}

// ParsePolicy builds a Policy from the fetch.* config values, rejecting
// a malformed host or CIDR entry. key prefixes error messages, e.g.
// "fetch".
func ParsePolicy(key string, allowPrivate bool, hosts, cidrs []string) (Policy, error) {
	p := Policy{AllowPrivate: allowPrivate}
	for i, h := range hosts {
		norm, err := normalizeHost(h)
		if err != nil {
			return Policy{}, fmt.Errorf("%s.allowed_hosts[%d]: %w", key, i, err)
		}
		p.AllowedHosts = append(p.AllowedHosts, norm)
	}
	for i, c := range cidrs {
		pfx, err := parseCIDR(c)
		if err != nil {
			return Policy{}, fmt.Errorf("%s.allowed_cidrs[%d]: %w", key, i, err)
		}
		p.AllowedCIDRs = append(p.AllowedCIDRs, pfx)
	}
	return p, nil
}

func normalizeHost(h string) (string, error) {
	h = strings.TrimSpace(h)
	if h == "" {
		return "", errors.New("empty entry (want a host name such as wiki.corp.example)")
	}
	if strings.ContainsAny(h, " \t\r\n/\\*@?#,:") {
		return "", fmt.Errorf("invalid entry %q: want a bare host name, without scheme, port, path or wildcard", h)
	}
	return strings.ToLower(strings.TrimSuffix(h, ".")), nil
}

// parseCIDR accepts a prefix ("10.20.0.0/16") or a single address
// ("10.20.0.5", taken as /32 or /128).
func parseCIDR(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid entry %q: want a CIDR such as 10.20.0.0/16 or an address", s)
		}
		return netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid entry %q: want a CIDR such as 10.20.0.0/16 or an address", s)
	}
	return p.Masked(), nil
}

// hostAllowed reports whether host is exempt from the block: either the
// whole block is off or host is listed in AllowedHosts.
func (p Policy) hostAllowed(host string) bool {
	if p.AllowPrivate {
		return true
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range p.AllowedHosts {
		if h == host {
			return true
		}
	}
	return false
}

// check returns a *BlockedError when a is in a blocked range the policy
// does not allow, nil otherwise.
func (p Policy) check(ranges []blockedRange, host string, a netip.Addr) error {
	if p.AllowPrivate {
		return nil
	}
	name := classify(ranges, a)
	if name == "" {
		return nil
	}
	plain := a.WithZone("")
	for _, pfx := range p.AllowedCIDRs {
		if pfx.Contains(plain) || pfx.Contains(plain.Unmap()) {
			return nil
		}
	}
	return &BlockedError{Host: host, Addr: plain, Range: name}
}

// Check reports whether the policy lets a fetch reach a: nil, or a
// *BlockedError wrapping ErrBlocked.
func (p Policy) Check(a netip.Addr) error {
	return p.check(defaultBlocked, "", a)
}

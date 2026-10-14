package safefetch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"hop.top/kit/go/core/netpolicy"
)

// DefaultMaxRedirects caps how many redirects one fetch follows.
const DefaultMaxRedirects = 10

// Resolver looks up the addresses of a host name. *net.Resolver
// satisfies it; tests substitute a fixed table.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Options configures a Guard.
type Options struct {
	// Policy decides which addresses are reachable. The zero value
	// blocks every private-network range.
	Policy Policy
	// Resolver resolves host names. Nil uses net.DefaultResolver.
	Resolver Resolver
	// MaxRedirects caps the redirects a Client follows. Zero uses
	// DefaultMaxRedirects.
	MaxRedirects int
	// DialTimeout bounds each connection attempt. Zero uses 30s.
	DialTimeout time.Duration
}

// Guard dials, and builds HTTP clients, under a Policy.
type Guard struct {
	policy       Policy
	resolver     Resolver
	maxRedirects int
	dialTimeout  time.Duration
	// ranges is the blocked-range table; defaultBlocked outside tests.
	ranges []blockedRange
}

// New returns a Guard for opts.
func New(opts Options) *Guard {
	g := &Guard{
		policy:       opts.Policy,
		resolver:     opts.Resolver,
		maxRedirects: opts.MaxRedirects,
		dialTimeout:  opts.DialTimeout,
		ranges:       defaultBlocked,
	}
	if g.resolver == nil {
		g.resolver = net.DefaultResolver
	}
	if g.maxRedirects <= 0 {
		g.maxRedirects = DefaultMaxRedirects
	}
	if g.dialTimeout <= 0 {
		g.dialTimeout = 30 * time.Second
	}
	return g
}

// Default returns a Guard with the default policy: every private range
// blocked, nothing allowlisted.
func Default() *Guard { return New(Options{}) }

// control is the net.Dialer Control hook: it runs on every socket just
// before connect, with the literal IP:port the socket is about to reach,
// and refuses a blocked address. This is the enforcement point; the
// resolution in DialContext only decides which IPs to try.
func (g *Guard) control(host string) func(network, address string, _ syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		ipStr, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("safefetch: dial %s %s: %w", network, address, err)
		}
		ip, err := netip.ParseAddr(ipStr)
		if err != nil {
			return fmt.Errorf("safefetch: dial %s %s: %w", network, address, err)
		}
		return g.policy.check(g.ranges, host, ip)
	}
}

// DialContext resolves addr's host and connects to the first resolved
// address that accepts, refusing any address the policy blocks. A host
// listed in Policy.AllowedHosts is dialed without the block.
func (g *Guard) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("safefetch: dial %s: %w", addr, err)
	}
	d := &net.Dialer{Timeout: g.dialTimeout, KeepAlive: 30 * time.Second}
	if !g.policy.hostAllowed(host) {
		d.Control = g.control(host)
	}

	ips, err := g.lookup(ctx, network, host)
	if err != nil {
		return nil, err
	}
	var firstErr error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, firstErr
}

// lookup returns host's addresses for network: host itself when it is a
// literal IP, else the resolver's answer.
func (g *Guard) lookup(ctx context.Context, network, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	ipNet := "ip"
	switch network {
	case "tcp4", "udp4":
		ipNet = "ip4"
	case "tcp6", "udp6":
		ipNet = "ip6"
	}
	ips, err := g.resolver.LookupNetIP(ctx, ipNet, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return ips, nil
}

// Transport returns an http.Transport that dials through the guard,
// tuned like http.DefaultTransport (which kit may have wrapped, so it is
// not cloned). It ignores HTTP(S)_PROXY: through a proxy the connection
// the guard checks is the proxy's, not the destination's.
func (g *Guard) Transport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           g.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// Client returns an *http.Client that fetches through the guard: http and
// https only, at most MaxRedirects redirects, each hop dialed (and so
// checked) anew. The transport also honours kit's --offline marker.
func (g *Guard) Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     schemeGuard{netpolicy.Guard(g.Transport())},
		CheckRedirect: g.checkRedirect,
	}
}

func (g *Guard) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > g.maxRedirects {
		return fmt.Errorf("safefetch: stopped after %d redirects", g.maxRedirects)
	}
	if !httpScheme(req.URL.Scheme) {
		return fmt.Errorf("safefetch: redirect to %s: %w", req.URL.Redacted(), ErrScheme)
	}
	return nil
}

func httpScheme(s string) bool {
	s = strings.ToLower(s)
	return s == "http" || s == "https"
}

// schemeGuard refuses any request whose URL is not http or https.
type schemeGuard struct{ base http.RoundTripper }

func (s schemeGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	if !httpScheme(req.URL.Scheme) {
		return nil, fmt.Errorf("safefetch: %s: %w", req.URL.Redacted(), ErrScheme)
	}
	return s.base.RoundTrip(req)
}

// CheckURL vets rawURL for a fetcher that cannot dial through the guard,
// such as a browser: the scheme must be http or https and every address
// the host resolves to must pass the policy. Unlike the dialer it cannot
// see the browser's own lookups or redirects, so it narrows the hole
// rather than closing it.
func (g *Guard) CheckURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("safefetch: parse URL: %w", err)
	}
	if !httpScheme(u.Scheme) {
		return fmt.Errorf("safefetch: %s: %w", u.Redacted(), ErrScheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("safefetch: %s: missing host", u.Redacted())
	}
	if g.policy.hostAllowed(host) {
		return nil
	}
	ips, err := g.lookup(ctx, "tcp", host)
	if err != nil {
		return fmt.Errorf("safefetch: resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if err := g.policy.check(g.ranges, host, ip); err != nil {
			return err
		}
	}
	return nil
}

// IsBlocked reports whether err is a refusal by this package (a blocked
// address or a non-http scheme): a permanent failure, not worth a retry.
func IsBlocked(err error) bool {
	return errors.Is(err, ErrBlocked) || errors.Is(err, ErrScheme)
}

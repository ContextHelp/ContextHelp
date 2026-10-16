// Package steps provides pipeline step implementations.
// client_bridge.go — shared, configurable HTTP client abstraction for fetch steps.
package steps

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
)

const (
	// DefaultUserAgent is the UA sent unless overridden.
	DefaultUserAgent = "ContextHelp/1.0 (+https://context.help)"
	// DefaultTimeout is applied when no timeout is configured.
	DefaultTimeout = 30 * time.Second
)

// DomainCredential injects an Authorization header for requests matching a host.
type DomainCredential struct {
	// Host is the exact hostname (e.g. "api.github.com").
	Host string
	// Header is the header name, typically "Authorization".
	Header string
	// Value is the raw header value (e.g. "Bearer <token>").
	Value string
}

// ClientBridgeOption configures a ClientBridge.
type ClientBridgeOption func(*ClientBridge)

// ClientBridge is a shared, configurable HTTP client wrapper reusable across fetch steps.
// It supports timeout, custom user-agent, extra headers, per-domain credential injection,
// and an optional cookie jar.
//
// Its default client fetches through a safefetch.Guard: the URLs it
// fetches come from captures, so private-network addresses are refused
// (see WithBridgeFetchGuard). Only an injected client bypasses that.
type ClientBridge struct {
	client       *http.Client
	guard        *safefetch.Guard
	timeout      time.Duration
	jar          http.CookieJar
	userAgent    string
	extraHeaders map[string]string
	domainCreds  []DomainCredential
}

// NewClientBridge constructs a ClientBridge with sensible defaults.
func NewClientBridge(opts ...ClientBridgeOption) *ClientBridge {
	cb := &ClientBridge{
		userAgent:    DefaultUserAgent,
		extraHeaders: make(map[string]string),
		timeout:      DefaultTimeout,
	}
	for _, opt := range opts {
		opt(cb)
	}
	if cb.client == nil {
		guard := cb.guard
		if guard == nil {
			guard = safefetch.Default()
		}
		cb.client = guard.Client(cb.timeout)
	}
	if cb.jar != nil {
		cb.client.Jar = cb.jar
	}
	return cb
}

// WithBridgeHTTPClient replaces the underlying *http.Client (e.g. for tests).
// The injected client is used as is: it bypasses the fetch guard.
func WithBridgeHTTPClient(c *http.Client) ClientBridgeOption {
	return func(cb *ClientBridge) { cb.client = c }
}

// WithBridgeFetchGuard sets the guard the default client dials through,
// built from the fetch.* config (config.FetchGuard). Without it the
// default policy applies: every private-network address refused.
// Ignored when WithBridgeHTTPClient is also used.
func WithBridgeFetchGuard(g *safefetch.Guard) ClientBridgeOption {
	return func(cb *ClientBridge) { cb.guard = g }
}

// WithBridgeTimeout sets the request timeout on the default client.
// Ignored when WithBridgeHTTPClient is also used.
func WithBridgeTimeout(d time.Duration) ClientBridgeOption {
	return func(cb *ClientBridge) { cb.timeout = d }
}

// WithBridgeUserAgent overrides the default User-Agent header.
func WithBridgeUserAgent(ua string) ClientBridgeOption {
	return func(cb *ClientBridge) { cb.userAgent = ua }
}

// WithBridgeHeader adds a static header applied to every request.
func WithBridgeHeader(key, value string) ClientBridgeOption {
	return func(cb *ClientBridge) { cb.extraHeaders[key] = value }
}

// WithBridgeCookieJar attaches a cookie jar to the underlying client.
// A nil jar argument creates a new public-suffix-aware jar.
func WithBridgeCookieJar(jar http.CookieJar) ClientBridgeOption {
	return func(cb *ClientBridge) {
		if jar == nil {
			jar, _ = cookiejar.New(nil)
		}
		cb.jar = jar
	}
}

// WithBridgeDomainCredential registers per-domain auth header injection.
// Multiple calls accumulate credentials.
func WithBridgeDomainCredential(dc DomainCredential) ClientBridgeOption {
	return func(cb *ClientBridge) { cb.domainCreds = append(cb.domainCreds, dc) }
}

// Do performs the request, applying User-Agent, extra headers, and domain credentials.
func (cb *ClientBridge) Do(req *http.Request) (*http.Response, error) {
	// User-Agent — only set if not already present.
	if req.Header.Get("User-Agent") == "" && cb.userAgent != "" {
		req.Header.Set("User-Agent", cb.userAgent)
	}
	// Static extra headers.
	for k, v := range cb.extraHeaders {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	// Per-domain credential injection.
	cb.injectDomainCreds(req)

	return cb.client.Do(req) // #nosec G704 -- default client dials through safefetch, which refuses private-network addresses
}

// HTTPClient returns the underlying *http.Client for callers that need it directly
// (e.g. httptest server integration via srv.Client()).
func (cb *ClientBridge) HTTPClient() *http.Client {
	return cb.client
}

// injectDomainCreds applies the first matching domain credential to the request.
func (cb *ClientBridge) injectDomainCreds(req *http.Request) {
	if len(cb.domainCreds) == 0 {
		return
	}
	host := hostOnly(req.URL)
	for _, dc := range cb.domainCreds {
		if strings.EqualFold(dc.Host, host) {
			header := dc.Header
			if header == "" {
				header = "Authorization"
			}
			if req.Header.Get(header) == "" {
				req.Header.Set(header, dc.Value)
			}
			return
		}
	}
}

// hostOnly strips the port from a URL's host for matching.
func hostOnly(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := u.Hostname()
	return strings.ToLower(host)
}

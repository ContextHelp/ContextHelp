// Package steps provides pipeline step implementations.
// client_bridge.go — shared, configurable HTTP client abstraction for fetch steps.
package steps

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
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
type ClientBridge struct {
	client      *http.Client
	userAgent   string
	extraHeaders map[string]string
	domainCreds []DomainCredential
}

// NewClientBridge constructs a ClientBridge with sensible defaults.
func NewClientBridge(opts ...ClientBridgeOption) *ClientBridge {
	cb := &ClientBridge{
		userAgent:    DefaultUserAgent,
		extraHeaders: make(map[string]string),
	}
	// Apply options before building the client so WithHTTPClient can override.
	for _, opt := range opts {
		opt(cb)
	}
	// Build a default client if none was injected.
	if cb.client == nil {
		cb.client = &http.Client{Timeout: DefaultTimeout}
	}
	return cb
}

// WithBridgeHTTPClient replaces the underlying *http.Client (e.g. for tests).
func WithBridgeHTTPClient(c *http.Client) ClientBridgeOption {
	return func(cb *ClientBridge) { cb.client = c }
}

// WithBridgeTimeout sets the request timeout on the underlying client.
// Ignored when WithBridgeHTTPClient is also used.
func WithBridgeTimeout(d time.Duration) ClientBridgeOption {
	return func(cb *ClientBridge) {
		if cb.client == nil {
			cb.client = &http.Client{Timeout: d}
		} else {
			cb.client.Timeout = d
		}
	}
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
		if cb.client == nil {
			cb.client = &http.Client{Timeout: DefaultTimeout, Jar: jar}
		} else {
			cb.client.Jar = jar
		}
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

	return cb.client.Do(req) // #nosec G704 -- request URL is controlled by pipeline configuration
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

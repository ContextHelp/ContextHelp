// Package dpkmsclient is ctxt's HTTP client for dpkms: the one way a ctxt
// command reaches an instance (ADR-077 §1).
//
// A Client talks to exactly one Endpoint. Picking that endpoint is the
// resolver's job; the client never walks to another instance. Every
// request carries the endpoint's bearer token and speaks JSON, and every
// failure comes back as a kit *output.Error whose exit code follows
// ADR-077 §2:
//
//	nothing answered (dial, DNS, TLS)   PREREQUISITE  70
//	401, 403                            UNAUTHORIZED   5
//	404                                 NOT_FOUND      3
//	409 (a policy veto included)        CONFLICT       4
//	400, 422                            USAGE          2
//	429                                 RATE_LIMITED  64
//	5xx, failure after connecting       TRANSIENT      6
//	any other status                    GENERIC        1
//
// Each request is sent once. A 401 or 403 in particular ends it: it is
// never retried, here or against another instance.
package dpkmsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"hop.top/kit/go/console/output"
)

// DefaultTimeout bounds a whole request, connection through response body,
// unless WithTimeout says otherwise.
const DefaultTimeout = 30 * time.Second

// Endpoint is one dpkms instance: a base URL and the bearer token sent
// with every request to it ("" sends none).
type Endpoint struct {
	URL   string
	Token string
}

// Client sends requests to one dpkms endpoint.
type Client struct {
	base  string
	token string
	hc    *http.Client
}

type options struct {
	timeout   time.Duration
	transport http.RoundTripper
}

// Option configures a Client.
type Option func(*options)

// WithTimeout bounds each request; 0 means no bound.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithTransport replaces http.DefaultTransport as the round tripper.
func WithTransport(rt http.RoundTripper) Option {
	return func(o *options) { o.transport = rt }
}

// New returns a Client for ep. A URL without an http or https scheme and
// a host is a USAGE error: it came from --server or from config.
func New(ep Endpoint, opts ...Option) (*Client, error) {
	u, err := url.Parse(ep.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		e := output.UsageError(fmt.Sprintf("invalid dpkms server URL %q: want http(s)://host[:port]", ep.URL))
		e.SuggestedFix = "check --server, server.url and server.urls in the ctxt config"
		return nil, e
	}
	o := options{timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(&o)
	}
	return &Client{
		base:  strings.TrimRight(ep.URL, "/"),
		token: ep.Token,
		// A nil Transport resolves http.DefaultTransport per request, so
		// wrappers installed on it later (test guards, kit's offline
		// policy) still apply.
		hc: &http.Client{Timeout: o.timeout, Transport: o.transport},
	}, nil
}

// URL returns the endpoint's base URL, without a trailing slash. It never
// carries the token, so it is safe to print.
func (c *Client) URL() string { return c.base }

// Request is one call to the dpkms API.
type Request struct {
	// Method is the HTTP method.
	Method string
	// Path is the absolute path under the base URL, e.g. "/api/v1/search".
	Path string
	// Query is encoded onto the URL when non-empty.
	Query url.Values
	// Body is sent as JSON when non-nil.
	Body any
	// Header adds request headers, e.g. X-Ctxt-Note. It cannot replace
	// Authorization, Content-Type or Accept.
	Header http.Header
}

// Get issues GET path?query and decodes a 2xx JSON body into out.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: query}, out)
}

// Post issues POST path with body as JSON and decodes a 2xx JSON body into
// out.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, Request{Method: http.MethodPost, Path: path, Body: body}, out)
}

// Do sends r once. On a 2xx it decodes the JSON body into out, when out
// is non-nil and the body is non-empty. Every failure is a kit
// *output.Error (see the package doc); a non-2xx answer also retains a
// *RemoteError for errors.As. A canceled ctx returns the transport error
// unclassified, so errors.Is(err, context.Canceled) holds.
func (c *Client) Do(ctx context.Context, r Request, out any) error {
	req, err := c.newRequest(ctx, r)
	if err != nil {
		return err
	}
	var connected atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
	}))

	resp, err := c.hc.Do(req) // #nosec G107,G704 -- operator-configured dpkms endpoint
	if err != nil {
		return c.transportError(ctx, err, connected.Load())
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return c.statusError(resp.StatusCode, body)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.transportError(ctx, err, true)
	}
	if out == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return output.WrapError(
			fmt.Errorf("decode %s %s response from dpkms at %s: %w", r.Method, r.Path, c.base, err),
			output.CodeGeneric, output.ExitGeneric)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, r Request) (*http.Request, error) {
	target := c.base + r.Path
	if len(r.Query) > 0 {
		target += "?" + r.Query.Encode()
	}
	var body io.Reader
	if r.Body != nil {
		raw, err := json.Marshal(r.Body)
		if err != nil {
			return nil, fmt.Errorf("encode %s %s request: %w", r.Method, r.Path, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, body)
	if err != nil {
		return nil, fmt.Errorf("build %s %s request: %w", r.Method, r.Path, err)
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Del("Authorization")
	req.Header.Set("Accept", "application/json")
	if r.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Del("Content-Type")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

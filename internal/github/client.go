package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

const (
	apiVersion      = "2022-11-28"
	maxBodyBytes    = 16 << 20
	defaultRetries  = 3
	defaultBackoff  = 500 * time.Millisecond
	secondaryWait   = 60 * time.Second // GitHub: wait at least a minute when no Retry-After
	etagCacheMaxLen = 256
)

// Client is a GitHub REST client: token per owner, primary and secondary
// rate-limit handling, ETag conditional requests, typed errors. It is safe
// for concurrent use.
type Client struct {
	hc         *http.Client
	baseURL    string
	tokens     TokenSource
	userAgent  string
	maxRetries int
	maxWait    time.Duration
	backoff    time.Duration

	// Test seams.
	sleep func(ctx context.Context, d time.Duration) error
	now   func() time.Time

	cache *etagCache
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client (default: 30s timeout).
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.hc = hc } }

// WithBaseURL sets the API root (default https://api.github.com).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithTokens sets the token source (default: no token, anonymous).
func WithTokens(t TokenSource) Option { return func(c *Client) { c.tokens = t } }

// WithMaxWait caps one rate-limit sleep; a longer wait returns a
// *RateLimitError instead (default 30s). Zero never sleeps.
func WithMaxWait(d time.Duration) Option { return func(c *Client) { c.maxWait = d } }

// WithMaxRetries sets how many times a rate-limited or 5xx request is
// retried (default 3).
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// withSleep and withClock are test seams.
func withSleep(f func(context.Context, time.Duration) error) Option {
	return func(c *Client) { c.sleep = f }
}
func withClock(f func() time.Time) Option { return func(c *Client) { c.now = f } }

// NewClient builds a Client.
func NewClient(opts ...Option) *Client {
	c := &Client{
		hc:         &http.Client{Timeout: 30 * time.Second},
		baseURL:    config.DefaultGitHubAPIURL,
		tokens:     StaticToken(""),
		userAgent:  "ctxt-github",
		maxRetries: defaultRetries,
		maxWait:    config.DefaultGitHubMaxWait,
		backoff:    defaultBackoff,
		sleep:      sleepCtx,
		now:        time.Now,
		cache:      newETagCache(etagCacheMaxLen),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// NewClientFromConfig builds a Client from cfg, reading token secrets
// through get. A nil get falls back to the process environment (the
// secret name is the variable name).
func NewClientFromConfig(cfg config.GitHubConfig, get SecretGetter, opts ...Option) *Client {
	if get == nil {
		get = func(_ context.Context, name string) (string, error) { return os.Getenv(name), nil }
	}
	base := []Option{WithTokens(NewConfigTokens(cfg, get))}
	if cfg.APIURL != "" {
		base = append(base, WithBaseURL(cfg.APIURL))
	}
	if cfg.MaxWait != 0 {
		base = append(base, WithMaxWait(cfg.MaxWait))
	}
	return NewClient(append(base, opts...)...)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// get performs GET path (relative to the API root, with query) on behalf
// of owner and returns the body. accept overrides the Accept header when
// non-empty. 304 answers are served from the ETag cache.
func (c *Client) get(ctx context.Context, owner, path, accept string) ([]byte, error) {
	target := c.baseURL + path
	token, err := c.tokens.Token(ctx, owner)
	if err != nil {
		return nil, err
	}
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	cacheKey := accept + "\x00" + token + "\x00" + target

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("X-GitHub-Api-Version", apiVersion)
		req.Header.Set("User-Agent", c.userAgent)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		cached, haveCached := c.cache.get(cacheKey)
		if haveCached {
			req.Header.Set("If-None-Match", cached.etag)
		}

		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("github: GET %s: %w", target, err)
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		resp.Body.Close()
		if rerr != nil {
			return nil, fmt.Errorf("github: read %s: %w", target, rerr)
		}

		switch {
		case resp.StatusCode == http.StatusNotModified && haveCached:
			return cached.body, nil
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if etag := resp.Header.Get("ETag"); etag != "" {
				c.cache.put(cacheKey, etag, body)
			}
			return body, nil
		}

		ae := &APIError{Status: resp.StatusCode, Method: http.MethodGet, URL: target, Message: messageOf(body)}
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			ae.err = ErrUnauthorized
			return nil, ae
		case resp.StatusCode == http.StatusNotFound:
			ae.err = ErrNotFound
			return nil, ae
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
			rl := c.classifyLimit(resp, ae)
			if rl == nil {
				ae.err = ErrForbidden
				return nil, ae
			}
			if attempt >= c.maxRetries || rl.RetryAfter > c.maxWait {
				return nil, rl
			}
			slog.Debug("github: rate limited, waiting", "kind", rl.Kind, "wait", rl.RetryAfter, "url", target)
			if err := c.sleep(ctx, rl.RetryAfter); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode >= 500:
			ae.err = ErrServer
			if attempt >= c.maxRetries {
				return nil, ae
			}
			if err := c.sleep(ctx, c.backoff<<attempt); err != nil {
				return nil, err
			}
			continue
		default:
			ae.err = fmt.Errorf("github: unexpected status %d", resp.StatusCode)
			return nil, ae
		}
	}
}

// classifyLimit returns the rate-limit error for a 403/429 response, or
// nil when the response is a plain forbidden.
//
//	secondary: Retry-After present, or the message names the secondary
//	           (abuse) limit; waits Retry-After, else a minute.
//	primary:   X-RateLimit-Remaining is 0; waits until X-RateLimit-Reset.
func (c *Client) classifyLimit(resp *http.Response, ae *APIError) *RateLimitError {
	h := resp.Header
	if ra := strings.TrimSpace(h.Get("Retry-After")); ra != "" {
		wait := secondaryWait
		if secs, err := strconv.Atoi(ra); err == nil && secs >= 0 {
			wait = time.Duration(secs) * time.Second
		} else if t, err := http.ParseTime(ra); err == nil {
			wait = max(t.Sub(c.now()), 0)
		}
		return &RateLimitError{APIError: *ae, Kind: LimitSecondary, RetryAfter: wait}
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		rl := &RateLimitError{APIError: *ae, Kind: LimitPrimary}
		if ts, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			rl.Reset = time.Unix(ts, 0)
			rl.RetryAfter = max(rl.Reset.Sub(c.now()), 0) + time.Second // clock skew margin
		} else {
			rl.RetryAfter = secondaryWait
		}
		return rl
	}
	lower := strings.ToLower(ae.Message)
	if strings.Contains(lower, "secondary rate limit") || strings.Contains(lower, "abuse detection") {
		return &RateLimitError{APIError: *ae, Kind: LimitSecondary, RetryAfter: secondaryWait}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{APIError: *ae, Kind: LimitSecondary, RetryAfter: secondaryWait}
	}
	return nil
}

func messageOf(body []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &m) == nil {
		return m.Message
	}
	return ""
}

// getJSON GETs path and decodes the JSON body into v.
func (c *Client) getJSON(ctx context.Context, owner, path string, v any) error {
	body, err := c.get(ctx, owner, path, "")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("github: decode %s: %w", path, err)
	}
	return nil
}

func escapeSegments(parts ...string) string {
	esc := make([]string, len(parts))
	for i, p := range parts {
		esc[i] = url.PathEscape(p)
	}
	return strings.Join(esc, "/")
}

// etagCache keeps the last body per request, bounded: when full it drops
// everything (cheap, and entries rebuild on the next 200).
type etagCache struct {
	mu  sync.Mutex
	max int
	m   map[string]cachedBody
}

type cachedBody struct {
	etag string
	body []byte
}

func newETagCache(max int) *etagCache { return &etagCache{max: max, m: map[string]cachedBody{}} }

func (c *etagCache) get(k string) (cachedBody, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *etagCache) put(k, etag string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.max {
		c.m = map[string]cachedBody{}
	}
	c.m[k] = cachedBody{etag: etag, body: body}
}

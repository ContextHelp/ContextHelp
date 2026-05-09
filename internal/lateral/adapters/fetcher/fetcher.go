// Package fetcher wires daemon-side web fetchers into the lateral
// strategy contracts.
//
// Two strategy interfaces share a single concrete client:
//
//   - jit.Fetcher: Fetch(ctx, url) (body string, err error)
//   - github.Fetcher: Get(ctx, url) (FetchResult{Body []byte,
//     Status int, ContentType string}, err error)
//
// Both are satisfied by HTTPFetcher (net/http-backed, the default) or
// IBRFetcher (shells out to the ibr CLI for cookie-aware browsing).
//
// # Variant selection
//
// The daemon's config picks which one. HTTPFetcher is the v1 default
// because:
//
//   - It needs no external binary (works in CI, container builds,
//     test environments).
//   - The strategies that consume it (github HTTP probes, jit sub-path
//     fetches) treat the body as opaque text — there's no DOM-aware
//     extraction in the wired probe set.
//
// IBRFetcher is the production option for cookie-protected pages
// (LinkedIn, Substack, X). It requires the ibr CLI on $PATH and adds
// per-call latency (~1s for warm daemon, multiple seconds otherwise);
// the daemon picks it explicitly when operators turn on
// `lateral.fetcher.kind = ibr`.
package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// HTTPClient is the subset of *http.Client this adapter calls.
// Defining the interface here lets tests inject a recorder without
// pulling in httptest.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPFetcher fetches via net/http.
type HTTPFetcher struct {
	client HTTPClient
	// userAgent is sent on every request. Default "ctxt-lateral/1.0"
	// — distinct enough that operators triaging server logs can spot
	// it. Override via Options.UserAgent.
	userAgent string
}

// Options configures HTTPFetcher.
type Options struct {
	// Client overrides the default *http.Client. Useful for tests
	// (httptest) and for production deployments that want a tuned
	// transport (proxy, custom timeout, OAuth wrapping).
	Client HTTPClient

	// Timeout caps a single Get/Fetch call. Zero falls back to 30s.
	// Ignored when Client is non-nil — the caller controls the
	// transport's timeouts.
	Timeout time.Duration

	// UserAgent overrides the User-Agent header. Empty falls back to
	// "ctxt-lateral/1.0".
	UserAgent string
}

const defaultUserAgent = "ctxt-lateral/1.0"
const defaultTimeout = 30 * time.Second

// NewHTTP wires an HTTPFetcher. Pass Options{} to use defaults.
func NewHTTP(opts Options) *HTTPFetcher {
	c := opts.Client
	if c == nil {
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = defaultTimeout
		}
		c = &http.Client{Timeout: timeout}
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	return &HTTPFetcher{client: c, userAgent: ua}
}

// jit.Fetcher contract.
var _ jit.Fetcher = (*HTTPFetcher)(nil)

// Fetch satisfies jit.Fetcher.Fetch. Returns the body as string and
// any non-2xx status as an error containing the status code so JIT's
// pipeline can route the failure.
func (h *HTTPFetcher) Fetch(ctx context.Context, urlStr string) (string, error) {
	res, err := h.do(ctx, urlStr)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("fetcher.Fetch read body: %w", err)
	}
	if res.StatusCode >= 400 {
		return string(body), fmt.Errorf("fetcher.Fetch %s: status %d", urlStr, res.StatusCode)
	}
	return string(body), nil
}

// github.Fetcher contract.
var _ github.Fetcher = (*HTTPFetcher)(nil)

// Get satisfies github.Fetcher.Get. Returns the full FetchResult
// (body bytes + status + content-type) so the github strategy's HTTP
// probes can inspect the headers. Status >= 400 is NOT an error here
// — the github strategy explicitly inspects Status (rate-limit handling
// keys on 429/403 + headers). Only transport-level failures error.
func (h *HTTPFetcher) Get(ctx context.Context, urlStr string) (github.FetchResult, error) {
	res, err := h.do(ctx, urlStr)
	if err != nil {
		return github.FetchResult{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return github.FetchResult{}, fmt.Errorf("fetcher.Get read body: %w", err)
	}
	return github.FetchResult{
		Body:        body,
		Status:      res.StatusCode,
		ContentType: res.Header.Get("Content-Type"),
	}, nil
}

func (h *HTTPFetcher) do(ctx context.Context, urlStr string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("fetcher build request: %w", err)
	}
	req.Header.Set("User-Agent", h.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/json;q=0.9,*/*;q=0.5")
	res, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetcher.Do %s: %w", urlStr, err)
	}
	return res, nil
}

// IBROptions configures an IBRFetcher.
type IBROptions struct {
	// Binary is the path to the ibr executable. Empty defaults to
	// "ibr" — looked up via $PATH.
	Binary string

	// Timeout caps each shell-out invocation. Zero falls back to
	// 60s (ibr cold-starts can take >5s; warm daemon mode is faster).
	Timeout time.Duration

	// ExtraArgs are appended after `snap <url>`. Use to thread flags
	// like --cookies, --mode aria, etc. without forking the adapter.
	ExtraArgs []string
}

// IBRFetcher shells out to the ibr CLI for cookie-aware browsing.
// Treats the captured DOM-JSON as the body and lets the consuming
// strategy (jit) parse what it needs. github strategies that depend
// on JSON API responses should keep using HTTPFetcher; ibr is for
// the JIT sub-path fetcher and any future shape-keyed adapters that
// want browser-rendered HTML.
type IBRFetcher struct {
	opts    IBROptions
	binary  string
	runFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewIBR wires an IBRFetcher. The binary is resolved at construction:
// failure to find ibr returns an error so the daemon can surface a
// clear startup diagnostic instead of failing on first fetch.
func NewIBR(opts IBROptions) (*IBRFetcher, error) {
	bin := opts.Binary
	if bin == "" {
		bin = "ibr"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("fetcher.NewIBR: %s: %w", bin, err)
	}
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	return &IBRFetcher{
		opts:   opts,
		binary: resolved,
		runFunc: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.CombinedOutput()
		},
	}, nil
}

var _ jit.Fetcher = (*IBRFetcher)(nil)
var _ github.Fetcher = (*IBRFetcher)(nil)

// Fetch satisfies jit.Fetcher. Calls `ibr snap <url>` and returns its
// stdout as the body. Non-zero exit produces an error containing the
// captured output.
func (i *IBRFetcher) Fetch(ctx context.Context, urlStr string) (string, error) {
	out, err := i.snap(ctx, urlStr)
	return string(out), err
}

// Get satisfies github.Fetcher. Status is hard-coded to 200 on success
// because ibr abstracts away the underlying HTTP status; non-200
// fetches surface as run-time errors instead. ContentType defaults to
// application/json (matching `ibr snap` output).
func (i *IBRFetcher) Get(ctx context.Context, urlStr string) (github.FetchResult, error) {
	out, err := i.snap(ctx, urlStr)
	if err != nil {
		return github.FetchResult{}, err
	}
	return github.FetchResult{
		Body:        out,
		Status:      http.StatusOK,
		ContentType: "application/json",
	}, nil
}

func (i *IBRFetcher) snap(ctx context.Context, urlStr string) ([]byte, error) {
	if i.opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, i.opts.Timeout)
		defer cancel()
	}
	args := append([]string{"snap", urlStr}, i.opts.ExtraArgs...)
	out, err := i.runFunc(ctx, i.binary, args...)
	if err != nil {
		// Surface the first 256 bytes of stdout in the error so
		// operators can triage without enabling debug logs.
		preview := out
		if len(preview) > 256 {
			preview = preview[:256]
		}
		return nil, fmt.Errorf("fetcher.IBR snap %s (status=%s): %w; output=%q",
			urlStr, exitStatus(err), err, preview)
	}
	return out, nil
}

// exitStatus extracts the process exit code (or "?" for non-exit
// errors) for inclusion in error messages.
func exitStatus(err error) string {
	if ee, ok := err.(*exec.ExitError); ok {
		return strconv.Itoa(ee.ExitCode())
	}
	return "?"
}

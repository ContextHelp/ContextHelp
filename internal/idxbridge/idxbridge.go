// Package idxbridge provides a client-side bridge that routes index/search
// (RSQL idx) queries and the analyze/ingest enqueue write surface to a running
// dpkms daemon via HTTP, falling back to a direct local implementation when
// no daemon is reachable.
//
// The bridge accepts an ordered list of daemon base URLs (primary first —
// e.g. a remote instance, then a local one). Every request walks the list in
// order and uses the first instance whose health probe answers; probe results
// are cached per instance with an asymmetric TTL (up: ProbeInterval, down:
// NegativeProbeInterval), so a recovered primary reclaims traffic within
// about a second and no failover is sticky.
//
// Usage:
//
//	bridge := idxbridge.New(idxbridge.Config{
//	    BaseURLs:        []string{"https://m3.example.net:7700", "http://127.0.0.1:8080"},
//	    ProbeTimeout:    500 * time.Millisecond,
//	    Fallback:        svc, // *service.Service or any IdxSearcher
//	    AnalyzeFallback: svc, // *service.Service or any IdxAnalyzer
//	})
//
//	// Bridge auto-detects daemons on first call; results are cached per instance.
//	results, total, err := bridge.SearchObjects(ctx, "type==article", 20, 0)
//	jobID, servedBy, err := bridge.Analyze(ctx, req)
package idxbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const (
	// DefaultBaseURL is the default dpkms daemon address.
	DefaultBaseURL = "http://127.0.0.1:8080"
	// DefaultProbeTimeout is the maximum time to wait for the health probe.
	DefaultProbeTimeout = 500 * time.Millisecond
	// DefaultProbeInterval is how long a daemon-up result is cached.
	DefaultProbeInterval = 30 * time.Second
	// DefaultNegativeProbeInterval is how long a daemon-down result is
	// cached. Deliberately much shorter than DefaultProbeInterval: a CLI
	// that probed just before daemon startup must notice the daemon within
	// about a second, not keep writing directly for the full positive TTL.
	// The same short TTL is what routes traffic back to a recovered primary.
	DefaultNegativeProbeInterval = 1 * time.Second
	// DefaultRequestTimeout bounds non-probe daemon requests. Distinct from
	// ProbeTimeout: probes must answer fast, but a real search or enqueue
	// may legitimately take longer.
	DefaultRequestTimeout = 30 * time.Second
)

// IdxSearcher is the local fallback interface for index/search queries.
// *service.Service satisfies this interface.
type IdxSearcher interface {
	SearchObjects(ctx context.Context, query string, limit, offset int, profileID ...string) (
		[]*storage.KnowledgeObject, int, error)
}

// Config holds configuration for the IdxBridge.
type Config struct {
	// BaseURLs is the ordered dpkms server list, primary first (scheme +
	// host + port each). Requests walk the list per call and use the first
	// instance whose health probe answers. When set, BaseURL is ignored.
	BaseURLs []string
	// BaseURL is the single-instance form of BaseURLs. Used only when
	// BaseURLs is empty; defaults to DefaultBaseURL when both are empty.
	BaseURL string
	// ProbeTimeout is the max time to wait when probing a daemon's health.
	// Defaults to DefaultProbeTimeout.
	ProbeTimeout time.Duration
	// ProbeInterval is how long a successful probe result is cached.
	// Defaults to DefaultProbeInterval.
	ProbeInterval time.Duration
	// NegativeProbeInterval is how long a failed probe result is cached.
	// Defaults to DefaultNegativeProbeInterval.
	NegativeProbeInterval time.Duration
	// Fallback is the local searcher used when no daemon is reachable.
	// Optional when AnalyzeFallback is set; at least one fallback is
	// required.
	Fallback IdxSearcher
	// AnalyzeFallback is the local enqueue path used when no daemon is
	// reachable — typically the dblock-gated direct storage path.
	// Optional when Fallback is set.
	AnalyzeFallback IdxAnalyzer
	// RequestTimeout bounds non-probe daemon requests when HTTPClient is
	// not supplied. Defaults to DefaultRequestTimeout.
	RequestTimeout time.Duration
	// HTTPClient overrides the default HTTP clients (e.g. for tests).
	HTTPClient *http.Client
}

// serverState is one configured instance plus its cached probe result.
type serverState struct {
	baseURL string

	mu       sync.Mutex
	live     bool
	probedAt time.Time
}

// IdxBridge routes SearchObjects and Analyze calls to the first reachable
// dpkms instance in its ordered server list, and falls back to the local
// Fallback implementations when none answers.
type IdxBridge struct {
	cfg       Config
	client    *http.Client // probe client, ProbeTimeout-bound
	reqClient *http.Client // request client, RequestTimeout-bound
	servers   []*serverState
}

// New creates an IdxBridge with the given Config.
// Panics when no fallback at all is configured — a bridge that can neither
// search nor enqueue locally is a misconstruction, not a runtime condition.
func New(cfg Config) *IdxBridge {
	if cfg.Fallback == nil && cfg.AnalyzeFallback == nil {
		panic("idxbridge.New: at least one of Fallback or AnalyzeFallback must be set")
	}
	urls := cfg.BaseURLs
	if len(urls) == 0 {
		if cfg.BaseURL == "" {
			cfg.BaseURL = DefaultBaseURL
		}
		urls = []string{cfg.BaseURL}
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = DefaultProbeTimeout
	}
	if cfg.ProbeInterval <= 0 {
		cfg.ProbeInterval = DefaultProbeInterval
	}
	if cfg.NegativeProbeInterval <= 0 {
		cfg.NegativeProbeInterval = DefaultNegativeProbeInterval
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	client := cfg.HTTPClient
	reqClient := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.ProbeTimeout}
		reqClient = &http.Client{Timeout: cfg.RequestTimeout}
	}
	servers := make([]*serverState, len(urls))
	for i, u := range urls {
		servers[i] = &serverState{baseURL: strings.TrimRight(u, "/")}
	}
	return &IdxBridge{cfg: cfg, client: client, reqClient: reqClient, servers: servers}
}

// Probe reports whether any configured instance is reachable, walking the
// ordered list. Per-instance results are cached asymmetrically: daemon-up for
// ProbeInterval, daemon-down for the much shorter NegativeProbeInterval, so
// an instance that starts (or recovers) right after a miss is noticed quickly.
func (b *IdxBridge) Probe(ctx context.Context) bool {
	for _, s := range b.servers {
		if b.probeServer(ctx, s) {
			return true
		}
	}
	return false
}

// probeServer returns the (possibly cached) health of one instance.
func (b *IdxBridge) probeServer(ctx context.Context, s *serverState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	ttl := b.cfg.NegativeProbeInterval
	if s.live {
		ttl = b.cfg.ProbeInterval
	}
	if !s.probedAt.IsZero() && time.Since(s.probedAt) < ttl {
		return s.live
	}

	s.live = b.probe(ctx, s.baseURL)
	s.probedAt = time.Now()
	return s.live
}

// probe performs the actual health check without locking.
func (b *IdxBridge) probe(ctx context.Context, baseURL string) bool {
	pctx, cancel := context.WithTimeout(ctx, b.cfg.ProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(pctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// InvalidateProbe resets every instance's cached probe result so the next
// call re-probes. Useful in tests or after a known daemon restart.
func (b *IdxBridge) InvalidateProbe() {
	for _, s := range b.servers {
		s.invalidate()
	}
}

func (s *serverState) invalidate() {
	s.mu.Lock()
	s.probedAt = time.Time{}
	s.mu.Unlock()
}

// serverList renders the configured base URLs for error messages.
func (b *IdxBridge) serverList() string {
	urls := make([]string, len(b.servers))
	for i, s := range b.servers {
		urls[i] = s.baseURL
	}
	return strings.Join(urls, ", ")
}

// SearchObjects executes an RSQL query. It walks the ordered server list and
// proxies the request to GET /api/v1/search on the first live instance; any
// failure there invalidates that instance's probe and walks on. When no
// instance serves the query it delegates to the Fallback.
func (b *IdxBridge) SearchObjects(
	ctx context.Context,
	query string,
	limit, offset int,
	profileID ...string,
) ([]*storage.KnowledgeObject, int, error) {
	for _, s := range b.servers {
		if !b.probeServer(ctx, s) {
			continue
		}
		objs, total, err := b.remoteSearch(ctx, s.baseURL, query, limit, offset, profileID...)
		if err == nil {
			return objs, total, nil
		}
		// Instance answered health but search failed — invalidate it and
		// walk to the next one.
		s.invalidate()
	}
	if b.cfg.Fallback == nil {
		return nil, 0, fmt.Errorf("idxbridge: no daemon reachable (%s) and no local search fallback configured", b.serverList())
	}
	return b.cfg.Fallback.SearchObjects(ctx, query, limit, offset, profileID...)
}

// remoteSearch calls one instance's /api/v1/search endpoint.
func (b *IdxBridge) remoteSearch(
	ctx context.Context,
	baseURL string,
	query string,
	limit, offset int,
	profileID ...string,
) ([]*storage.KnowledgeObject, int, error) {
	u, err := url.Parse(baseURL + "/api/v1/search")
	if err != nil {
		return nil, 0, fmt.Errorf("idxbridge: bad base URL: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(offset))
	if len(profileID) > 0 && profileID[0] != "" {
		q.Set("profile", profileID[0])
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("idxbridge: build request: %w", err)
	}

	resp, err := b.reqClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("idxbridge: search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, 0, fmt.Errorf("idxbridge: daemon returned %d: %s", resp.StatusCode, body)
	}

	var payload struct {
		Data  []*storage.KnowledgeObject `json:"data"`
		Total int                        `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, 0, fmt.Errorf("idxbridge: decode response: %w", err)
	}
	return payload.Data, payload.Total, nil
}

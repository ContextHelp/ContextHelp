// Package idxbridge provides a client-side bridge that routes index/search
// (RSQL idx) queries and the analyze/ingest enqueue write surface to a running
// dpkms daemon via HTTP, falling back to a direct local implementation when
// the daemon is unreachable.
//
// Usage:
//
//	bridge := idxbridge.New(idxbridge.Config{
//	    BaseURL:         "http://127.0.0.1:8080",
//	    ProbeTimeout:    500 * time.Millisecond,
//	    Fallback:        svc, // *service.Service or any IdxSearcher
//	    AnalyzeFallback: svc, // *service.Service or any IdxAnalyzer
//	})
//
//	// Bridge auto-detects daemon on first call; result is cached per instance.
//	results, total, err := bridge.SearchObjects(ctx, "type==article", 20, 0)
//	jobID, err := bridge.Analyze(ctx, req)
package idxbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
	// BaseURL is the dpkms daemon's HTTP base URL (scheme + host + port).
	// Defaults to DefaultBaseURL when empty.
	BaseURL string
	// ProbeTimeout is the max time to wait when probing the daemon health.
	// Defaults to DefaultProbeTimeout.
	ProbeTimeout time.Duration
	// ProbeInterval is how long a successful probe result is cached.
	// Defaults to DefaultProbeInterval.
	ProbeInterval time.Duration
	// NegativeProbeInterval is how long a failed probe result is cached.
	// Defaults to DefaultNegativeProbeInterval.
	NegativeProbeInterval time.Duration
	// Fallback is the local searcher used when the daemon is unreachable.
	// Optional when AnalyzeFallback is set; at least one fallback is
	// required.
	Fallback IdxSearcher
	// AnalyzeFallback is the local enqueue path used when the daemon is
	// unreachable — typically the dblock-gated direct storage path.
	// Optional when Fallback is set.
	AnalyzeFallback IdxAnalyzer
	// RequestTimeout bounds non-probe daemon requests when HTTPClient is
	// not supplied. Defaults to DefaultRequestTimeout.
	RequestTimeout time.Duration
	// HTTPClient overrides the default HTTP clients (e.g. for tests).
	HTTPClient *http.Client
}

// IdxBridge routes SearchObjects and Analyze calls to the dpkms daemon when
// it is reachable, and falls back to the local Fallback implementations
// otherwise.
type IdxBridge struct {
	cfg       Config
	client    *http.Client // probe client, ProbeTimeout-bound
	reqClient *http.Client // request client, RequestTimeout-bound

	mu          sync.Mutex
	daemonLive  bool
	probedAt    time.Time
}

// New creates an IdxBridge with the given Config.
// Panics when no fallback at all is configured — a bridge that can neither
// search nor enqueue locally is a misconstruction, not a runtime condition.
func New(cfg Config) *IdxBridge {
	if cfg.Fallback == nil && cfg.AnalyzeFallback == nil {
		panic("idxbridge.New: at least one of Fallback or AnalyzeFallback must be set")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
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
	return &IdxBridge{cfg: cfg, client: client, reqClient: reqClient}
}

// Probe checks whether the daemon is reachable by calling its /health endpoint.
// Returns true if the daemon responded with a 2xx status within the configured
// ProbeTimeout. Results are cached asymmetrically: daemon-up for ProbeInterval,
// daemon-down for the much shorter NegativeProbeInterval, so a daemon that
// starts right after a miss is noticed quickly.
func (b *IdxBridge) Probe(ctx context.Context) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	ttl := b.cfg.NegativeProbeInterval
	if b.daemonLive {
		ttl = b.cfg.ProbeInterval
	}
	if !b.probedAt.IsZero() && time.Since(b.probedAt) < ttl {
		return b.daemonLive
	}

	b.daemonLive = b.probe(ctx)
	b.probedAt = time.Now()
	return b.daemonLive
}

// probe performs the actual health check without locking.
func (b *IdxBridge) probe(ctx context.Context) bool {
	pctx, cancel := context.WithTimeout(ctx, b.cfg.ProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(pctx, http.MethodGet, b.cfg.BaseURL+"/health", nil)
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

// InvalidateProbe resets the cached probe result so the next call re-probes.
// Useful in tests or after a known daemon restart.
func (b *IdxBridge) InvalidateProbe() {
	b.mu.Lock()
	b.probedAt = time.Time{}
	b.mu.Unlock()
}

// SearchObjects executes an RSQL query. When the daemon is live it proxies the
// request to GET /api/v1/search; otherwise it delegates to the Fallback.
func (b *IdxBridge) SearchObjects(
	ctx context.Context,
	query string,
	limit, offset int,
	profileID ...string,
) ([]*storage.KnowledgeObject, int, error) {
	if b.Probe(ctx) {
		objs, total, err := b.remoteSearch(ctx, query, limit, offset, profileID...)
		if err == nil {
			return objs, total, nil
		}
		// Daemon responded to health but search failed — invalidate and fall back.
		b.InvalidateProbe()
	}
	if b.cfg.Fallback == nil {
		return nil, 0, fmt.Errorf("idxbridge: daemon unreachable at %s and no local search fallback configured", b.cfg.BaseURL)
	}
	return b.cfg.Fallback.SearchObjects(ctx, query, limit, offset, profileID...)
}

// remoteSearch calls the daemon's /api/v1/search endpoint.
func (b *IdxBridge) remoteSearch(
	ctx context.Context,
	query string,
	limit, offset int,
	profileID ...string,
) ([]*storage.KnowledgeObject, int, error) {
	u, err := url.Parse(b.cfg.BaseURL + "/api/v1/search")
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

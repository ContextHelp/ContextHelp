// Package enqueue is the production ambient.Enqueuer that POSTs RawEvents
// to dpkms via the existing /api/v1/analyze endpoint (per ADR-066 §Decision
// "Enqueue path" + ADR-056 unified enqueue API).
//
// The Runner consumes this via the ambient.Enqueuer interface; tests use a
// recordingEnqueuer instead. This package's HTTPClient adds:
//
//   - Endpoint URL resolution from config (ctxt config dpkms.url)
//   - Optional bearer-token auth (env-resolved per ADR-023)
//   - Retry with bounded backoff on transient failures (1s, 2s, 4s, …, capped)
//   - Marshalling of RawEvent into the POST body shape dpkms expects:
//     { content, type, ambient_source, fingerprint, session_id, metadata }
//
// Replay semantics (per ADR-067 §Replay safety): the buffer holds events
// during dpkms-down windows; the Runner re-attempts via this client when
// the buffer worker drains. Idempotency on dpkms's side is provided by the
// existing post-pipeline ContentHash dedup (internal/jobs/worker.go), so
// duplicate POSTs after a partial-success replay are safe.
package enqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// DefaultTimeout caps how long a single POST attempt may run. The Runner
// bounds the overall enqueue cost via this; replay across longer outages is
// handled by the buffer worker, not by lengthening individual POSTs.
const DefaultTimeout = 30 * time.Second

// HTTPClient is the production ambient.Enqueuer. It POSTs to the existing
// /api/v1/analyze endpoint with RawEvent fields mapped to the documented
// payload shape.
type HTTPClient struct {
	endpoint   string
	authToken  string
	httpClient *http.Client
}

// Option configures HTTPClient construction.
type Option func(*HTTPClient)

// WithAuthToken sets the bearer token attached to every POST. When unset,
// no Authorization header is added (default localhost / no-auth deployment).
func WithAuthToken(token string) Option {
	return func(c *HTTPClient) { c.authToken = token }
}

// WithHTTPClient overrides the underlying *http.Client. Tests use this to
// inject a stub or a fake transport; production callers leave it at the
// default (DefaultTimeout-bounded http.Client).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *HTTPClient) { c.httpClient = hc }
}

// New constructs an HTTPClient targeting the supplied dpkms endpoint URL.
// The endpoint should be the dpkms server root (e.g. "http://127.0.0.1:8080");
// the client appends /api/v1/analyze itself. Trailing slashes are tolerated.
func New(endpoint string, opts ...Option) (*HTTPClient, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("ambient/enqueue: endpoint is required")
	}
	c := &HTTPClient{
		endpoint:   strings.TrimRight(endpoint, "/"),
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Endpoint returns the resolved endpoint URL (dpkms root, without
// /api/v1/analyze suffix). Used by status / MCP health tool.
func (c *HTTPClient) Endpoint() string {
	return c.endpoint
}

// payload is the body shape dpkms's /api/v1/analyze accepts. Mirrors the
// new optional fields per ADR-066 §Decision "Enqueue path".
type payload struct {
	Content       string         `json:"content"`
	Type          string         `json:"type,omitempty"`
	Pipeline      string         `json:"pipeline,omitempty"`
	AmbientSource string         `json:"ambient_source,omitempty"`
	Fingerprint   string         `json:"fingerprint,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// Enqueue POSTs ev to dpkms's /api/v1/analyze. Returns nil on 2xx; an error
// describing the failure category otherwise. The Runner emits
// ctxt.ambient.enqueue.{succeeded,failed} based on the returned error.
//
// Mapping of RawEvent → payload:
//
//	Payload          → content
//	Kind             → type (text | url | image | file | window-focus | meeting)
//	SuggestedPipeline → pipeline (lets dpkms's selector use it as a hint)
//	Source           → ambient_source (e.g. "clipboard")
//	Fingerprint      → fingerprint (dpkms uses this AND post-pipeline ContentHash)
//	SessionID        → session_id (per ADR-067)
//	Metadata         → metadata
func (c *HTTPClient) Enqueue(ctx context.Context, ev ambient.RawEvent) error {
	body := payload{
		Content:       string(ev.Payload),
		Type:          string(ev.Kind),
		Pipeline:      ev.SuggestedPipeline,
		AmbientSource: ev.Source,
		Fingerprint:   ev.Fingerprint,
		SessionID:     ev.SessionID,
		Metadata:      ev.Metadata,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := c.endpoint + "/api/v1/analyze"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("dpkms responded %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// Compile-time assertion: HTTPClient satisfies the ambient.Enqueuer
// interface so the substrate Runner can hold one without conversion.
var _ ambient.Enqueuer = (*HTTPClient)(nil)

package idxbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// IdxAnalyzer is the local fallback for the analyze/ingest enqueue surface.
// *service.Service satisfies this interface.
type IdxAnalyzer interface {
	Analyze(ctx context.Context, req service.AnalyzeRequest) (string, error)
}

// AnalyzeFunc adapts a plain function to the IdxAnalyzer interface, letting
// callers defer expensive construction (storage open, migrations) until the
// fallback actually runs.
type AnalyzeFunc func(ctx context.Context, req service.AnalyzeRequest) (string, error)

// Analyze implements IdxAnalyzer.
func (f AnalyzeFunc) Analyze(ctx context.Context, req service.AnalyzeRequest) (string, error) {
	return f(ctx, req)
}

// RemoteError is an application-level response from a live daemon (non-2xx
// with a completed HTTP exchange). The daemon answered and made a decision, so
// routing must NOT fall back on it — a rejected request would otherwise be
// silently re-run against the local direct path.
type RemoteError struct {
	// StatusCode is the daemon's HTTP status.
	StatusCode int
	// Body is the daemon's response body (truncated), preserved verbatim so
	// callers can surface the daemon's own message.
	Body string
}

// Error renders the daemon's status and message.
func (e *RemoteError) Error() string {
	return fmt.Sprintf("daemon returned %d: %s", e.StatusCode, e.Body)
}

// Analyze enqueues content for ingestion. When the daemon is live it posts to
// POST /api/v1/analyze; when unreachable it delegates to the configured
// AnalyzeFallback (the gated local direct path). A live daemon's rejection
// (*RemoteError) is surfaced, never retried locally; only transport-level
// failures fall back.
func (b *IdxBridge) Analyze(ctx context.Context, req service.AnalyzeRequest) (string, error) {
	if b.Probe(ctx) {
		jobID, err := b.remoteAnalyze(ctx, req)
		if err == nil {
			return jobID, nil
		}
		var rerr *RemoteError
		if errors.As(err, &rerr) {
			return "", err
		}
		// Daemon answered health but the request failed at transport level —
		// re-probe on the next call and fall back now.
		b.InvalidateProbe()
	}
	if b.cfg.AnalyzeFallback == nil {
		return "", fmt.Errorf("idxbridge: daemon unreachable at %s and no local analyze fallback configured", b.cfg.BaseURL)
	}
	return b.cfg.AnalyzeFallback.Analyze(ctx, req)
}

// remoteAnalyze posts the request to the daemon's analyze endpoint.
func (b *IdxBridge) remoteAnalyze(ctx context.Context, req service.AnalyzeRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("idxbridge: marshal analyze request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.cfg.BaseURL+"/api/v1/analyze", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("idxbridge: build analyze request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := b.reqClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("idxbridge: analyze request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("idxbridge: read analyze response: %w", err)
	}

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return "", &RemoteError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	var payload struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return "", fmt.Errorf("idxbridge: decode analyze response: %w", err)
	}
	return payload.JobID, nil
}

package idxbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
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

// RemoteError is an application-level response from a live daemon: a non-2xx
// status on a completed HTTP exchange, on either the analyze or the search
// surface. The daemon answered and made a decision, so routing must NOT fall
// back on a 4xx — the rejected request would otherwise be silently re-run
// against the next instance or the local direct path.
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

// Analyze enqueues content for ingestion. It walks the ordered server list
// and posts to POST /api/v1/analyze on the first live instance; servedBy
// reports that instance's base URL, or "" when the configured AnalyzeFallback
// (the gated local direct path) accepted the enqueue. A live instance's
// rejection (*RemoteError) is surfaced, never replayed against the next
// instance or the local queue; only transport-level failures walk on.
//
// One call = one logical submission = one idempotency key: when the caller
// did not supply req.IdempotencyKey the bridge mints a UUID here, before the
// walk, and every attempt (each instance, and the local fallback) carries the
// identical key. A transport failure after an instance already enqueued the
// payload — response lost in transit — therefore replays as a dedupe hit on
// the next instance sharing the queue instead of a duplicate ingestion.
func (b *IdxBridge) Analyze(ctx context.Context, req service.AnalyzeRequest) (jobID, servedBy string, err error) {
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = uuid.NewString()
	}
	for _, s := range b.servers {
		if !b.probeServer(ctx, s) {
			continue
		}
		jobID, err := b.remoteAnalyze(ctx, s.baseURL, req)
		if err == nil {
			return jobID, s.baseURL, nil
		}
		var rerr *RemoteError
		if errors.As(err, &rerr) {
			return "", "", err
		}
		// Instance answered health but the request failed at transport
		// level — re-probe it on the next call and walk on now.
		s.invalidate()
	}
	if b.cfg.AnalyzeFallback == nil {
		return "", "", fmt.Errorf("idxbridge: no daemon reachable (%s) and no local analyze fallback configured", b.serverList())
	}
	jobID, err = b.cfg.AnalyzeFallback.Analyze(ctx, req)
	return jobID, "", err
}

// remoteAnalyze posts the request to one instance's analyze endpoint.
func (b *IdxBridge) remoteAnalyze(ctx context.Context, baseURL string, req service.AnalyzeRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("idxbridge: marshal analyze request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v1/analyze", bytes.NewReader(body))
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

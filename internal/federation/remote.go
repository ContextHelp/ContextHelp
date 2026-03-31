package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FederationPushRequest is the JSON body sent to POST /api/v1/federation/push.
type FederationPushRequest struct {
	Objects []storage.KnowledgeObject `json:"objects"`
	Edges   []storage.Edge            `json:"edges"`
}

// RemotePusher pushes objects to a remote dpkms instance via HTTP POST.
// Endpoint: POST /api/v1/federation/push
// Auth: Bearer token per target (optional; empty = no auth header).
// Retry: exponential backoff, max 3 attempts on transient errors.
type RemotePusher struct {
	name    string
	baseURL string // e.g. https://team.internal:8080
	token   string // Bearer token; empty = no auth
	client  *http.Client
}

// NewRemotePusher creates a RemotePusher for the given peer.
func NewRemotePusher(name, baseURL, token string) *RemotePusher {
	return &RemotePusher{
		name:    name,
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the federation entry name from config.
func (p *RemotePusher) Name() string { return p.name }

// Push sends objects and edges as JSON to POST /api/v1/federation/push.
// Retries up to 3 times with exponential backoff (1s, 2s, 4s) on transient errors.
// 4xx (except 429) are non-retriable; returns error immediately.
func (p *RemotePusher) Push(ctx context.Context, objects []storage.KnowledgeObject, edges []storage.Edge) error {
	payload := FederationPushRequest{
		Objects: objects,
		Edges:   edges,
	}
	if payload.Objects == nil {
		payload.Objects = []storage.KnowledgeObject{}
	}
	if payload.Edges == nil {
		payload.Edges = []storage.Edge{}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("federation remote push %q: marshal: %w", p.name, err)
	}

	url := p.baseURL + "/api/v1/federation/push"

	const maxAttempts = 3
	backoff := time.Second

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("federation remote push %q: %w", p.name, ctx.Err())
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("federation remote push %q: build request: %w", p.name, err)
		}
		req.Header.Set("Content-Type", "application/json")
		if p.token != "" {
			req.Header.Set("Authorization", "Bearer "+p.token)
		}

		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("federation remote push %q: attempt %d: %w", p.name, attempt+1, err)
			continue // transient — retry
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return nil
		}

		// 4xx (except 429) are non-retriable.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return fmt.Errorf("federation remote push %q: non-retriable HTTP %d", p.name, resp.StatusCode)
		}

		// 5xx and 429 are transient — retry.
		lastErr = fmt.Errorf("federation remote push %q: attempt %d: HTTP %d", p.name, attempt+1, resp.StatusCode)
	}

	return fmt.Errorf("federation remote push %q: all %d attempts failed: %w", p.name, maxAttempts, lastErr)
}

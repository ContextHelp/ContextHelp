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
//
// Entities is the set of entity rows referenced by mention edges in this
// batch (ADR-049: object→entity, edge_type='mentions'). Receivers upsert
// them so the mention edges they accompany do not dangle. T-0175 added
// Entities for US-0319 AC #6.
type FederationPushRequest struct {
	Objects  []storage.KnowledgeObject `json:"objects"`
	Edges    []storage.Edge            `json:"edges"`
	Entities []storage.Entity          `json:"entities"`
}

// RemotePusher pushes objects to a remote dpkms instance via HTTP POST.
// Endpoint: POST /api/v1/federation/push
// Auth: Bearer token per target (optional; empty = no auth header).
// Retry: exponential backoff, max 3 attempts on transient errors.
//
// Watermarks: when constructed via NewRemotePusherWithWatermarks, a successful
// HTTP push advances the source-side federation_watermarks row for this peer
// to time.Now() (T-0188). Without that advance the worker re-sends the same
// batch every tick and the receiver returns 5xx on duplicate ids. The store
// is intentionally optional: the bare NewRemotePusher constructor preserves
// the original behaviour for tests + callers that don't care about the gate.
type RemotePusher struct {
	name       string
	baseURL    string // e.g. https://team.internal:8080
	token      string // Bearer token; empty = no auth
	client     *http.Client
	watermarks storage.WatermarkStore // optional; nil = no watermark advance
}

// NewRemotePusher creates a RemotePusher for the given peer. Watermarks are
// not advanced — use NewRemotePusherWithWatermarks for production wiring.
func NewRemotePusher(name, baseURL, token string) *RemotePusher {
	return &RemotePusher{
		name:    name,
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// NewRemotePusherWithWatermarks creates a RemotePusher that advances the
// source-side watermark on each successful (200) HTTP push. wm is the source
// driver's WatermarkStore (the same store LocalPusher writes to for local
// targets — see local.go and US-0319 AC #3).
func NewRemotePusherWithWatermarks(
	name, baseURL, token string,
	wm storage.WatermarkStore,
) *RemotePusher {
	p := NewRemotePusher(name, baseURL, token)
	p.watermarks = wm
	return p
}

// Name returns the federation entry name from config.
func (p *RemotePusher) Name() string { return p.name }

// Push sends objects, edges, and entities as JSON to POST /api/v1/federation/push.
// Retries up to 3 times with exponential backoff (1s, 2s, 4s) on transient errors.
// 4xx (except 429) are non-retriable; returns error immediately.
func (p *RemotePusher) Push(
	ctx context.Context,
	objects []storage.KnowledgeObject,
	edges []storage.Edge,
	entities []storage.Entity,
) error {
	payload := FederationPushRequest{
		Objects:  objects,
		Edges:    edges,
		Entities: entities,
	}
	if payload.Objects == nil {
		payload.Objects = []storage.KnowledgeObject{}
	}
	if payload.Edges == nil {
		payload.Edges = []storage.Edge{}
	}
	if payload.Entities == nil {
		payload.Entities = []storage.Entity{}
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
			// T-0188: advance source-side watermark so the next tick filters
			// this batch out. Mirrors LocalPusher.Push (US-0319 AC #3). A
			// watermark write failure is reported as a push failure so the
			// next tick retries — better to re-send than to silently drift
			// out of sync.
			if p.watermarks != nil {
				if err := p.watermarks.SetWatermark(ctx, p.name, time.Now().UTC()); err != nil {
					return fmt.Errorf("federation remote push %q: advance watermark: %w",
						p.name, err)
				}
			}
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

package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
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

// pushBody is FederationPushRequest on the wire, with each object
// already encoded: Push encodes an object once, to size its request and
// to send it.
type pushBody struct {
	Objects  []json.RawMessage `json:"objects"`
	Edges    []storage.Edge    `json:"edges"`
	Entities []storage.Entity  `json:"entities"`
}

// DefaultMaxRequestBytes caps the encoded objects one push request
// carries. Content travels inline, so a batch of large objects is split
// across requests.
const DefaultMaxRequestBytes = 4 << 20

// RemotePusher pushes objects to a remote dpkms instance via HTTP POST.
// Endpoint: POST /api/v1/federation/push
// Auth: Bearer token per target (optional; empty = no auth header).
// Retry: exponential backoff, max 3 attempts on transient errors.
//
// A batch goes out in as many requests as keep each request's encoded
// objects within maxRequestBytes; an object larger than that goes alone.
//
// Watermarks: when constructed via NewRemotePusherWithWatermarks, the
// target's watermark lives in the source's WatermarkStore under the
// entry name and moves to the last object of each request the receiver
// acknowledges (200). The bare NewRemotePusher keeps none: its
// watermark is always zero, so Sync re-sends everything.
type RemotePusher struct {
	name            string
	baseURL         string // e.g. https://team.internal:8080
	token           string // Bearer token; empty = no auth
	client          *http.Client
	watermarks      storage.WatermarkStore // optional; nil = no watermark
	blobs           storage.BlobStore      // the source's store; resolves blob:// refs before sending
	maxRequestBytes int                    // <= 0 = one request per batch
}

// NewRemotePusher creates a RemotePusher for the given peer. It keeps no
// watermark — use NewRemotePusherWithWatermarks for production wiring.
func NewRemotePusher(name, baseURL, token string) *RemotePusher {
	return &RemotePusher{
		name:            name,
		baseURL:         baseURL,
		token:           token,
		client:          &http.Client{Timeout: 30 * time.Second},
		maxRequestBytes: DefaultMaxRequestBytes,
	}
}

// NewRemotePusherWithWatermarks creates a RemotePusher whose watermark
// lives in wm, the source driver's WatermarkStore.
func NewRemotePusherWithWatermarks(
	name, baseURL, token string,
	wm storage.WatermarkStore,
	blobs storage.BlobStore,
) *RemotePusher {
	p := NewRemotePusher(name, baseURL, token)
	p.watermarks = wm
	p.blobs = blobs
	return p
}

// Name returns the federation entry name from config.
func (p *RemotePusher) Name() string { return p.name }

// Watermark returns the watermark kept in the source's store; zero
// without one.
func (p *RemotePusher) Watermark(ctx context.Context) (int64, error) {
	if p.watermarks == nil {
		return 0, nil
	}
	return p.watermarks.GetWatermark(ctx, p.name)
}

// encodeInline returns o encoded for the wire, every blob:// ref
// replaced by the content it names, read from the source's store. o is
// not modified.
func (p *RemotePusher) encodeInline(ctx context.Context, o storage.KnowledgeObject) (json.RawMessage, error) {
	raw, err := blob.Content(ctx, p.blobs, o.RawContent)
	if err != nil {
		return nil, fmt.Errorf("object %q: %w", o.ID, err)
	}
	text, err := blob.Content(ctx, p.blobs, o.TextContent)
	if err != nil {
		return nil, fmt.Errorf("object %q: %w", o.ID, err)
	}
	o.RawContent, o.TextContent = raw, text
	enc, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("object %q: marshal: %w", o.ID, err)
	}
	return enc, nil
}

// Push sends objects, edges, and entities as JSON to POST
// /api/v1/federation/push, split into requests of at most
// maxRequestBytes of encoded objects. Each edge rides with the first
// object it touches; the first request carries every entity, so mention
// edges in later requests resolve, and any edge touching no object.
// Requests go in order and Push stops at the first that fails.
//
// Each request retries up to 3 times with exponential backoff (1s, 2s, 4s)
// on transient errors. 4xx (except 429) are non-retriable; returns error
// immediately.
func (p *RemotePusher) Push(
	ctx context.Context,
	objects []storage.KnowledgeObject,
	edges []storage.Edge,
	entities []storage.Entity,
) error {
	edgesOf, loose := assignEdges(objects, edges)
	body := pushBody{Edges: loose, Entities: entities}
	size := 0
	var last int64

	flush := func() error {
		if err := p.send(ctx, body); err != nil {
			return err
		}
		if p.watermarks != nil && len(body.Objects) > 0 {
			// Advance only on the receiver's ack, so a failed request
			// is re-sent next tick. A watermark write failure is a push
			// failure too: better to re-send than to drift out of sync.
			if err := p.watermarks.SetWatermark(ctx, p.name, last); err != nil {
				return fmt.Errorf("federation remote push %q: advance watermark: %w", p.name, err)
			}
		}
		body, size = pushBody{}, 0
		return nil
	}

	for i := range objects {
		// The wire format carries objects only, no blobs: send content
		// inline; the receiver externalizes it again by its own threshold.
		enc, err := p.encodeInline(ctx, objects[i])
		if err != nil {
			return fmt.Errorf("federation remote push %q: %w", p.name, err)
		}
		if len(body.Objects) > 0 && p.maxRequestBytes > 0 && size+len(enc) > p.maxRequestBytes {
			if err := flush(); err != nil {
				return err
			}
		}
		body.Objects = append(body.Objects, enc)
		body.Edges = append(body.Edges, edgesOf[i]...)
		size += len(enc)
		last = objects[i].ChangeSeq
	}
	return flush()
}

// assignEdges groups edges by the index of the first object each one
// touches (from or to); loose are the edges touching none.
func assignEdges(objects []storage.KnowledgeObject, edges []storage.Edge) (byObject map[int][]storage.Edge, loose []storage.Edge) {
	first := make(map[string]int, len(objects))
	for i := len(objects) - 1; i >= 0; i-- {
		first[objects[i].ID] = i
	}
	byObject = make(map[int][]storage.Edge)
	for _, e := range edges {
		i, ok := first[e.FromID]
		if j, toOK := first[e.ToID]; toOK && (!ok || j < i) {
			i, ok = j, true
		}
		if !ok {
			loose = append(loose, e)
			continue
		}
		byObject[i] = append(byObject[i], e)
	}
	return byObject, loose
}

// send posts one request body, retrying transient failures.
func (p *RemotePusher) send(ctx context.Context, payload pushBody) error {
	if payload.Objects == nil {
		payload.Objects = []json.RawMessage{}
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

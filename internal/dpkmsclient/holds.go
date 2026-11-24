package dpkmsclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// HoldRequest is the body of POST /api/v1/holds.
type HoldRequest struct {
	ObjectID  string `json:"object_id,omitempty"`
	SourceKey string `json:"source_key,omitempty"`
	HoldID    string `json:"hold_id"`
	Source    string `json:"source,omitempty"`
	Reason    string `json:"reason"`
	PlacedBy  string `json:"placed_by,omitempty"`
}

// ListHolds returns active holds matching the optional filters.
func (c *Client) ListHolds(ctx context.Context, objectID, sourceKey, holdID string) ([]storage.ObjectHold, error) {
	q := url.Values{}
	if objectID != "" {
		q.Set("object_id", objectID)
	}
	if sourceKey != "" {
		q.Set("source_key", sourceKey)
	}
	if holdID != "" {
		q.Set("hold_id", holdID)
	}
	var out struct {
		Data []storage.ObjectHold `json:"data"`
	}
	if err := c.Get(ctx, "/api/v1/holds", q, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// PlaceHold places a hold on an object or source_key (POST /api/v1/holds).
func (c *Client) PlaceHold(ctx context.Context, req HoldRequest) ([]storage.ObjectHold, error) {
	var out struct {
		Data []storage.ObjectHold `json:"data"`
	}
	if err := c.Post(ctx, "/api/v1/holds", req, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// ReleaseHold releases an active hold (DELETE /api/v1/holds/{hold_id}).
func (c *Client) ReleaseHold(ctx context.Context, holdID, objectID, sourceKey, reason string) (int, error) {
	q := url.Values{}
	if objectID != "" {
		q.Set("object_id", objectID)
	}
	if sourceKey != "" {
		q.Set("source_key", sourceKey)
	}
	if reason != "" {
		q.Set("reason", reason)
	}
	var out struct {
		Released int `json:"released"`
	}
	if err := c.Do(ctx, Request{Method: http.MethodDelete, Path: "/api/v1/holds/" + url.PathEscape(holdID), Query: q}, &out); err != nil {
		return 0, err
	}
	return out.Released, nil
}

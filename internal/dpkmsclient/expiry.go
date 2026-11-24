package dpkmsclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// SetObjectExpiry schedules automatic expiration for an existing object.
// When scope is empty the entire object is erased at expiresAt; when non-empty
// only the described data is redacted in place.
func (c *Client) SetObjectExpiry(ctx context.Context, objectID string, expiresAt time.Time, scope string) error {
	body := map[string]any{
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	}
	if scope != "" {
		body["expiry_scope"] = scope
	}
	return c.Do(ctx, Request{
		Method: http.MethodPut,
		Path:   "/api/v1/objects/" + url.PathEscape(objectID) + "/expiry",
		Body:   body,
	}, nil)
}

// ClearObjectExpiry removes any scheduled expiration from an existing object.
func (c *Client) ClearObjectExpiry(ctx context.Context, objectID string) error {
	return c.Do(ctx, Request{
		Method: http.MethodDelete,
		Path:   "/api/v1/objects/" + url.PathEscape(objectID) + "/expiry",
	}, nil)
}

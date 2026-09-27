package dpkmsclient

import (
	"context"
	"net/url"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// GetObject returns the knowledge object with this ID, from
// GET /api/v1/objects/{id}. An unknown ID is a NOT_FOUND error.
func (c *Client) GetObject(ctx context.Context, id string) (*storage.KnowledgeObject, error) {
	var obj storage.KnowledgeObject
	if err := c.Get(ctx, "/api/v1/objects/"+url.PathEscape(id), nil, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

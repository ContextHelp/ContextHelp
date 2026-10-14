package dpkmsclient

import (
	"context"
	"net/url"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ObjectFacets counts the objects f selects by metadata type (GET
// /api/v1/objects/facets); objects without one count under "(none)".
// f's page fields are not sent.
func (c *Client) ObjectFacets(ctx context.Context, f ObjectFilter) (map[string]int, error) {
	var out struct {
		Data map[string]int `json:"data"`
	}
	if err := c.Get(ctx, "/api/v1/objects/facets", f.filterQuery(), &out); err != nil {
		return nil, err
	}
	if out.Data == nil {
		out.Data = map[string]int{}
	}
	return out.Data, nil
}

// RelatedObjects returns the objects that share a mention target with
// the object id, up to depth hops away, at most limit of them (GET
// /api/v1/objects/{id}/related). depth or limit 0 takes the API's
// default (1 and 10). A missing object is NOT_FOUND (exit 3).
func (c *Client) RelatedObjects(ctx context.Context, id string, depth, limit int) ([]*storage.KnowledgeObject, error) {
	q := url.Values{}
	if depth > 0 {
		q.Set("depth", strconv.Itoa(depth))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out struct {
		Data []*storage.KnowledgeObject `json:"data"`
	}
	if err := c.Get(ctx, objectPath(id)+"/related", q, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

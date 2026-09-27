package dpkmsclient

import (
	"context"
	"net/url"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EntityQuery selects entities for ListEntities. Zero fields take dpkms's
// defaults: every entity, every namespace, a page of 20.
type EntityQuery struct {
	// Query keeps entities whose slug, title or an alias contains it,
	// ignoring ASCII case.
	Query string
	// Namespace keeps entities in exactly this namespace.
	Namespace string
	Limit     int
	Offset    int
}

// ListEntities returns one page of entities matching q, by slug, from
// GET /api/v1/entities. On a gated instance a non-admin caller sees only
// the namespaces it is entitled to.
func (c *Client) ListEntities(ctx context.Context, q EntityQuery) ([]*storage.Entity, error) {
	v := url.Values{}
	if q.Query != "" {
		v.Set("q", q.Query)
	}
	if q.Namespace != "" {
		v.Set("namespace", q.Namespace)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	var out struct {
		Data []*storage.Entity `json:"data"`
	}
	if err := c.Get(ctx, "/api/v1/entities", v, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// GetEntity returns the entity with this exact slug. An unknown slug is a
// NOT_FOUND error.
func (c *Client) GetEntity(ctx context.Context, slug string) (*storage.Entity, error) {
	var e storage.Entity
	if err := c.Get(ctx, "/api/v1/entities/"+url.PathEscape(slug), nil, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// ResolveEntity returns the entity mention names, by exact slug, else by
// exact alias, from GET /api/v1/entities/resolve. A mention that names
// nothing is a NOT_FOUND error; an empty one is a USAGE error.
func (c *Client) ResolveEntity(ctx context.Context, mention string) (*storage.Entity, error) {
	var e storage.Entity
	if err := c.Get(ctx, "/api/v1/entities/resolve", url.Values{"mention": {mention}}, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// EntityBacklinks returns the objects that mention the entity.
func (c *Client) EntityBacklinks(ctx context.Context, slug string) ([]*storage.KnowledgeObject, error) {
	var out struct {
		Data []*storage.KnowledgeObject `json:"data"`
	}
	if err := c.Get(ctx, "/api/v1/entities/"+url.PathEscape(slug)+"/backlinks", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

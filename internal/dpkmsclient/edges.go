package dpkmsclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// LinkCreate is the body of POST /api/v1/edges: a typed link from
// Source to Target. Type is any link type, inverse names included;
// Context is stored as the edges' "context" metadata.
type LinkCreate struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Type    string `json:"type"`
	Context string `json:"context,omitempty"`
}

// CreateLink stores a link and, unless its type is symmetric, the
// inverse link, together, and returns the stored edges, forward first
// (POST /api/v1/edges, scope write:objects). A missing object is a
// NOT_FOUND error (exit 3); an unknown type a USAGE error (exit 2).
func (c *Client) CreateLink(ctx context.Context, l LinkCreate) ([]*storage.Edge, error) {
	var out struct {
		Data []*storage.Edge `json:"data"`
	}
	if err := c.Post(ctx, "/api/v1/edges", l, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// LinkQuery selects the links ListLinks returns.
type LinkQuery struct {
	// Direction is out, in or both (the default when empty).
	Direction string
	// Type keeps only edges of this type; "" keeps every type.
	Type string
	// Depth 0 lists every object-to-object edge touching the object.
	// Depth 1 to 3 walks the links breadth first to that depth, reaching
	// each object once.
	Depth int
}

// LinkHop is one edge of a link listing or walk.
type LinkHop struct {
	// Depth is the hop's distance from the queried object, from 1.
	Depth int `json:"depth"`
	// From is the object the hop leaves: the queried object at depth 1.
	From string `json:"from"`
	// Direction is out when Edge leaves From, in when it points at From.
	Direction string `json:"direction"`
	// ObjectID is the object at the hop's other end.
	ObjectID string        `json:"object_id"`
	Edge     *storage.Edge `json:"edge"`
}

// ListLinks returns the links of the object id (GET
// /api/v1/objects/{id}/edges, scope read:objects): outbound before
// inbound, or the hops of a walk in breadth-first order. A missing
// object is a NOT_FOUND error (exit 3).
func (c *Client) ListLinks(ctx context.Context, id string, q LinkQuery) ([]LinkHop, error) {
	v := url.Values{}
	if q.Direction != "" {
		v.Set("direction", q.Direction)
	}
	if q.Type != "" {
		v.Set("type", q.Type)
	}
	if q.Depth > 0 {
		v.Set("depth", strconv.Itoa(q.Depth))
	}
	var out struct {
		Data []LinkHop `json:"data"`
	}
	if err := c.Get(ctx, objectPath(id)+"/edges", v, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// DeleteLinks removes the links between source and target in both
// directions, the forward and inverse edges together, and returns how
// many edges went (DELETE /api/v1/edges, scope delete:edges, which only
// admin tokens hold). With typ only that link and its inverse go.
func (c *Client) DeleteLinks(ctx context.Context, source, target, typ string) (int, error) {
	v := url.Values{"source": {source}, "target": {target}}
	if typ != "" {
		v.Set("type", typ)
	}
	var out struct {
		Deleted int `json:"deleted"`
	}
	if err := c.Do(ctx, Request{Method: http.MethodDelete, Path: "/api/v1/edges", Query: v}, &out); err != nil {
		return 0, err
	}
	return out.Deleted, nil
}

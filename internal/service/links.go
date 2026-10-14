package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/graph"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrInvalidLink is a link request that cannot be applied: a missing
// object ID, an unknown link type, an unknown direction or a depth out
// of range. Nothing is read or written.
var ErrInvalidLink = errors.New("invalid link")

// nodeObject is the node type of a knowledge object in the edge store.
const nodeObject = "object"

// Link directions, relative to the object a hop leaves.
const (
	LinkOut  = "out"
	LinkIn   = "in"
	LinkBoth = "both"
)

// MaxLinkDepth bounds a link traversal.
const MaxLinkDepth = 3

// LinkRequest is a typed link from Source to Target (POST
// /api/v1/edges). Type is any link type, inverse names included;
// Context, when set, is stored as the edges' "context" metadata.
type LinkRequest struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Type    string `json:"type"`
	Context string `json:"context,omitempty"`
}

func invalidLink(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidLink, fmt.Sprintf(format, args...))
}

func linkType(s string) (graph.LinkType, error) {
	lt := graph.LinkType(s)
	if !graph.ValidLinkType(lt) {
		return "", invalidLink("unknown link type %q; valid types: %s", s, strings.Join(graph.UserLinkTypeStrings(), ", "))
	}
	return lt, nil
}

// CreateLink stores the link Source -[Type]-> Target and, unless the type
// is symmetric, its inverse Target -[inverse]-> Source, in one
// transaction, and returns the stored edges, forward first. Both objects
// must exist: a missing one is storage.ErrNotFound naming it.
func (s *Service) CreateLink(ctx context.Context, req LinkRequest) ([]*storage.Edge, error) {
	if req.Source == "" || req.Target == "" {
		return nil, invalidLink("source and target are required")
	}
	lt, err := linkType(req.Type)
	if err != nil {
		return nil, err
	}
	for _, end := range [][2]string{{"source", req.Source}, {"target", req.Target}} {
		if _, err := s.GetObject(ctx, end[1]); err != nil {
			return nil, fmt.Errorf("%s object %q: %w", end[0], end[1], err)
		}
	}

	now := time.Now().Truncate(time.Second)
	var meta map[string]any
	if req.Context != "" {
		meta = map[string]any{"context": req.Context}
	}
	edge := func(from, typ, to string) *storage.Edge {
		return &storage.Edge{
			ID: uuid.NewString(), FromType: nodeObject, FromID: from, ToType: nodeObject, ToID: to,
			EdgeType: typ, Weight: 1.0, Metadata: meta, CreatedAt: now,
		}
	}
	edges := []*storage.Edge{edge(req.Source, string(lt), req.Target)}
	if !graph.IsSymmetric(lt) {
		inv, _ := graph.InverseLinkType(lt)
		edges = append(edges, edge(req.Target, string(inv), req.Source))
	}
	if err := s.Store.Edges().CreateAll(ctx, edges); err != nil {
		return nil, fmt.Errorf("create link: %w", err)
	}
	return edges, nil
}

// allLinkTypes is every link type, inverse names included.
func allLinkTypes() []string {
	var out []string
	for _, lt := range graph.UserLinkTypes {
		out = append(out, string(lt))
		if inv, _ := graph.InverseLinkType(lt); inv != lt {
			out = append(out, string(inv))
		}
	}
	return out
}

// DeleteLinks deletes the links between source and target, both
// directions in one transaction, and returns how many edges it deleted.
// With typ, that link and its inverse: source -[typ]-> target and target
// -[inverse]-> source. Without, every link either way. Object edges that
// are not links (mentions, alternatives) stay.
func (s *Service) DeleteLinks(ctx context.Context, source, target, typ string) (int, error) {
	if source == "" || target == "" {
		return 0, invalidLink("source and target are required")
	}
	forward, reverse := allLinkTypes(), allLinkTypes()
	if typ != "" {
		lt, err := linkType(typ)
		if err != nil {
			return 0, err
		}
		inv, _ := graph.InverseLinkType(lt)
		forward, reverse = []string{string(lt)}, []string{string(inv)}
	}
	n, err := s.Store.Edges().DeleteMatching(ctx, []storage.EdgeMatch{
		{FromType: nodeObject, FromID: source, ToType: nodeObject, ToID: target, EdgeTypes: forward},
		{FromType: nodeObject, FromID: target, ToType: nodeObject, ToID: source, EdgeTypes: reverse},
	})
	if err != nil {
		return 0, fmt.Errorf("delete links: %w", err)
	}
	return n, nil
}

// LinkQuery selects the links of one object (GET
// /api/v1/objects/{id}/edges).
type LinkQuery struct {
	ObjectID string
	// Direction is out, in or both (the default when empty).
	Direction string
	// Type keeps only edges of this type; "" keeps every type.
	Type string
	// Depth 0 lists every edge touching the object. Depth 1 to
	// MaxLinkDepth walks the links breadth first to that depth and
	// reaches each object once, through the first edge found.
	Depth int
}

// LinkHop is one edge of a link listing or traversal.
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

// ListLinks returns the object-to-object edges q selects: outbound
// before inbound, each in store order. A missing object is
// storage.ErrNotFound.
func (s *Service) ListLinks(ctx context.Context, q LinkQuery) ([]LinkHop, error) {
	switch q.Direction {
	case "":
		q.Direction = LinkBoth
	case LinkOut, LinkIn, LinkBoth:
	default:
		return nil, invalidLink("direction %q: want in, out or both", q.Direction)
	}
	if q.Depth < 0 || q.Depth > MaxLinkDepth {
		return nil, invalidLink("depth %d: want 1 to %d", q.Depth, MaxLinkDepth)
	}
	if _, err := s.GetObject(ctx, q.ObjectID); err != nil {
		return nil, err
	}
	if q.Depth == 0 {
		return s.linkHops(ctx, q, q.ObjectID, 1)
	}

	// Breadth first; an object is reached once, through the first edge
	// that finds it.
	visited := map[string]bool{q.ObjectID: true}
	frontier := []string{q.ObjectID}
	var out []LinkHop
	for depth := 1; depth <= q.Depth && len(frontier) > 0; depth++ {
		var next []string
		for _, id := range frontier {
			hops, err := s.linkHops(ctx, q, id, depth)
			if err != nil {
				return nil, err
			}
			for _, h := range hops {
				if visited[h.ObjectID] {
					continue
				}
				visited[h.ObjectID] = true
				out = append(out, h)
				next = append(next, h.ObjectID)
			}
		}
		frontier = next
	}
	return out, nil
}

// linkHops lists the object-to-object edges touching id that q's
// direction and type keep, as hops at depth.
func (s *Service) linkHops(ctx context.Context, q LinkQuery, id string, depth int) ([]LinkHop, error) {
	var out []LinkHop
	keep := func(e *storage.Edge) bool { return q.Type == "" || e.EdgeType == q.Type }
	if q.Direction != LinkIn {
		edges, err := s.Store.Edges().ListFrom(ctx, nodeObject, id)
		if err != nil {
			return nil, fmt.Errorf("list outbound links of %s: %w", id, err)
		}
		for _, e := range edges {
			if e.ToType == nodeObject && keep(e) {
				out = append(out, LinkHop{Depth: depth, From: id, Direction: LinkOut, ObjectID: e.ToID, Edge: e})
			}
		}
	}
	if q.Direction != LinkOut {
		edges, err := s.Store.Edges().ListTo(ctx, nodeObject, id)
		if err != nil {
			return nil, fmt.Errorf("list inbound links of %s: %w", id, err)
		}
		for _, e := range edges {
			if e.FromType == nodeObject && keep(e) {
				out = append(out, LinkHop{Depth: depth, From: id, Direction: LinkIn, ObjectID: e.FromID, Edge: e})
			}
		}
	}
	return out, nil
}

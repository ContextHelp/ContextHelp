package sharing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
)

// Shape is what a route's response carries (ADR-080 §8, decision D20):
// every route declares one next to its scope, and the backstop walks
// the known ID paths of that shape. It never keys on a bare field
// name: object IDs are opaque strings, and "id" also names principals,
// policies, jobs and sessions.
type Shape string

// Response shapes.
const (
	// ShapeNone carries no object, entity or edge: the backstop walks
	// nothing and only the withheld-value canary runs, which has
	// nothing to find when no object was released.
	ShapeNone Shape = "none"
	// ShapeObjects carries objects, their graphs and their edges.
	ShapeObjects Shape = "objects"
	// ShapeEntities carries entities.
	ShapeEntities Shape = "entities"
	// ShapeMixed carries both, and the search graph's nodes and edges.
	ShapeMixed Shape = "mixed"
)

// Shapes lists every shape a route may declare.
var Shapes = []Shape{ShapeNone, ShapeObjects, ShapeEntities, ShapeMixed}

// IDKind says what an ID-bearing value must be found in the ledger as.
type IDKind int

// ID kinds.
const (
	// KindObject is an object ID.
	KindObject IDKind = iota
	// KindEntity is an entity slug, or its ctxt://entity/ URI.
	KindEntity
	// KindEdge is an edge ID.
	KindEdge
	// KindEndpoint is an edge end: an object ID or an entity URI.
	KindEndpoint
	// KindNodeID is a graph node ID, <object>/<type>/<n>: its object
	// must have been released.
	KindNodeID
	// KindNodeRef is a node URI, ctxt:node/<object>/<type>/<n>.
	KindNodeRef
	// KindGraphNodeKey is a search graph node key: "query", obj:<id>
	// or ent:<slug>.
	KindGraphNodeKey
)

// IDPath is one ID-bearing path of a response shape, in the transport's
// JSON: keys separated by dots, "[]" after a key (or alone, at the
// root) for every element of an array, "{}" after a key for every key
// of a map, and "!json" after a key whose string value holds a JSON
// document the path continues into.
type IDPath struct {
	Path string
	Kind IDKind
}

// httpObjectPaths are the object-bearing paths of the REST API's
// envelopes: a single object, data and objects lists, find's
// explanations, link hops, aliases, graph nodes, derived source lists,
// and the MCP search results inside a tools/call answer.
var httpObjectPaths = []IDPath{
	{"id", KindObject},
	{"data[].id", KindObject},
	{"objects[].id", KindObject},
	{"explain[].id", KindObject},
	{"object_id", KindObject},
	{"[].object_id", KindObject},
	{"data[].object_id", KindObject},
	{"data[].from", KindObject},
	{"data[].edge.id", KindEdge},
	{"data[].edge.from_id", KindEndpoint},
	{"data[].edge.to_id", KindEndpoint},
	{"graph.nodes[].id", KindNodeID},
	{"data[].graph.nodes[].id", KindNodeID},
	{"objects[].graph.nodes[].id", KindNodeID},
	{"metadata.source_ids[]", KindObject},
	{"data[].metadata.source_ids[]", KindObject},
	{"objects[].metadata.source_ids[]", KindObject},
	{"result.content[].text!json.results[].id", KindObject},
}

// httpEntityPaths are the entity-bearing paths of the REST API.
var httpEntityPaths = []IDPath{
	{"slug", KindEntity},
	{"data[].slug", KindEntity},
	{"entities[].slug", KindEntity},
}

// httpGraphPaths are the search graph document's node keys and edge
// ends.
var httpGraphPaths = []IDPath{
	{"nodes{}", KindGraphNodeKey},
	{"edges[].source", KindGraphNodeKey},
	{"edges[].target", KindGraphNodeKey},
}

// grpcObjectPaths are the object-bearing paths of the gRPC API's
// messages in protojson with proto names.
var grpcObjectPaths = []IDPath{
	{"id", KindObject},
	{"objects[].id", KindObject},
	{"graph.nodes[].id", KindNodeID},
	{"objects[].graph.nodes[].id", KindNodeID},
	{"results[].object.id", KindObject},
	{"results[].object.graph.nodes[].id", KindNodeID},
	{"results[].node_hits[].object_id", KindObject},
	{"results[].node_hits[].node_ref", KindNodeRef},
}

// grpcEntityPaths are the entity-bearing paths of the gRPC API.
var grpcEntityPaths = []IDPath{
	{"slug", KindEntity},
	{"entities[].slug", KindEntity},
}

// HTTPPaths returns the ID paths the backstop walks for shape on the
// REST API; nil for ShapeNone.
func HTTPPaths(shape Shape) []IDPath {
	switch shape {
	case ShapeObjects:
		return httpObjectPaths
	case ShapeEntities:
		return httpEntityPaths
	case ShapeMixed:
		return concat(httpObjectPaths, httpEntityPaths, httpGraphPaths)
	}
	return nil
}

// GRPCPaths returns the ID paths the backstop walks for shape on the
// gRPC API; nil for ShapeNone.
func GRPCPaths(shape Shape) []IDPath {
	switch shape {
	case ShapeObjects:
		return grpcObjectPaths
	case ShapeEntities:
		return grpcEntityPaths
	case ShapeMixed:
		return concat(grpcObjectPaths, grpcEntityPaths)
	}
	return nil
}

func concat(lists ...[]IDPath) []IDPath {
	var out []IDPath
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// ErrNotJSON is returned by Backstop for a body that is not a JSON
// document while the shape has paths to walk: the backstop cannot
// check it, so the transport fails closed.
var ErrNotJSON = errors.New("backstop: body is not JSON")

// BackstopResult is what the backstop made of a response.
type BackstopResult struct {
	// Body is the response with every hit stripped or replaced.
	Body []byte
	// Hits describes each hit: the path and value of an ID not in the
	// ledger, or the category of a withheld value found in the body.
	Hits []string
	// Absent reports that the body's own object or entity (a root id)
	// was not in the ledger: the resource is answered as absent.
	Absent bool
}

// Backstop checks body against ledger (ADR-080 §8): every value at one
// of paths must be in the ledger, else it is stripped (its array
// element or map entry removed; a root value marks the body Absent);
// and every withheld value the ledger holds is replaced in every
// string of the body by its category token. A body that is not JSON
// is ErrNotJSON when there are paths to walk; with none, the canary
// runs on the raw text.
func Backstop(body []byte, paths []IDPath, ledger *Ledger) (BackstopResult, error) {
	b := &backstopRun{ledger: ledger, canaries: ledger.Canaries()}
	if len(bytes.TrimSpace(body)) == 0 {
		return BackstopResult{Body: body}, nil
	}
	var doc any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		if len(paths) > 0 {
			return BackstopResult{}, ErrNotJSON
		}
		out := b.canary(string(body))
		return BackstopResult{Body: []byte(out), Hits: b.hits}, nil
	}
	absent := false
	for _, p := range paths {
		var strip bool
		doc, strip = b.apply(doc, parsePath(p.Path), p.Kind, p.Path)
		if strip {
			absent = true
		}
	}
	doc = b.canaryTree(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		return BackstopResult{}, fmt.Errorf("backstop: encode: %w", err)
	}
	if len(b.hits) > 0 {
		backstopHits.Add(uint64(len(b.hits)))
	}
	return BackstopResult{Body: out, Hits: b.hits, Absent: absent}, nil
}

// backstopHits counts every hit the backstop ever stripped or replaced
// in this process.
var backstopHits atomic.Uint64

// BackstopHits reports how many hits the backstop stripped or replaced
// since the process started. Any count above zero is a bug in the
// scoped reads or the render layer to fix.
func BackstopHits() uint64 { return backstopHits.Load() }

// segment is one step of an IDPath.
type segment struct {
	key     string
	array   bool
	mapKeys bool
	json    bool
}

// parsePath splits an IDPath into segments.
func parsePath(path string) []segment {
	toks := strings.Split(path, ".")
	segs := make([]segment, 0, len(toks))
	for _, tok := range toks {
		s := segment{key: tok}
		switch {
		case tok == "[]":
			s = segment{array: true}
		case strings.HasSuffix(tok, "[]"):
			s = segment{key: strings.TrimSuffix(tok, "[]"), array: true}
		case strings.HasSuffix(tok, "{}"):
			s = segment{key: strings.TrimSuffix(tok, "{}"), mapKeys: true}
		case strings.HasSuffix(tok, "!json"):
			s = segment{key: strings.TrimSuffix(tok, "!json"), json: true}
		}
		segs = append(segs, s)
	}
	return segs
}

type backstopRun struct {
	ledger   *Ledger
	canaries []Withheld
	hits     []string
}

// allowed reports whether value may stay at a path of kind.
func (b *backstopRun) allowed(value string, kind IDKind) bool {
	if value == "" {
		return true
	}
	switch kind {
	case KindObject:
		return b.ledger.HasObject(value)
	case KindEntity:
		return b.ledger.HasEntity(value)
	case KindEdge:
		return b.ledger.HasEdge(value)
	case KindEndpoint:
		return b.ledger.HasObject(value) || b.ledger.HasEntity(value)
	case KindNodeID:
		obj, _, _ := strings.Cut(value, "/")
		return b.ledger.HasObject(obj)
	case KindNodeRef:
		obj, _, _ := strings.Cut(strings.TrimPrefix(value, "ctxt:node/"), "/")
		return b.ledger.HasObject(obj)
	case KindGraphNodeKey:
		switch {
		case value == "query":
			return true
		case strings.HasPrefix(value, "obj:"):
			return b.ledger.HasObject(strings.TrimPrefix(value, "obj:"))
		case strings.HasPrefix(value, "ent:"):
			return b.ledger.HasEntity(strings.TrimPrefix(value, "ent:"))
		}
		return false
	}
	return false
}

// apply walks node along segs and returns the node with every hit
// removed, and whether node itself must go (a hit at a leaf propagates
// up to the nearest array element or map entry, which is removed; at
// the root it marks the body absent).
func (b *backstopRun) apply(node any, segs []segment, kind IDKind, path string) (any, bool) {
	if len(segs) == 0 {
		s, ok := node.(string)
		if !ok || b.allowed(s, kind) {
			return node, false
		}
		b.hits = append(b.hits, path+"="+s)
		return nil, true
	}
	seg := segs[0]
	if seg.key == "" {
		if !seg.array {
			return node, false
		}
		return b.applyArray(node, segs[1:], kind, path)
	}
	m, ok := node.(map[string]any)
	if !ok {
		return node, false
	}
	child, present := m[seg.key]
	if !present {
		return node, false
	}
	switch {
	case seg.array:
		m[seg.key], _ = b.applyArray(child, segs[1:], kind, path)
		return m, false
	case seg.mapKeys:
		if cm, ok := child.(map[string]any); ok {
			for k := range cm {
				if !b.allowed(k, kind) {
					b.hits = append(b.hits, path+"="+k)
					delete(cm, k)
				}
			}
		}
		return m, false
	case seg.json:
		s, ok := child.(string)
		if !ok {
			return m, false
		}
		var inner any
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		if dec.Decode(&inner) != nil {
			return m, false
		}
		inner, _ = b.apply(inner, segs[1:], kind, path)
		inner = b.canaryTree(inner)
		if enc, err := json.Marshal(inner); err == nil {
			m[seg.key] = string(enc)
		}
		return m, false
	default:
		res, strip := b.apply(child, segs[1:], kind, path)
		if strip {
			return m, true
		}
		m[seg.key] = res
		return m, false
	}
}

// applyArray walks every element of node along segs, dropping the
// elements that must go.
func (b *backstopRun) applyArray(node any, segs []segment, kind IDKind, path string) (any, bool) {
	arr, ok := node.([]any)
	if !ok {
		return node, false
	}
	out := make([]any, 0, len(arr))
	for _, e := range arr {
		res, strip := b.apply(e, segs, kind, path)
		if strip {
			continue
		}
		out = append(out, res)
	}
	return out, false
}

// canaryTree replaces withheld values in every string of a decoded
// document.
func (b *backstopRun) canaryTree(node any) any {
	if len(b.canaries) == 0 {
		return node
	}
	switch x := node.(type) {
	case string:
		return b.canary(x)
	case map[string]any:
		for k, v := range x {
			x[k] = b.canaryTree(v)
		}
		return x
	case []any:
		for i, v := range x {
			x[i] = b.canaryTree(v)
		}
		return x
	}
	return node
}

// canary replaces every withheld value found in s by its category
// token, longest value first.
func (b *backstopRun) canary(s string) string {
	for _, c := range b.canaries {
		if strings.Contains(s, c.Value) {
			b.hits = append(b.hits, "canary:"+c.Category)
			s = strings.ReplaceAll(s, c.Value, "["+c.Category+"]")
		}
	}
	return s
}

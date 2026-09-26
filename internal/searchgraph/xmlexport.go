package searchgraph

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"unicode/utf8"
)

// XML exports (GraphML, GEXF) share one attribute model.
//
// JGF metadata becomes typed attributes. Nested objects are flattened by
// joining keys with "_": graph.metadata.weights.mention_boost becomes
// weights_mention_boost, node metadata score.total becomes score_total,
// counts.nodes becomes counts_nodes, caps.max_nodes becomes caps_max_nodes.
// Top-level metadata keys keep their JGF names. The flattened names never
// collide (a test enforces it).
//
// Every attribute of the contract is declared, whether or not the document
// uses it. A value is written only when present: empty strings, omitted
// optional numbers (zero ranks, nil raw scores, a nil score breakdown) and
// an absent vector_error produce no value rather than an empty one.
// Numbers that JGF always carries (pools, counts, weights, truncated) are
// always written, zero included.
//
// Text is escaped for XML. Characters that XML 1.0 cannot carry (C0
// controls other than tab, LF and CR; U+FFFE; U+FFFF) and invalid UTF-8
// bytes are replaced with U+FFFD, so hostile labels always yield
// well-formed documents. Tab, LF and CR survive: CR always, and tab and LF
// inside attribute values, are written as character references so XML
// parsers do not normalise them away.
//
// Floats use the shortest round-trip form, like encoding/json. NaN and
// infinities cannot be represented and make the encoder fail before
// anything is written.

// attrType is the type of an exported attribute.
type attrType int

const (
	attrString attrType = iota
	attrInt
	attrDouble
	attrBool
)

// graphML returns the GraphML attr.type name.
func (t attrType) graphML() string {
	switch t {
	case attrInt:
		return "int"
	case attrDouble:
		return "double"
	case attrBool:
		return "boolean"
	default:
		return "string"
	}
}

// gexf returns the GEXF attribute type name.
func (t attrType) gexf() string {
	switch t {
	case attrInt:
		return "integer"
	case attrDouble:
		return "double"
	case attrBool:
		return "boolean"
	default:
		return "string"
	}
}

// attr is one exported attribute of a T. get reports the value (string,
// int, float64 or bool) and whether it is present.
type attr[T any] struct {
	name string
	typ  attrType
	get  func(T) (any, bool)
}

func strAttr[T any](name string, f func(T) string) attr[T] {
	return attr[T]{name, attrString, func(v T) (any, bool) { s := f(v); return s, s != "" }}
}

// intAttr is always present.
func intAttr[T any](name string, f func(T) int) attr[T] {
	return attr[T]{name, attrInt, func(v T) (any, bool) { return f(v), true }}
}

// optIntAttr is absent when zero (JGF omitempty).
func optIntAttr[T any](name string, f func(T) int) attr[T] {
	return attr[T]{name, attrInt, func(v T) (any, bool) { n := f(v); return n, n != 0 }}
}

// floatAttr is always present.
func floatAttr[T any](name string, f func(T) float64) attr[T] {
	return attr[T]{name, attrDouble, func(v T) (any, bool) { return f(v), true }}
}

// ptrFloatAttr is absent when nil.
func ptrFloatAttr[T any](name string, f func(T) *float64) attr[T] {
	return attr[T]{name, attrDouble, func(v T) (any, bool) {
		p := f(v)
		if p == nil {
			return nil, false
		}
		return *p, true
	}}
}

func boolAttr[T any](name string, f func(T) bool) attr[T] {
	return attr[T]{name, attrBool, func(v T) (any, bool) { return f(v), true }}
}

// scoreAttr is present whenever the node has a score breakdown.
func scoreAttr(name string, f func(*Score) float64) attr[Node] {
	return attr[Node]{"score_" + name, attrDouble, func(n Node) (any, bool) {
		if n.Metadata.Score == nil {
			return nil, false
		}
		return f(n.Metadata.Score), true
	}}
}

// graphAttrs are the graph-level attributes: graph type and label, then
// graph.metadata flattened.
var graphAttrs = []attr[*Graph]{
	strAttr("type", func(g *Graph) string { return g.Type }),
	strAttr("label", func(g *Graph) string { return g.Label }),
	strAttr("vocabulary", func(g *Graph) string { return g.Metadata.Vocabulary }),
	strAttr("generated_at", func(g *Graph) string { return g.Metadata.GeneratedAt }),
	strAttr("query", func(g *Graph) string { return g.Metadata.Query }),
	strAttr("fts_query", func(g *Graph) string { return g.Metadata.FTSQuery }),
	strAttr("mode", func(g *Graph) string { return g.Metadata.Mode }),
	strAttr("vector_error", func(g *Graph) string { return g.Metadata.VectorError }),
	strAttr("vector_model", func(g *Graph) string { return g.Metadata.VectorModel }),
	strAttr("semantic_status", func(g *Graph) string { return g.Metadata.SemanticStatus }),
	intAttr("fts_pool", func(g *Graph) int { return g.Metadata.FTSPool }),
	intAttr("vector_pool", func(g *Graph) int { return g.Metadata.VectorPool }),
	intAttr("rrf_k", func(g *Graph) int { return g.Metadata.RRFK }),
	intAttr("limit", func(g *Graph) int { return g.Metadata.Limit }),
	floatAttr("fts_weight", func(g *Graph) float64 { return g.Metadata.FTSWeight }),
	floatAttr("vector_weight", func(g *Graph) float64 { return g.Metadata.VectorWeight }),
	floatAttr("threshold", func(g *Graph) float64 { return g.Metadata.Threshold }),
	floatAttr("weights_mention_boost", func(g *Graph) float64 { return g.Metadata.Weights.MentionBoost }),
	floatAttr("weights_max_mention_boost", func(g *Graph) float64 { return g.Metadata.Weights.MaxMentionBoost }),
	floatAttr("weights_direct_backlink", func(g *Graph) float64 { return g.Metadata.Weights.DirectBacklink }),
	floatAttr("weights_hop_backlink", func(g *Graph) float64 { return g.Metadata.Weights.HopBacklink }),
	floatAttr("weights_word_overlap", func(g *Graph) float64 { return g.Metadata.Weights.WordOverlap }),
	intAttr("counts_fts_hits", func(g *Graph) int { return g.Metadata.Counts.FTSHits }),
	intAttr("counts_vector_hits", func(g *Graph) int { return g.Metadata.Counts.VectorHits }),
	intAttr("counts_candidates", func(g *Graph) int { return g.Metadata.Counts.Candidates }),
	intAttr("counts_both", func(g *Graph) int { return g.Metadata.Counts.Both }),
	intAttr("counts_fts_only", func(g *Graph) int { return g.Metadata.Counts.FTSOnly }),
	intAttr("counts_vector_only", func(g *Graph) int { return g.Metadata.Counts.VectorOnly }),
	intAttr("counts_returned", func(g *Graph) int { return g.Metadata.Counts.Returned }),
	intAttr("counts_cut_limit", func(g *Graph) int { return g.Metadata.Counts.CutLimit }),
	intAttr("counts_cut_threshold", func(g *Graph) int { return g.Metadata.Counts.CutThreshold }),
	intAttr("counts_nodes", func(g *Graph) int { return g.Metadata.Counts.Nodes }),
	intAttr("counts_edges", func(g *Graph) int { return g.Metadata.Counts.Edges }),
	intAttr("counts_entities", func(g *Graph) int { return g.Metadata.Counts.Entities }),
	boolAttr("truncated", func(g *Graph) bool { return g.Metadata.Truncated }),
	intAttr("caps_max_nodes", func(g *Graph) int { return g.Metadata.Caps.MaxNodes }),
	intAttr("caps_max_edges", func(g *Graph) int { return g.Metadata.Caps.MaxEdges }),
}

// labelAttr is the node label as an attribute (GraphML has no native
// label; GEXF uses its label XML attribute instead).
var labelAttr = strAttr("label", func(n Node) string { return n.Label })

// nodeAttrs are node.metadata flattened.
var nodeAttrs = []attr[Node]{
	strAttr("kind", func(n Node) string { return n.Metadata.Kind }),
	strAttr("object_id", func(n Node) string { return n.Metadata.ObjectID }),
	strAttr("stage", func(n Node) string { return n.Metadata.Stage }),
	strAttr("legs", func(n Node) string { return n.Metadata.Legs }),
	optIntAttr("rank", func(n Node) int { return n.Metadata.Rank }),
	optIntAttr("fts_rank", func(n Node) int { return n.Metadata.FTSRank }),
	optIntAttr("vector_rank", func(n Node) int { return n.Metadata.VectorRank }),
	ptrFloatAttr("fts_raw", func(n Node) *float64 { return n.Metadata.FTSRaw }),
	ptrFloatAttr("vector_raw", func(n Node) *float64 { return n.Metadata.VectorRaw }),
	ptrFloatAttr("rrf", func(n Node) *float64 { return n.Metadata.RRF }),
	scoreAttr("fts", func(s *Score) float64 { return s.FTS }),
	scoreAttr("vector", func(s *Score) float64 { return s.Vector }),
	scoreAttr("mention_boost", func(s *Score) float64 { return s.MentionBoost }),
	scoreAttr("graph_relevance", func(s *Score) float64 { return s.GraphRelevance }),
	scoreAttr("word_overlap", func(s *Score) float64 { return s.WordOverlap }),
	scoreAttr("total", func(s *Score) float64 { return s.Total }),
	strAttr("object_type", func(n Node) string { return n.Metadata.ObjectType }),
	strAttr("source", func(n Node) string { return n.Metadata.Source }),
	strAttr("created_at", func(n Node) string { return n.Metadata.CreatedAt }),
	strAttr("slug", func(n Node) string { return n.Metadata.Slug }),
	optIntAttr("mention_count", func(n Node) int { return n.Metadata.MentionCount }),
}

// Edge attributes. GraphML carries all four as data; GEXF maps relation,
// directed and weight onto its native label/kind, type and weight.
var (
	edgeRelationAttr   = strAttr("relation", func(e Edge) string { return e.Relation })
	edgeDirectedAttr   = boolAttr("directed", func(e Edge) bool { return e.Directed })
	edgeDerivationAttr = strAttr("derivation", func(e Edge) string { return e.Metadata.Derivation })
	edgeWeightAttr     = ptrFloatAttr("weight", func(e Edge) *float64 { return e.Metadata.Weight })
)

// errNonFinite reports a NaN or infinite number.
var errNonFinite = errors.New("non-finite number")

// formatValue renders an attribute value.
func formatValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case int:
		return strconv.Itoa(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		return formatFloat(x)
	default:
		return "", fmt.Errorf("unsupported attribute value %T", v)
	}
}

// formatFloat renders f like encoding/json: shortest round-trip digits,
// exponent form only below 1e-6 or from 1e21.
func formatFloat(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("%w: %v", errNonFinite, f)
	}
	abs := math.Abs(f)
	format := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	b := strconv.AppendFloat(nil, f, format, -1, 64)
	if format == 'e' {
		// Clean up e-09 to e-9, as encoding/json does.
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return string(b), nil
}

// attrValue is one present, formatted attribute value.
type attrValue struct {
	name  string
	value string
}

// values formats the present attributes of v, in table order.
func values[T any](attrs []attr[T], v T) ([]attrValue, error) {
	out := make([]attrValue, 0, len(attrs))
	for _, a := range attrs {
		raw, ok := a.get(v)
		if !ok {
			continue
		}
		s, err := formatValue(raw)
		if err != nil {
			return nil, fmt.Errorf("attribute %s: %w", a.name, err)
		}
		out = append(out, attrValue{a.name, s})
	}
	return out, nil
}

// sortedNodeIDs returns the node ids in byte order (the JGF encoder's map
// key order).
func sortedNodeIDs(g *Graph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// edgeID is the stable id of the i-th edge.
func edgeID(i int) string { return "e" + strconv.Itoa(i) }

// xmlBuf accumulates a document; writes to a bytes.Buffer cannot fail.
type xmlBuf struct{ bytes.Buffer }

// raw appends markup verbatim.
func (b *xmlBuf) raw(parts ...string) {
	for _, p := range parts {
		b.WriteString(p)
	}
}

// text appends s escaped for XML element content.
func (b *xmlBuf) text(s string) { b.escape(s, false) }

// attr appends ` name="value"` with value escaped.
func (b *xmlBuf) attr(name, value string) {
	b.raw(" ", name, `="`)
	b.escape(value, true)
	b.raw(`"`)
}

// escape appends s escaped for element content or, when inAttr, for a
// double-quoted attribute value. Characters XML 1.0 cannot carry and
// invalid UTF-8 bytes become U+FFFD. CR (and, in attributes, tab and LF)
// are written as character references so parsers do not normalise them.
func (b *xmlBuf) escape(s string, inAttr bool) {
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		i += w
		switch {
		case (r == utf8.RuneError && w == 1) || !isXMLChar(r):
			b.WriteRune(utf8.RuneError)
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '\r':
			b.WriteString("&#xD;")
		case inAttr && r == '"':
			b.WriteString("&quot;")
		case inAttr && r == '\n':
			b.WriteString("&#xA;")
		case inAttr && r == '\t':
			b.WriteString("&#x9;")
		default:
			b.WriteRune(r)
		}
	}
}

// isXMLChar reports whether r is a legal XML 1.0 character (the Char
// production).
func isXMLChar(r rune) bool {
	return r == 0x09 || r == 0x0A || r == 0x0D ||
		r >= 0x20 && r <= 0xD7FF ||
		r >= 0xE000 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0x10FFFF
}

// flush writes the finished document.
func (b *xmlBuf) flush(w io.Writer, format string) error {
	if _, err := w.Write(b.Bytes()); err != nil {
		return fmt.Errorf("searchgraph: encode %s: %w", format, err)
	}
	return nil
}

const xmlDecl = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// Edge direction values shared by GraphML edgedefault and GEXF
// defaultedgetype / edge type.
const (
	xmlDirected   = "directed"
	xmlUndirected = "undirected"
)

// xmlDirection names an edge direction in both XML formats.
func xmlDirection(directed bool) string {
	if directed {
		return xmlDirected
	}
	return xmlUndirected
}

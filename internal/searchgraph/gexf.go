package searchgraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// RGB is an sRGB color.
type RGB struct{ R, G, B uint8 }

// Hex returns the color as #rrggbb.
func (c RGB) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// VizPalette is the node color and size scheme of the GEXF export.
type VizPalette struct {
	// Query and Entity color those node kinds.
	Query, Entity RGB
	// Returned, CutLimit and CutThreshold color object nodes by stage.
	Returned, CutLimit, CutThreshold RGB
	// Unknown colors a node whose kind or stage is not in the vocabulary.
	Unknown RGB

	// QuerySize and EntitySize are fixed node sizes.
	QuerySize, EntitySize float64
	// Object nodes scale linearly with score.total from ObjectMinSize (a
	// total of zero or less, or no score) to ObjectMaxSize (the highest
	// total in the graph).
	ObjectMinSize, ObjectMaxSize float64
}

// Palette is the single source of the export colors and sizes; viewers
// that should match the GEXF output read it from here.
var Palette = VizPalette{
	Query:        RGB{0xe1, 0x1d, 0x48}, // #e11d48
	Entity:       RGB{0x8b, 0x5c, 0xf6}, // #8b5cf6
	Returned:     RGB{0x25, 0x63, 0xeb}, // #2563eb
	CutLimit:     RGB{0xd9, 0x77, 0x06}, // #d97706
	CutThreshold: RGB{0x94, 0xa3, 0xb8}, // #94a3b8
	Unknown:      RGB{0x64, 0x74, 0x8b}, // #64748b

	QuerySize:     20,
	EntitySize:    6,
	ObjectMinSize: 4,
	ObjectMaxSize: 16,
}

// nodeColor picks n's color: by kind, and by stage for objects.
func (p *VizPalette) nodeColor(n Node) RGB {
	switch n.Metadata.Kind {
	case KindQuery:
		return p.Query
	case KindEntity:
		return p.Entity
	case KindObject:
		switch service.TraceStage(n.Metadata.Stage) {
		case service.TraceStageReturned:
			return p.Returned
		case service.TraceStageCutLimit:
			return p.CutLimit
		case service.TraceStageCutThreshold:
			return p.CutThreshold
		}
	}
	return p.Unknown
}

// nodeSize picks n's size; maxTotal is the highest object score.total in
// the graph. Sizes are rounded to two decimals.
func (p *VizPalette) nodeSize(n Node, maxTotal float64) float64 {
	switch n.Metadata.Kind {
	case KindQuery:
		return p.QuerySize
	case KindEntity:
		return p.EntitySize
	}
	frac := 0.0
	if s := n.Metadata.Score; s != nil && maxTotal > 0 && s.Total > 0 {
		frac = min(s.Total/maxTotal, 1)
	}
	return math.Round((p.ObjectMinSize+(p.ObjectMaxSize-p.ObjectMinSize)*frac)*100) / 100
}

// GEXF namespaces.
const (
	gexfNS    = "http://gexf.net/1.3"
	gexfVizNS = "http://gexf.net/1.3/viz"
)

// GEXF attribute id prefixes (the same as the GraphML key ids).
const (
	gexfNodeAttr = graphMLNodeKey
	gexfEdgeAttr = graphMLEdgeKey
)

// gexfEdgeAttrs are the edge attvalues; relation, directed and weight use
// native GEXF edge attributes instead.
var gexfEdgeAttrs = []attr[Edge]{edgeDerivationAttr}

// EncodeGEXF writes the document as GEXF 1.3 (https://gexf.net/) with the
// viz module, for Gephi and graphology. It validates against the GEXF 1.3
// RELAX NG schema. NetworkX reads only GEXF up to 1.2draft (and rejects
// mixed graphs); NetworkX users should load the GraphML export instead.
//
// Nodes keep their JGF ids and carry the label as the native label
// attribute. node.metadata becomes typed attvalues declared under
// <attributes class="node">, with ids n_<name> and titles <name> (types
// string, integer, double, boolean); nested metadata is flattened with
// "_" (score.total -> score_total).
//
// Edges are native mixed: the graph declares defaultedgetype="directed"
// and every edge states type="directed" or type="undirected" from JGF
// directed. The relation is both the edge label and its kind; weight,
// when present, is the native weight; derivation is the attvalue
// e_derivation. Edge ids are e0, e1, ... in document edge order.
//
// GEXF 1.3 has no graph-level attributes, so <meta> carries the graph:
// creator "ctxt", keywords = the vocabulary, lastmodifieddate = the UTC
// date of generated_at, and description = graph.metadata as compact JSON
// (the JGF object verbatim, typed; JSON escapes control characters, so
// they survive there).
//
// viz: color by kind (query, entity) and, for objects, by stage;
// size fixed for query and entity nodes and scaled by score.total for
// objects. Both come from Palette.
//
// Values follow the shared XML export rules: every contract attribute is
// declared, absent values are omitted (never written empty), floats use
// the encoding/json form, NaN or infinite numbers fail the encode before
// anything is written, and characters XML 1.0 cannot carry (C0 controls
// other than tab, LF and CR; U+FFFE; U+FFFF; invalid UTF-8) are replaced
// with U+FFFD. Nodes are written in id order; the output is deterministic.
func (d *Document) EncodeGEXF(w io.Writer) error {
	var b xmlBuf
	if err := d.writeGEXF(&b, &Palette); err != nil {
		return fmt.Errorf("searchgraph: encode gexf: %w", err)
	}
	return b.flush(w, "gexf")
}

func (d *Document) writeGEXF(b *xmlBuf, p *VizPalette) error {
	g := &d.Graph
	b.raw(xmlDecl, `<gexf xmlns="`, gexfNS, `" xmlns:viz="`, gexfVizNS, `" version="1.3">`, "\n")
	if err := writeGEXFMeta(b, &g.Metadata); err != nil {
		return err
	}
	b.raw(`  <graph mode="static" defaultedgetype="`, xmlDirection(g.Directed), `" idtype="string">`, "\n")
	writeGEXFAttributes(b, "node", gexfNodeAttr, nodeAttrs)
	writeGEXFAttributes(b, "edge", gexfEdgeAttr, gexfEdgeAttrs)

	ids := sortedNodeIDs(g)
	maxTotal := 0.0
	for _, id := range ids {
		if s := g.Nodes[id].Metadata.Score; s != nil && s.Total > maxTotal {
			maxTotal = s.Total
		}
	}
	b.raw("    <nodes>\n")
	for _, id := range ids {
		if err := writeGEXFNode(b, p, id, g.Nodes[id], maxTotal); err != nil {
			return err
		}
	}
	b.raw("    </nodes>\n    <edges>\n")
	for i, e := range g.Edges {
		if err := writeGEXFEdge(b, i, e); err != nil {
			return err
		}
	}
	b.raw("    </edges>\n  </graph>\n</gexf>\n")
	return nil
}

func writeGEXFMeta(b *xmlBuf, md *GraphMetadata) error {
	var js bytes.Buffer
	enc := json.NewEncoder(&js)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(md); err != nil {
		return fmt.Errorf("meta description: %w", err)
	}
	b.raw("  <meta")
	if t, err := time.Parse(time.RFC3339, md.GeneratedAt); err == nil {
		b.attr("lastmodifieddate", t.UTC().Format(time.DateOnly))
	}
	b.raw(">\n    <creator>ctxt</creator>\n    <keywords>")
	b.text(md.Vocabulary)
	b.raw("</keywords>\n    <description>")
	b.text(strings.TrimSuffix(js.String(), "\n"))
	b.raw("</description>\n  </meta>\n")
	return nil
}

func writeGEXFAttributes[T any](b *xmlBuf, class, prefix string, attrs []attr[T]) {
	b.raw(`    <attributes class="`, class, `" mode="static">`, "\n")
	for _, a := range attrs {
		b.raw("      <attribute")
		b.attr("id", prefix+a.name)
		b.attr("title", a.name)
		b.attr("type", a.typ.gexf())
		b.raw("/>\n")
	}
	b.raw("    </attributes>\n")
}

func writeGEXFAttvalues(b *xmlBuf, prefix string, vals []attrValue) {
	if len(vals) == 0 {
		return
	}
	b.raw("        <attvalues>\n")
	for _, v := range vals {
		b.raw(`          <attvalue for="`, prefix, v.name, `"`)
		b.attr("value", v.value)
		b.raw("/>\n")
	}
	b.raw("        </attvalues>\n")
}

func writeGEXFNode(b *xmlBuf, p *VizPalette, id string, n Node, maxTotal float64) error {
	vals, err := values(nodeAttrs, n)
	if err != nil {
		return fmt.Errorf("node %s: %w", id, err)
	}
	size, err := formatFloat(p.nodeSize(n, maxTotal))
	if err != nil {
		return fmt.Errorf("node %s size: %w", id, err)
	}
	b.raw("      <node")
	b.attr("id", id)
	if n.Label != "" {
		b.attr("label", n.Label)
	}
	b.raw(">\n")
	writeGEXFAttvalues(b, gexfNodeAttr, vals)
	c := p.nodeColor(n)
	b.raw(`        <viz:color r="`, strconv.Itoa(int(c.R)), `" g="`, strconv.Itoa(int(c.G)),
		`" b="`, strconv.Itoa(int(c.B)), `"/>`, "\n")
	b.raw(`        <viz:size value="`, size, `"/>`, "\n")
	b.raw("      </node>\n")
	return nil
}

func writeGEXFEdge(b *xmlBuf, i int, e Edge) error {
	vals, err := values(gexfEdgeAttrs, e)
	if err != nil {
		return fmt.Errorf("edge %d: %w", i, err)
	}
	typ := xmlDirection(e.Directed)
	b.raw("      <edge")
	b.attr("id", edgeID(i))
	b.attr("source", e.Source)
	b.attr("target", e.Target)
	b.attr("type", typ)
	if e.Relation != "" {
		b.attr("label", e.Relation)
		b.attr("kind", e.Relation)
	}
	if e.Metadata.Weight != nil {
		ws, err := formatFloat(*e.Metadata.Weight)
		if err != nil {
			return fmt.Errorf("edge %d weight: %w", i, err)
		}
		b.attr("weight", ws)
	}
	if len(vals) == 0 {
		b.raw("/>\n")
		return nil
	}
	b.raw(">\n")
	writeGEXFAttvalues(b, gexfEdgeAttr, vals)
	b.raw("      </edge>\n")
	return nil
}

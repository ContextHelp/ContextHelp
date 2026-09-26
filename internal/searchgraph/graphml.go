package searchgraph

import (
	"fmt"
	"io"
	"strings"
)

// GraphML key id prefixes, by domain.
const (
	graphMLGraphKey = "g_"
	graphMLNodeKey  = "n_"
	graphMLEdgeKey  = "e_"
)

// graphMLEdgeAttrs are the edge data keys, in output order.
var graphMLEdgeAttrs = []attr[Edge]{edgeRelationAttr, edgeDirectedAttr, edgeDerivationAttr, edgeWeightAttr}

// graphMLNodeAttrs are the node data keys: the label, then metadata.
var graphMLNodeAttrs = append([]attr[Node]{labelAttr}, nodeAttrs...)

// EncodeGraphML writes the document as GraphML
// (http://graphml.graphdrawing.org/), valid against the GraphML 1.0 XML
// Schema. It is the portable export: NetworkX, igraph, Gephi, yEd and
// Cytoscape all load it.
//
// Attributes are declared as typed <key> elements (attr.type string, int,
// double or boolean) whose ids carry a domain prefix: g_ for the graph,
// n_ for nodes, e_ for edges, followed by the flattened attribute name
// (n_score_total, g_counts_nodes). Graph-level metadata is written as
// <data> on the <graph>, the node label as n_label.
//
// Directedness is portable rather than native: the graph declares
// edgedefault="directed" and every edge carries a boolean e_directed
// attribute; no edge sets GraphML's per-edge directed attribute, because
// NetworkX (and igraph) reject graphs that mix directed and undirected
// edges. Consumers that support mixed graphs should read e_directed.
// Relation (e_relation), derivation (e_derivation) and weight (e_weight,
// when present) are edge data too.
//
// Node ids are the JGF ids, except that GraphML requires ids to be XML
// NMTOKENs: every byte outside [A-Za-z0-9_:-] is written as '.' followed
// by two upper-case hex digits (so "ent:search/rrf" becomes
// "ent:search.2Frrf" and '.' itself ".2E"). The mapping is stable and
// reversible; the raw ids stay available as n_object_id and n_slug. Edge
// ids are e0, e1, ... in document edge order. Nodes are written in id
// order, so the output is deterministic.
//
// Values follow the shared XML export rules: every contract attribute is
// declared, absent values are omitted (never written empty), nested
// metadata is flattened with "_" (score.total -> score_total), floats use
// the encoding/json form, NaN or infinite numbers fail the encode before anything is
// written, and characters XML 1.0 cannot carry (C0 controls other than
// tab, LF and CR; U+FFFE; U+FFFF; invalid UTF-8) are replaced with U+FFFD.
func (d *Document) EncodeGraphML(w io.Writer) error {
	var b xmlBuf
	if err := d.writeGraphML(&b); err != nil {
		return fmt.Errorf("searchgraph: encode graphml: %w", err)
	}
	return b.flush(w, "graphml")
}

func (d *Document) writeGraphML(b *xmlBuf) error {
	g := &d.Graph
	b.raw(xmlDecl,
		`<graphml xmlns="http://graphml.graphdrawing.org/xmlns"`,
		` xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"`,
		` xsi:schemaLocation="http://graphml.graphdrawing.org/xmlns http://graphml.graphdrawing.org/xmlns/1.0/graphml.xsd">`, "\n")
	writeGraphMLKeys(b, "graph", graphMLGraphKey, graphAttrs)
	writeGraphMLKeys(b, "node", graphMLNodeKey, graphMLNodeAttrs)
	writeGraphMLKeys(b, "edge", graphMLEdgeKey, graphMLEdgeAttrs)

	b.raw("  <graph")
	b.attr("id", GraphMLID(g.ID))
	b.attr("edgedefault", xmlDirection(g.Directed))
	b.raw(">\n")
	gv, err := values(graphAttrs, g)
	if err != nil {
		return fmt.Errorf("graph: %w", err)
	}
	writeGraphMLData(b, "    ", graphMLGraphKey, gv)

	for _, id := range sortedNodeIDs(g) {
		n := g.Nodes[id]
		nv, err := values(graphMLNodeAttrs, n)
		if err != nil {
			return fmt.Errorf("node %s: %w", id, err)
		}
		b.raw("    <node")
		b.attr("id", GraphMLID(id))
		b.raw(">\n")
		writeGraphMLData(b, "      ", graphMLNodeKey, nv)
		b.raw("    </node>\n")
	}
	for i, e := range g.Edges {
		ev, err := values(graphMLEdgeAttrs, e)
		if err != nil {
			return fmt.Errorf("edge %d: %w", i, err)
		}
		b.raw("    <edge")
		b.attr("id", edgeID(i))
		b.attr("source", GraphMLID(e.Source))
		b.attr("target", GraphMLID(e.Target))
		b.raw(">\n")
		writeGraphMLData(b, "      ", graphMLEdgeKey, ev)
		b.raw("    </edge>\n")
	}
	b.raw("  </graph>\n</graphml>\n")
	return nil
}

func writeGraphMLKeys[T any](b *xmlBuf, domain, prefix string, attrs []attr[T]) {
	for _, a := range attrs {
		b.raw("  <key")
		b.attr("id", prefix+a.name)
		b.attr("for", domain)
		b.attr("attr.name", a.name)
		b.attr("attr.type", a.typ.graphML())
		b.raw("/>\n")
	}
}

func writeGraphMLData(b *xmlBuf, indent, prefix string, vals []attrValue) {
	for _, v := range vals {
		b.raw(indent, `<data key="`, prefix, v.name, `">`)
		b.text(v.value)
		b.raw("</data>\n")
	}
}

// GraphMLID maps a JGF node id to the GraphML (NMTOKEN) id EncodeGraphML
// writes: bytes outside [A-Za-z0-9_:-] become '.' plus two upper-case hex
// digits. The mapping is injective, so distinct JGF ids never collide.
func GraphMLID(id string) string {
	const hex = "0123456789ABCDEF"
	var sb strings.Builder
	for i := range len(id) {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '_' || c == ':' || c == '-' {
			sb.WriteByte(c)
			continue
		}
		sb.WriteByte('.')
		sb.WriteByte(hex[c>>4])
		sb.WriteByte(hex[c&0x0f])
	}
	return sb.String()
}

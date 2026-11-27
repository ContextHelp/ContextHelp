package pipeline

import (
	"maps"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// graphMark is what a graph held before a step ran: each node ID and edge
// key with the stamp it carried ("" for none).
type graphMark struct {
	nodes map[string]string
	edges map[string]string
}

// edgeKey identifies an edge within a graph by its ID, or by its endpoints and
// type when it has none: a step keeps the IDs it gives its edges, so the ID
// follows an edge through the run. (heldKey, in the service, adds endpoints
// and type: it compares edges people and importers wrote.)
func edgeKey(e storage.GraphEdge) string {
	if e.ID != "" {
		return e.ID
	}
	return e.FromID + "\x00" + e.EdgeType + "\x00" + e.ToID
}

func markGraph(g *storage.ObjectGraph) graphMark {
	m := graphMark{nodes: map[string]string{}, edges: map[string]string{}}
	if g == nil {
		return m
	}
	for _, n := range g.Nodes {
		m.nodes[n.ID] = n.ProducedBy()
	}
	for _, e := range g.Edges {
		m.edges[edgeKey(e)] = e.ProducedBy()
	}
	return m
}

// withStamp returns meta with ProducedByKey set to step, or removed when
// step is "". meta is copied: it may be shared with another structure (a
// section's metadata).
func withStamp(meta map[string]any, step string) map[string]any {
	out := make(map[string]any, len(meta)+1)
	maps.Copy(out, meta)
	if step == "" {
		delete(out, storage.ProducedByKey)
		if len(out) == 0 {
			return nil
		}
		return out
	}
	out[storage.ProducedByKey] = step
	return out
}

func stampOf(meta map[string]any) string {
	s, _ := meta[storage.ProducedByKey].(string)
	return s
}

// settleGraph makes g the graph a step leaves behind. A node or edge not in
// before is the step's: it gets the step's name. One that was there keeps
// the stamp it had (or none), whatever the step wrote: a step cannot
// remove or change another's stamp, nor a rewrite of the graph wipe them.
// The graph is marked Stamped. It returns g, or a new graph when g is nil.
func settleGraph(g *storage.ObjectGraph, before graphMark, step string) *storage.ObjectGraph {
	if g == nil {
		g = &storage.ObjectGraph{}
	}
	g.Stamped = true
	for i := range g.Nodes {
		want := step
		if prev, ok := before.nodes[g.Nodes[i].ID]; ok {
			want = prev
		}
		if stampOf(g.Nodes[i].Metadata) != want {
			g.Nodes[i].Metadata = withStamp(g.Nodes[i].Metadata, want)
		}
	}
	for i := range g.Edges {
		want := step
		if prev, ok := before.edges[edgeKey(g.Edges[i])]; ok {
			want = prev
		}
		if stampOf(g.Edges[i].Metadata) != want {
			g.Edges[i].Metadata = withStamp(g.Edges[i].Metadata, want)
		}
	}
	return g
}

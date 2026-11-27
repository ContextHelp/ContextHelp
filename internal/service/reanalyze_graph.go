package service

import (
	"slices"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// stepNames is the set of step names of pipe.
func stepNames(pipe *pipeline.Pipeline) map[string]bool {
	out := map[string]bool{}
	for _, st := range pipe.Steps {
		out[st.Name()] = true
	}
	return out
}

// reanalyzeGraph returns the graph the steps start from: a copy of the
// stored graph without what the steps about to run (ran) will rebuild.
//
// A node or edge stamped with a step name was produced by that step: it is
// dropped when the step runs again and kept otherwise. An unstamped one was
// added by an importer, a client, a person or the attached original: it is
// always kept. An edge no step rebuilds whose endpoint is dropped is held back
// (heldEdge) and put back by reattachHeldEdges if that endpoint exists again.
//
// A legacy object (its graph is not marked Stamped) has no stamp to read: it
// gets legacyReanalyzeGraph once, and the run that follows marks the graph
// and stamps its step nodes.
func reanalyzeGraph(obj *storage.KnowledgeObject, produced, ran map[string]bool) (*storage.ObjectGraph, heldSet) {
	g := obj.Graph
	if g == nil {
		return nil, heldSet{}
	}
	if !g.Stamped {
		return legacyReanalyzeGraph(g, produced), heldSet{}
	}
	out := &storage.ObjectGraph{Stamped: true}
	dropped := map[string]nodeSig{}
	before := map[nodeSig]int{}
	for _, n := range g.Nodes {
		before[nodeSig{n.NodeType, n.Label}]++
		if ran[n.ProducedBy()] {
			dropped[n.ID] = nodeSig{n.NodeType, n.Label}
			continue
		}
		n.Metadata = storageutil.CloneMetadata(n.Metadata)
		out.Nodes = append(out.Nodes, n)
	}
	var held []heldEdge
	for _, e := range g.Edges {
		if ran[e.ProducedBy()] {
			continue
		}
		e.Metadata = storageutil.CloneMetadata(e.Metadata)
		ends := map[string]nodeSig{}
		for _, id := range []string{e.FromID, e.ToID} {
			if sig, ok := dropped[id]; ok {
				ends[id] = sig
			}
		}
		if len(ends) > 0 {
			held = append(held, heldEdge{e, ends})
			continue
		}
		out.Edges = append(out.Edges, e)
	}
	return out, heldSet{edges: held, dropped: dropped, before: before}
}

// nodeSig identifies a node across a reanalysis: its ID, type and label.
type nodeSig struct{ nodeType, label string }

// heldEdge is an edge no step rebuilds whose endpoint a step is about to
// rebuild: it waits for the run, with the endpoints it needs back.
type heldEdge struct {
	edge storage.GraphEdge
	ends map[string]nodeSig // dropped endpoints, by ID
}

// heldSet is what a reanalysis holds back: the edges, every node the steps
// drop, as they were, and how many nodes carried each type and label before
// the run.
type heldSet struct {
	edges   []heldEdge
	dropped map[string]nodeSig
	before  map[nodeSig]int
}

// reattachHeldEdges puts back on the graph the held edges whose dropped
// endpoints exist again, with the same ID, type and label. The rest are
// dropped: what they pointed at is no longer there, or moved. When a node
// of a type moved in this run and the endpoint's label is carried by two
// nodes of that type, before the run or after it, the label cannot tell
// them apart: no edge is put back on it.
func reattachHeldEdges(g *storage.ObjectGraph, held heldSet) {
	if g == nil {
		return
	}
	moved := movedTypes(g, held.dropped)
	labels := map[nodeSig]int{}
	for _, n := range g.Nodes {
		labels[nodeSig{n.NodeType, n.Label}]++
	}
	have := map[string]bool{}
	for _, e := range g.Edges {
		have[heldKey(e)] = true
	}
	for _, h := range held.edges {
		back := true
		for id, sig := range h.ends {
			n := g.FindNode(id)
			same := n != nil && n.NodeType == sig.nodeType && n.Label == sig.label
			shared := labels[sig] >= 2 || held.before[sig] >= 2
			if !same || (moved[sig.nodeType] && shared) {
				back = false
			}
		}
		if back && !have[heldKey(h.edge)] {
			g.Edges = append(g.Edges, h.edge)
		}
	}
}

// movedTypes are the node types where a dropped node was not rebuilt as it
// was: its ID is gone, or holds another type or label now.
func movedTypes(g *storage.ObjectGraph, dropped map[string]nodeSig) map[string]bool {
	out := map[string]bool{}
	for id, sig := range dropped {
		if n := g.FindNode(id); n == nil || n.NodeType != sig.nodeType || n.Label != sig.label {
			out[sig.nodeType] = true
		}
	}
	return out
}

// legacyReanalyzeGraph is the policy for a graph with no stamps: it is kept
// as stored unless some step produces the graph (the Graph, Sections, Tags
// or Mentions field), and then everything but the entity_mention nodes
// (and their edges), which hold mention provenance, is dropped, unless a
// step produces Mentions. Nodes carry no record of their creator, so
// importer and user nodes of a step's kind are dropped too.
func legacyReanalyzeGraph(g *storage.ObjectGraph, produced map[string]bool) *storage.ObjectGraph {
	cp := &storage.ObjectGraph{Edges: slices.Clone(g.Edges)}
	for _, n := range g.Nodes {
		n.Metadata = storageutil.CloneMetadata(n.Metadata)
		cp.Nodes = append(cp.Nodes, n)
	}
	if !produced["Graph"] && !produced["Sections"] && !produced["Tags"] && !produced["Mentions"] {
		return cp
	}
	out := &storage.ObjectGraph{}
	kept := map[string]bool{}
	for _, n := range cp.Nodes {
		if n.NodeType == storage.NodeTypeEntityMention && !produced["Mentions"] {
			out.Nodes = append(out.Nodes, n)
			kept[n.ID] = true
		}
	}
	for _, e := range cp.Edges {
		if kept[e.FromID] {
			out.Edges = append(out.Edges, e)
		}
	}
	if len(out.Nodes) == 0 {
		return nil
	}
	return out
}

// heldKey identifies an edge for reattachment by ID, endpoints and type: held
// edges come from people and importers, who may leave the ID empty or reuse
// it, so an ID alone (the pipeline's edgeKey) could equate edges that differ.
func heldKey(e storage.GraphEdge) string {
	return e.ID + "\x00" + e.FromID + "\x00" + e.ToID + "\x00" + e.EdgeType
}

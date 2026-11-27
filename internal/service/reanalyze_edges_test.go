package service

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A user edge to a node a step rebuilds comes back with it; when what is
// rebuilt at that ID is another node, the edge goes.
func TestReanalyzeReattachesForeignEdges(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	id := obj.ID
	section := id + "/section/0"
	o := e.get(t, id)
	require.NotNil(t, o.Graph.FindNode(section))
	o.Graph.Nodes = append(o.Graph.Nodes, storage.GraphNode{ID: id + "/decision/90", NodeType: storage.NodeTypeDecision, Label: "mine"})
	o.Graph.Edges = append(o.Graph.Edges, storage.GraphEdge{ID: "user-edge", FromID: id + "/decision/90", ToID: section, EdgeType: storage.EdgeTypeReferences})
	e.update(t, o)
	hasEdge := func() bool {
		return slices.ContainsFunc(e.get(t, id).Graph.Edges, func(ed storage.GraphEdge) bool { return ed.ID == "user-edge" })
	}

	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
	require.NoError(t, err)
	assert.True(t, hasEdge(), "the edge to a rebuilt node was lost")

	o = e.get(t, id)
	o.Graph.FindNode(section).Label = "renamed"
	e.update(t, o)
	_, _, err = e.svc.ReanalyzeObject(ownerCtx(), id)
	require.NoError(t, err)
	assert.False(t, hasEdge(), "the edge was put back on a node that is not the one it pointed at")
}

// A foreign edge to a step node survives a reanalysis that rebuilds the
// node, with a foreign node of another type at a high ordinal beside it.
func TestReanalyzeKeepsForeignEdgesToRebuiltNodes(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	id := obj.ID
	o := e.get(t, id)
	var decision string
	for _, n := range o.Graph.Nodes {
		if n.NodeType == storage.NodeTypeDecision && n.ProducedBy() == "graph_extractor" {
			decision = n.ID
		}
	}
	require.NotEmpty(t, decision, "fixture has no graph_extractor decision")
	o.Graph.Nodes = append(o.Graph.Nodes,
		storage.GraphNode{ID: id + "/section/90", NodeType: storage.NodeTypeSection, Label: "foreign"},
		storage.GraphNode{ID: id + "/tag/95", NodeType: storage.NodeTypeTag, Label: "foreign"},
		storage.GraphNode{ID: id + "/decision/90", NodeType: storage.NodeTypeDecision, Label: "mine"})
	o.Graph.Edges = append(o.Graph.Edges,
		storage.GraphEdge{ID: "to-section", FromID: id + "/decision/90", ToID: id + "/section/0", EdgeType: storage.EdgeTypeReferences},
		storage.GraphEdge{ID: "to-decision", FromID: id + "/tag/95", ToID: decision, EdgeType: storage.EdgeTypeReferences})
	e.update(t, o)
	for run := 1; run <= 2; run++ {
		_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
		require.NoError(t, err)
		got := e.get(t, id)
		assert.NotNil(t, got.Graph.FindNode(id+"/section/0"), "run %d: section/0 moved", run)
		assert.NotNil(t, got.Graph.FindNode(decision), "run %d: the decision moved", run)
		for _, eid := range []string{"to-section", "to-decision"} {
			assert.True(t, slices.ContainsFunc(got.Graph.Edges, func(ed storage.GraphEdge) bool { return ed.ID == eid }), "run %d: edge %s lost", run, eid)
		}
	}
}

// A held edge without an ID is not mistaken for a regenerated edge that
// shares its endpoints but has another type.
func TestReattachKeysOnEdgeType(t *testing.T) {
	g := &storage.ObjectGraph{
		Nodes: []storage.GraphNode{{ID: "a", NodeType: "section", Label: "x"}, {ID: "b", NodeType: "section", Label: "y"}},
		Edges: []storage.GraphEdge{{FromID: "a", ToID: "b", EdgeType: "contains"}},
	}
	held := heldSet{edges: []heldEdge{{edge: storage.GraphEdge{FromID: "a", ToID: "b", EdgeType: "references"}, ends: map[string]nodeSig{"a": {"section", "x"}}}}}
	reattachHeldEdges(g, held)
	assert.Len(t, g.Edges, 2)
}

// An edge to a node that moved is dropped, never put on the node that took
// its ID.
func TestReanalyzeDropsAnEdgeToAMovedNode(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	id := obj.ID
	o := e.get(t, id)
	moved := o.Graph.FindNode(id + "/tag/1")
	require.NotNil(t, moved)
	movedLabel := moved.Label
	// tag/0 becomes a foreign node: the tagger's nodes move up past it.
	n0 := o.Graph.FindNode(id + "/tag/0")
	n0.Label = "imported"
	delete(n0.Metadata, storage.ProducedByKey)
	o.Graph.Nodes = append(o.Graph.Nodes, storage.GraphNode{ID: id + "/decision/90", NodeType: storage.NodeTypeDecision, Label: "mine"})
	o.Graph.Edges = append(o.Graph.Edges, storage.GraphEdge{ID: "to-moved", FromID: id + "/decision/90", ToID: moved.ID, EdgeType: storage.EdgeTypeReferences})
	e.update(t, o)
	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
	require.NoError(t, err)
	got := e.get(t, id)
	now := got.Graph.FindNode(moved.ID)
	require.NotNil(t, now)
	require.NotEqual(t, movedLabel, now.Label, "the node did not move: the check is vacuous")
	for _, ed := range got.Graph.Edges {
		assert.NotEqual(t, "to-moved", ed.ID, "the edge was put on the node that took the ID")
	}
}

// With two rebuilt nodes of a type that share a label, and a node of the
// type that moved, a label cannot tell the nodes apart: no held edge goes
// back onto that type.
func TestReattachSkipsAnAmbiguousType(t *testing.T) {
	e := newRealEnv(t)
	body := "## Dup\n\nalpha body words here\n\n## Dup\n\nbeta body words here\n\n## Dup\n\ngamma body words here\n"
	e.createStoredWith(t, "amb1", body, "text.long@v0", &storage.ObjectGraph{Stamped: true,
		Nodes: []storage.GraphNode{
			{ID: "amb1/section/1", NodeType: storage.NodeTypeSection, Label: "foreign"},
			{ID: "amb1/section/0", NodeType: storage.NodeTypeSection, Label: "Dup", Metadata: map[string]any{storage.ProducedByKey: "sectioner"}},
			{ID: "amb1/section/2", NodeType: storage.NodeTypeSection, Label: "Dup", Metadata: map[string]any{storage.ProducedByKey: "sectioner"}},
			{ID: "amb1/section/4", NodeType: storage.NodeTypeSection, Label: "Dup", Metadata: map[string]any{storage.ProducedByKey: "sectioner"}},
			{ID: "amb1/decision/90", NodeType: storage.NodeTypeDecision, Label: "mine"},
		},
		Edges: []storage.GraphEdge{{ID: "to-dup", FromID: "amb1/decision/90", ToID: "amb1/section/2", EdgeType: storage.EdgeTypeReferences}},
	})
	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), "amb1")
	require.NoError(t, err)
	got := e.get(t, "amb1")
	require.GreaterOrEqual(t, countByLabel(got.Graph, "Dup"), 2, "the sections are not titled alike: the check is vacuous")
	assert.False(t, slices.ContainsFunc(got.Graph.Edges, func(ed storage.GraphEdge) bool { return ed.ID == "to-dup" }))
}

// A changed LLM reply is the normal case: a held edge to a node rebuilt
// as it was comes back although another node of its type changed and the
// type holds a label twice elsewhere.
func TestReattachKeepsAnEdgeToAnUnambiguousNodeAfterAChangedReply(t *testing.T) {
	e := newRealEnv(t)
	e.llm.answer("concise tags", "llmtagone, llmtagtwo, fixedtopic")
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	id := obj.ID
	o := e.get(t, id)
	var target *storage.GraphNode
	for i, n := range o.Graph.Nodes {
		if n.NodeType == storage.NodeTypeTag && n.Label == "llmtagone" {
			target = &o.Graph.Nodes[i]
		}
	}
	require.NotNil(t, target, "fixture has no llmtagone tag")
	targetID := target.ID
	o.Graph.Nodes = append(o.Graph.Nodes, storage.GraphNode{ID: id + "/decision/90", NodeType: storage.NodeTypeDecision, Label: "mine"})
	o.Graph.Edges = append(o.Graph.Edges, storage.GraphEdge{ID: "to-tag", FromID: id + "/decision/90", ToID: targetID, EdgeType: storage.EdgeTypeReferences})
	e.update(t, o)

	e.llm.mu.Lock()
	e.llm.replies = nil
	e.llm.mu.Unlock()
	e.llm.answer("concise tags", "llmtagone, llmtagthree, fixedtopic")
	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
	require.NoError(t, err)
	got := e.get(t, id)
	n := got.Graph.FindNode(targetID)
	require.NotNil(t, n)
	assert.Equal(t, "llmtagone", n.Label, "the tag moved: the check is vacuous")
	assert.Equal(t, 2, countByLabel(got.Graph, "fixedtopic"), "the type holds no repeated label: the check is vacuous")
	assert.Zero(t, countByLabel(got.Graph, "llmtagtwo"), "the reply did not change: the check is vacuous")
	assert.True(t, slices.ContainsFunc(got.Graph.Edges, func(ed storage.GraphEdge) bool { return ed.ID == "to-tag" }), "the edge to an unambiguous node was dropped")
}

// Two sections shared a title before the run and one is left after it (the
// content was edited): the survivor's ID and label match the edge's old
// endpoint, but it may be the other section. The edge is not put back.
func TestReattachSkipsALabelThatWasSharedBeforeTheRun(t *testing.T) {
	e := newRealEnv(t)
	e.createStoredWith(t, "amb2", "## Dup\n\nbeta body words here\n", "text.long@v0", &storage.ObjectGraph{Stamped: true,
		Nodes: []storage.GraphNode{
			{ID: "amb2/section/0", NodeType: storage.NodeTypeSection, Label: "Dup", Metadata: map[string]any{storage.ProducedByKey: "sectioner"}},
			{ID: "amb2/section/1", NodeType: storage.NodeTypeSection, Label: "Dup", Metadata: map[string]any{storage.ProducedByKey: "sectioner"}},
			{ID: "amb2/decision/90", NodeType: storage.NodeTypeDecision, Label: "mine"},
		},
		Edges: []storage.GraphEdge{{ID: "to-alpha", FromID: "amb2/decision/90", ToID: "amb2/section/0", EdgeType: storage.EdgeTypeReferences}},
	})
	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), "amb2")
	require.NoError(t, err)
	got := e.get(t, "amb2")
	n := got.Graph.FindNode("amb2/section/0")
	require.NotNil(t, n, "no section came back at the old ID: the check is vacuous")
	require.Equal(t, "Dup", n.Label)
	require.Equal(t, 1, countByLabel(got.Graph, "Dup"), "the title is still shared after the run: the check is vacuous")
	assert.False(t, slices.ContainsFunc(got.Graph.Edges, func(ed storage.GraphEdge) bool { return ed.ID == "to-alpha" }))
}

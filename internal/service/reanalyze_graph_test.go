package service

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// stampedSet lists the nodes and edges of g that carry a stamp, with
// their content, so two runs can be compared without ordering.
func stampedSet(g *storage.ObjectGraph) []string {
	var out []string
	if g == nil {
		return nil
	}
	for _, n := range g.Nodes {
		if n.ProducedBy() != "" {
			out = append(out, fmt.Sprintf("node %s %s %q %q by %s", n.ID, n.NodeType, n.Label, n.Content, n.ProducedBy()))
		}
	}
	for _, e := range g.Edges {
		if e.ProducedBy() != "" {
			out = append(out, fmt.Sprintf("edge %s by %s", e.ID, e.ProducedBy()))
		}
	}
	slices.Sort(out)
	return out
}

func unstampedIDs(g *storage.ObjectGraph) []string {
	var out []string
	for _, n := range g.Nodes {
		if n.ProducedBy() == "" {
			out = append(out, n.ID)
		}
	}
	slices.Sort(out)
	return out
}

func hasNode(g *storage.ObjectGraph, id string) bool { return g.FindNode(id) != nil }

func countType(g *storage.ObjectGraph, typ string) int {
	n := 0
	for _, nd := range g.Nodes {
		if nd.NodeType == typ {
			n++
		}
	}
	return n
}

func (e *realEnv) update(t *testing.T, o *storage.KnowledgeObject) {
	t.Helper()
	require.NoError(t, e.driver.Objects().Update(ownerCtx(), o))
}

func pipelineSteps(t *testing.T, e *realEnv, name string) map[string]bool {
	t.Helper()
	p, err := e.reg.Get(name)
	require.NoError(t, err)
	return stepNames(p)
}

// Nodes and edges steps add at ingest carry the step's name; nodes that
// were there before the steps do not, and the stamps survive the store.
func TestGraphStampedAtIngest(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	got := e.get(t, obj.ID)
	require.NotNil(t, got.Graph)
	ran := pipelineSteps(t, e, "text.long")
	for _, n := range got.Graph.Nodes {
		assert.True(t, ran[n.ProducedBy()], "node %s stamped %q, not a step of the pipeline", n.ID, n.ProducedBy())
	}
	for _, ed := range got.Graph.Edges {
		assert.True(t, ran[ed.ProducedBy()], "edge %s stamped %q", ed.ID, ed.ProducedBy())
	}
	for _, typ := range []string{storage.NodeTypeSection, storage.NodeTypeTag, storage.NodeTypeEntityMention} {
		assert.Positive(t, countType(got.Graph, typ), "fixture has no %s node", typ)
	}

	// A node there before the steps run stays unstamped.
	pipe, err := e.reg.Get("text.long")
	require.NoError(t, err)
	pre := storage.GraphNode{ID: "pre1/section/0", NodeType: storage.NodeTypeSection, Label: "imported"}
	draft := &storage.KnowledgeObject{ID: "pre1", Type: "text", RawContent: invText, Source: "s", Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{pre}}}
	out, err := pipeline.RunSteps(ownerCtx(), pipe, draft)
	require.NoError(t, err)
	assert.Equal(t, []string{"pre1/section/0"}, unstampedIDs(out.Graph))
	require.NoError(t, e.driver.Objects().Create(ownerCtx(), out))
	assert.Equal(t, stampedSet(out.Graph), stampedSet(e.get(t, "pre1").Graph), "stamps lost in the store")
}

// seedForeignNodes adds nodes no step made, each of a type steps produce.
func seedForeignNodes(t *testing.T, e *realEnv, id string, withMention bool) {
	t.Helper()
	o := e.get(t, id)
	keep := mentions.ParseSlice([]string{"keep.me"})
	if withMention {
		o.Mentions = append(o.Mentions, keep...)
		o.Graph.Nodes = append(o.Graph.Nodes, storage.GraphNode{ID: id + "/entity_mention/90", NodeType: storage.NodeTypeEntityMention, Label: keep[0].String(),
			Metadata: map[string]any{storage.ProvenanceSourceField: storage.MentionSourceMentions}})
	}
	o.Graph.Nodes = append(o.Graph.Nodes,
		storage.GraphNode{ID: id + "/section/90", NodeType: storage.NodeTypeSection, Label: "imported", Order: 90},
		storage.GraphNode{ID: id + "/tag/90", NodeType: storage.NodeTypeTag, Label: "client"},
		storage.GraphNode{ID: id + "/decision/90", NodeType: storage.NodeTypeDecision, Label: "mine"},
	)
	o.Graph.Edges = append(o.Graph.Edges, storage.GraphEdge{ID: "foreign", FromID: id + "/section/90", ToID: id + "/tag/90", EdgeType: storage.EdgeTypeReferences})
	e.update(t, o)
}

var foreignIDs = []string{"/section/90", "/tag/90", "/decision/90"}

func assertForeignKept(t *testing.T, g *storage.ObjectGraph, id string, withMention bool) {
	t.Helper()
	for _, s := range foreignIDs {
		n := g.FindNode(id + s)
		if assert.NotNil(t, n, "node %s was dropped", id+s) {
			assert.Empty(t, n.ProducedBy(), "node %s was stamped", id+s)
		}
	}
	if withMention {
		if n := g.FindNode(id + "/entity_mention/90"); assert.NotNil(t, n, "user mention node dropped") {
			assert.Empty(t, n.ProducedBy())
		}
	}
	assert.True(t, slices.ContainsFunc(g.Edges, func(ed storage.GraphEdge) bool { return ed.ID == "foreign" }), "foreign edge dropped")
}

// Nodes no step made survive reanalysis; the step-made ones are rebuilt,
// the same count and without duplicates, run after run.
func TestReanalyzeKeepsForeignNodesAndRegeneratesStepNodes(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	seedForeignNodes(t, e, obj.ID, true)
	before := e.get(t, obj.ID)
	want := stampedShape(before.Graph)
	require.NotEmpty(t, want)

	for run := 1; run <= 2; run++ {
		_, _, err := e.svc.ReanalyzeObject(ownerCtx(), obj.ID)
		require.NoError(t, err)
		after := e.get(t, obj.ID)
		assert.Equal(t, want, stampedShape(after.Graph), "run %d: step nodes not regenerated as they were", run)
		assertForeignKept(t, after.Graph, obj.ID, true)
		assert.Equal(t, len(before.Graph.Nodes), len(after.Graph.Nodes), "run %d: node count", run)
		assert.Equal(t, len(before.Graph.Edges), len(after.Graph.Edges), "run %d: edge count", run)
		seen := map[string]bool{}
		for _, n := range after.Graph.Nodes {
			assert.False(t, seen[n.ID], "duplicate node %s", n.ID)
			seen[n.ID] = true
		}
		got := mentionSet(after.Mentions)
		for _, m := range mentionSet(before.Mentions) {
			assert.Contains(t, got, m, "mention lost")
		}
	}
}

// A step that is no longer in the pipeline cannot regenerate its nodes:
// they stay.
func TestReanalyzeKeepsNodesOfAStepNoLongerRun(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	before := e.get(t, obj.ID)
	pipe, err := e.reg.Get("text.long")
	require.NoError(t, err)
	var kept []pipeline.PipelineStep
	for _, st := range pipe.Steps {
		if st.Name() != "tagger" {
			kept = append(kept, st)
		}
	}
	require.Less(t, len(kept), len(pipe.Steps), "text.long has no tagger")
	e.reg.Upsert("text.long", &pipeline.Pipeline{PipelineName: "text.long", Steps: kept})

	_, _, err = e.svc.ReanalyzeObject(ownerCtx(), obj.ID)
	require.NoError(t, err)
	after := e.get(t, obj.ID)
	tagNodes := func(g *storage.ObjectGraph) (out []string) {
		for _, n := range g.Nodes {
			if n.ProducedBy() == "tagger" {
				out = append(out, n.ID+n.Label)
			}
		}
		slices.Sort(out)
		return out
	}
	require.NotEmpty(t, tagNodes(before.Graph), "fixture has no tagger nodes")
	assert.Equal(t, tagNodes(before.Graph), tagNodes(after.Graph))
}

// An object derived before nodes were stamped has no stamp to read: the
// first reanalysis rebuilds its step kinds (importer nodes of those kinds
// go), stamps what it builds, and the next one is exact.
func TestReanalyzeLegacyObject(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	o := e.get(t, obj.ID)
	for i := range o.Graph.Nodes {
		delete(o.Graph.Nodes[i].Metadata, storage.ProducedByKey)
	}
	for i := range o.Graph.Edges {
		delete(o.Graph.Edges[i].Metadata, storage.ProducedByKey)
	}
	o.Graph.Stamped = false
	require.False(t, o.Graph.Stamped)
	// An update cannot unmark a graph (the store keeps the mark): replace the row.
	require.NoError(t, e.driver.Objects().Delete(ownerCtx(), obj.ID))
	require.NoError(t, e.driver.Objects().Create(ownerCtx(), o))
	seedForeignNodes(t, e, obj.ID, true)

	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), obj.ID)
	require.NoError(t, err)
	first := e.get(t, obj.ID)
	assert.True(t, first.Graph.Stamped, "the first reanalysis left the graph unmarked")
	assert.NotEmpty(t, stampedShape(first.Graph), "the first reanalysis stamped nothing")
	assert.False(t, hasNode(first.Graph, obj.ID+"/section/90"), "interim policy: a node of a step's kind is rebuilt")

	seedForeignNodes(t, e, obj.ID, false)
	want := stampedShape(e.get(t, obj.ID).Graph)
	_, _, err = e.svc.ReanalyzeObject(ownerCtx(), obj.ID)
	require.NoError(t, err)
	second := e.get(t, obj.ID)
	assertForeignKept(t, second.Graph, obj.ID, false)
	assert.Equal(t, want, stampedShape(second.Graph))
}

// The attached original is never dropped, and its blob still resolves.
func TestReanalyzeKeepsOriginalNode(t *testing.T) {
	e := newRealEnv(t)
	fx := invFixtures["image.ocr"](t, e)
	id := invIngest(t, e, "image.ocr", fx)
	before := e.get(t, id)
	orig, ok := blob.OriginalOf(before)
	require.True(t, ok)
	node := before.Graph.FindNode(storage.NewNodeID(id, storage.NodeTypeArtifact, 0))
	require.NotNil(t, node)
	assert.Empty(t, node.ProducedBy(), "the original is not a step's node")

	for range 2 {
		_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
		require.NoError(t, err)
		after := e.get(t, id)
		n := after.Graph.FindNode(node.ID)
		require.NotNil(t, n, "original node dropped")
		assert.Empty(t, n.ProducedBy())
		got, ok := blob.OriginalOf(after)
		require.True(t, ok)
		assert.Equal(t, orig.Key, got.Key)
		content, err := blob.Content(context.Background(), e.driver.Blobs(), blobref.For(got.Key))
		require.NoError(t, err, "original blob no longer resolves")
		assert.Equal(t, string(fx.binary), content)
		assert.Equal(t, len(before.Graph.Nodes), len(after.Graph.Nodes))
	}
}

// stampedShape is stampedSet without ids: a regenerated node takes the
// next free ordinal, which a kept node of its type may have moved.
func stampedShape(g *storage.ObjectGraph) []string {
	var out []string
	if g == nil {
		return nil
	}
	for _, n := range g.Nodes {
		if n.ProducedBy() != "" {
			out = append(out, fmt.Sprintf("node %s %q %q by %s", n.NodeType, n.Label, n.Content, n.ProducedBy()))
		}
	}
	for _, e := range g.Edges {
		if e.ProducedBy() != "" {
			out = append(out, "edge by "+e.ProducedBy())
		}
	}
	slices.Sort(out)
	return out
}

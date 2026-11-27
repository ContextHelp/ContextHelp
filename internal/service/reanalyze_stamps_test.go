package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// rewriteStep stands for an external step: it replaces the graph with
// copies of its nodes that carry no metadata, and adds a node.
type rewriteStep struct{ pipeline.BaseContract }

func (rewriteStep) Name() string { return "external_rewrite" }

func (rewriteStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	g := &storage.ObjectGraph{}
	if d.Graph != nil {
		for _, n := range d.Graph.Nodes {
			g.Nodes = append(g.Nodes, storage.GraphNode{ID: n.ID, NodeType: n.NodeType, Label: n.Label, Content: n.Content, Order: n.Order})
		}
		for _, e := range d.Graph.Edges {
			g.Edges = append(g.Edges, storage.GraphEdge{ID: e.ID, FromID: e.FromID, ToID: e.ToID, EdgeType: e.EdgeType})
		}
	}
	g.Nodes = append(g.Nodes, storage.GraphNode{
		ID: storage.NewNodeID(d.ID, storage.NodeTypeSummary, storage.FreeOrdinal(g, d.ID, storage.NodeTypeSummary, 0)), NodeType: storage.NodeTypeSummary, Label: "ext",
	})
	d.Graph = g
	return d, nil
}

func (e *realEnv) withTrailingStep(t *testing.T, name string, st pipeline.PipelineStep) {
	t.Helper()
	pipe, err := e.reg.Get(name)
	require.NoError(t, err)
	e.reg.Upsert(name, &pipeline.Pipeline{PipelineName: name, Steps: append(slices.Clone(pipe.Steps), st)})
}

// A step that rewrites the graph without metadata does not make the
// stamps of earlier steps vanish: node counts stay put across reanalyses.
func TestReanalyzeStableWithAStepThatRewritesTheGraph(t *testing.T) {
	e := newRealEnv(t)
	e.withTrailingStep(t, "text.long", rewriteStep{pipeline.NewBaseContract(pipeline.StepContract{})})
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	before := e.get(t, obj.ID)
	n, sections := len(before.Graph.Nodes), countType(before.Graph, storage.NodeTypeSection)
	for run := 1; run <= 2; run++ {
		_, _, err := e.svc.ReanalyzeObject(ownerCtx(), obj.ID)
		require.NoError(t, err)
		after := e.get(t, obj.ID)
		assert.Equal(t, n, len(after.Graph.Nodes), "run %d: node count", run)
		assert.Equal(t, sections, countType(after.Graph, storage.NodeTypeSection), "run %d: sections", run)
		assert.Equal(t, stampedShape(before.Graph), stampedShape(after.Graph), "run %d", run)
	}
}

func (e *realEnv) createStored(t *testing.T, id, pipeline string, g *storage.ObjectGraph) {
	t.Helper()
	e.createStoredWith(t, id, invText, pipeline, g)
}

func (e *realEnv) createStoredWith(t *testing.T, id, content, pipeline string, g *storage.ObjectGraph) {
	t.Helper()
	require.NoError(t, e.driver.Objects().Create(ownerCtx(), &storage.KnowledgeObject{
		ID: id, Type: "text", RawContent: content, Source: "s", Pipeline: pipeline, Graph: g,
	}))
}

// An object is legacy until its first reanalysis, once: the run marks the graph even when no step adds a
// node.
func TestReanalyzeMarksTheGraphOnce(t *testing.T) {
	e := newRealEnv(t)
	e.createStored(t, "leg1", "text.long@v0", nil)
	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), "leg1")
	require.NoError(t, err)
	assert.True(t, e.get(t, "leg1").Graph.Stamped)

	// A marked graph with only a foreign node: the node stays, nothing is
	// duplicated.
	e.createStored(t, "leg2", "text.long", &storage.ObjectGraph{Stamped: true, Nodes: []storage.GraphNode{
		{ID: "leg2/section/90", NodeType: storage.NodeTypeSection, Label: "imported", Order: 90},
	}})
	for range 2 {
		_, _, err = e.svc.ReanalyzeObject(ownerCtx(), "leg2")
		require.NoError(t, err)
		got := e.get(t, "leg2")
		assert.NotNil(t, got.Graph.FindNode("leg2/section/90"), "foreign node dropped")
		assert.Equal(t, 1, countByLabel(got.Graph, "imported"))
	}
}

func countByLabel(g *storage.ObjectGraph, label string) int {
	n := 0
	for _, nd := range g.Nodes {
		if nd.Label == label {
			n++
		}
	}
	return n
}

// The mark belongs to the store: an update carrying a graph the client
// built, without the mark, leaves the stored graph marked.
func TestUpdateWithAnUnmarkedGraphKeepsTheMark(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	id := obj.ID
	o := e.get(t, id)
	require.True(t, o.Graph.Stamped)
	g := *o.Graph
	g.Stamped = false
	g.Nodes = append(slices.Clone(g.Nodes), storage.GraphNode{ID: id + "/section/90", NodeType: storage.NodeTypeSection, Label: "client"})
	o.Graph = &g
	e.update(t, o)
	assert.True(t, e.get(t, id).Graph.Stamped, "the client's graph turned a marked graph into a legacy one")
	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), id)
	require.NoError(t, err)
	assert.NotNil(t, e.get(t, id).Graph.FindNode(id+"/section/90"), "the coarse rule dropped the client's node")
}

// reprocessLLM answers the tagger's prompt with one tag more than the
// ingest LLM did.
type reprocessLLM struct{}

func (reprocessLLM) Name() string { return "fake" }

func (reprocessLLM) Generate(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "concise tags") {
		return "llmtagone, llmtagtwo, extratag", nil
	}
	return "{}", nil
}

// A reprocess job stamps what its step adds like any run, so a later
// reanalysis rebuilds it: the step nodes equal those of a fresh ingest
// that got the same LLM replies, and no tag node is unstamped.
func TestReprocessStampsWhatItAdds(t *testing.T) {
	e := newRealEnv(t)
	obj := e.ingestJob(t, "text.long", "src", invText, false)
	payload, err := json.Marshal(jobs.ReprocessPayload{ObjectID: obj.ID, Step: jobs.StepTagger})
	require.NoError(t, err)
	_, err = (&jobs.Reprocessor{Objects: e.svc, LLM: reprocessLLM{}}).Handle(ownerCtx(), &storage.Job{Payload: string(payload)})
	require.NoError(t, err)
	got := e.get(t, obj.ID)
	hasExtra := false
	for _, n := range got.Graph.Nodes {
		if n.NodeType == storage.NodeTypeTag {
			assert.NotEmpty(t, n.ProducedBy(), "tag node %s (%s) is unstamped", n.ID, n.Label)
			hasExtra = hasExtra || n.Label == "extratag"
		}
	}
	require.True(t, hasExtra, "the reprocess added no tag: the check is vacuous")

	e.llm.answer("concise tags", "llmtagone, llmtagtwo, extratag")
	_, _, err = e.svc.ReanalyzeObject(ownerCtx(), obj.ID)
	require.NoError(t, err)

	fresh := newRealEnv(t)
	fresh.llm.answer("concise tags", "llmtagone, llmtagtwo, extratag")
	f := fresh.ingestJob(t, "text.long", "src", invText, false)
	assert.Equal(t, stampedShape(fresh.get(t, f.ID).Graph), stampedShape(e.get(t, obj.ID).Graph))
}

// A graph stored before stamping stays unmarked through a reprocess (its
// unstamped step nodes must not turn foreign); the next reanalysis applies
// the earlier rule once, leaves no label twice, and marks the graph.
func TestReprocessLeavesALegacyGraphUnmarked(t *testing.T) {
	e := newRealEnv(t)
	e.createStored(t, "lg1", "text.long@v0", &storage.ObjectGraph{Nodes: []storage.GraphNode{
		{ID: "lg1/tag/0", NodeType: storage.NodeTypeTag, Label: "llmtagone"},
		{ID: "lg1/tag/1", NodeType: storage.NodeTypeTag, Label: "llmtagtwo"},
	}})
	payload, err := json.Marshal(jobs.ReprocessPayload{ObjectID: "lg1", Step: jobs.StepTagger})
	require.NoError(t, err)
	_, err = (&jobs.Reprocessor{Objects: e.svc, LLM: reprocessLLM{}}).Handle(ownerCtx(), &storage.Job{Payload: string(payload)})
	require.NoError(t, err)
	got := e.get(t, "lg1")
	require.NotNil(t, got.Graph)
	assert.False(t, got.Graph.Stamped, "the reprocess marked a legacy graph")

	_, _, err = e.svc.ReanalyzeObject(ownerCtx(), "lg1")
	require.NoError(t, err)
	got = e.get(t, "lg1")
	assert.True(t, got.Graph.Stamped)
	seen := map[string]bool{}
	for _, n := range got.Graph.Nodes {
		if n.NodeType == storage.NodeTypeTag {
			assert.False(t, seen[n.Label], "tag %q appears twice", n.Label)
			seen[n.Label] = true
		}
	}
}

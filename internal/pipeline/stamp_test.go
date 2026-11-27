package pipeline_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type graphStep struct {
	pipeline.BaseContract
	name string
	run  func(d *storage.KnowledgeObject)
}

func (s graphStep) Name() string { return s.name }
func (s graphStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	s.run(d)
	return d, nil
}

func newGraphStep(name string, run func(d *storage.KnowledgeObject)) graphStep {
	return graphStep{pipeline.NewBaseContract(pipeline.StepContract{}), name, run}
}

func TestRunStepsStampsWhatEachStepAdds(t *testing.T) {
	shared := map[string]any{"k": "v"}
	pre := storage.GraphNode{ID: "o/section/0", NodeType: "section"}
	draft := &storage.KnowledgeObject{ID: "o", Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{pre}}}
	a := newGraphStep("a", func(d *storage.KnowledgeObject) {
		d.Graph.Nodes = append(d.Graph.Nodes, storage.GraphNode{ID: "o/tag/0", NodeType: "tag", Metadata: shared})
		d.Graph.Edges = append(d.Graph.Edges, storage.GraphEdge{ID: "e1", FromID: "o/section/0", ToID: "o/tag/0"})
	})
	b := newGraphStep("b", func(d *storage.KnowledgeObject) {
		d.Graph.Nodes[0].Label = "edited" // modified, not added
		d.Graph.Nodes[1].Label = "edited by b"
		d.Graph.Nodes = append(d.Graph.Nodes, storage.GraphNode{ID: "o/tag/1", NodeType: "tag"})
	})
	out, err := pipeline.RunSteps(context.Background(), &pipeline.Pipeline{Steps: []pipeline.PipelineStep{a, b}}, draft)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"o/section/0": "", "o/tag/0": "a", "o/tag/1": "b"}
	for _, n := range out.Graph.Nodes {
		if got := n.ProducedBy(); got != want[n.ID] {
			t.Errorf("node %s stamped %q, want %q", n.ID, got, want[n.ID])
		}
	}
	if got := out.Graph.Edges[0].ProducedBy(); got != "a" {
		t.Errorf("edge stamped %q, want a", got)
	}
	if _, ok := shared[storage.ProducedByKey]; ok {
		t.Error("the stamp was written into a metadata map the step shares")
	}
}

// A step that rewrites the graph (an external step replaces it wholesale)
// cannot wipe, change or add the stamp of a node it did not add.
func TestRunStepsKeepsStampsThroughARewrite(t *testing.T) {
	draft := &storage.KnowledgeObject{ID: "o", Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{{ID: "o/section/0", NodeType: "section"}}}}
	a := newGraphStep("a", func(d *storage.KnowledgeObject) {
		d.Graph.Nodes = append(d.Graph.Nodes, storage.GraphNode{ID: "o/tag/0", NodeType: "tag"})
		d.Graph.Edges = append(d.Graph.Edges, storage.GraphEdge{ID: "e1", FromID: "o/section/0", ToID: "o/tag/0"})
	})
	rewrite := newGraphStep("ext", func(d *storage.KnowledgeObject) {
		g := &storage.ObjectGraph{}
		for _, n := range d.Graph.Nodes {
			g.Nodes = append(g.Nodes, storage.GraphNode{ID: n.ID, NodeType: n.NodeType, Metadata: map[string]any{storage.ProducedByKey: "forged"}})
		}
		for _, e := range d.Graph.Edges {
			g.Edges = append(g.Edges, storage.GraphEdge{ID: e.ID, FromID: e.FromID, ToID: e.ToID})
		}
		g.Nodes = append(g.Nodes, storage.GraphNode{ID: "o/tag/1", NodeType: "tag"})
		d.Graph = g
	})
	out, err := pipeline.RunSteps(context.Background(), &pipeline.Pipeline{Steps: []pipeline.PipelineStep{a, rewrite}}, draft)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"o/section/0": "", "o/tag/0": "a", "o/tag/1": "ext"}
	for _, n := range out.Graph.Nodes {
		if got := n.ProducedBy(); got != want[n.ID] {
			t.Errorf("node %s stamped %q, want %q", n.ID, got, want[n.ID])
		}
	}
	if got := out.Graph.Edges[0].ProducedBy(); got != "a" {
		t.Errorf("edge stamped %q, want a", got)
	}
	if !out.Graph.Stamped {
		t.Error("graph not marked Stamped")
	}
}

// The graph is marked whether or not a step adds a node, and a step that
// drops or replaces the graph does not remove the mark.
func TestRunStepsMarksTheGraphStamped(t *testing.T) {
	noop := newGraphStep("noop", func(*storage.KnowledgeObject) {})
	out, err := pipeline.RunSteps(context.Background(), &pipeline.Pipeline{Steps: []pipeline.PipelineStep{noop}}, &storage.KnowledgeObject{ID: "o"})
	if err != nil || out.Graph == nil || !out.Graph.Stamped {
		t.Fatalf("a run that adds no node left graph %+v (err %v)", out.Graph, err)
	}
	for name, run := range map[string]func(*storage.KnowledgeObject){
		"nil":   func(d *storage.KnowledgeObject) { d.Graph = nil },
		"fresh": func(d *storage.KnowledgeObject) { d.Graph = &storage.ObjectGraph{} },
	} {
		out, err := pipeline.RunSteps(context.Background(), &pipeline.Pipeline{Steps: []pipeline.PipelineStep{newGraphStep(name, run)}}, &storage.KnowledgeObject{ID: "o"})
		if err != nil || out.Graph == nil || !out.Graph.Stamped {
			t.Errorf("%s: graph %+v (err %v)", name, out.Graph, err)
		}
	}
}

// A step that changes the graph in place and then delegates still leaves
// its nodes stamped.
func TestRunStepsStampsWhatADelegatingStepAdded(t *testing.T) {
	d := newDelegator()
	out, err := pipeline.RunSteps(context.Background(), &pipeline.Pipeline{Steps: []pipeline.PipelineStep{d}}, &storage.KnowledgeObject{ID: "o", Graph: &storage.ObjectGraph{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Graph.Nodes) != 1 || out.Graph.Nodes[0].ProducedBy() != "delegator" {
		t.Errorf("nodes = %+v", out.Graph.Nodes)
	}
}

type delegator struct{ pipeline.BaseContract }

func newDelegator() delegator  { return delegator{pipeline.NewBaseContract(pipeline.StepContract{})} }
func (delegator) Name() string { return "delegator" }
func (delegator) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	d.Graph.Nodes = append(d.Graph.Nodes, storage.GraphNode{ID: "o/tag/0", NodeType: "tag"})
	return d, pipeline.ErrDelegate
}

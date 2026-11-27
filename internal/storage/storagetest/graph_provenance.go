package storagetest

import (
	"context"
	"reflect"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// GraphProvenanceConformance pins that the step stamp on graph nodes and
// edges survives a create, an update and a read: a node or edge without a
// stamp stays without one.
func GraphProvenanceConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	graph := func(label string) *storage.ObjectGraph {
		return &storage.ObjectGraph{
			Nodes: []storage.GraphNode{
				{ID: "gp1/section/0", NodeType: storage.NodeTypeSection, Label: label},
				{ID: "gp1/tag/0", NodeType: storage.NodeTypeTag, Label: "t", Metadata: map[string]any{storage.ProducedByKey: "tagger"}},
			},
			Edges: []storage.GraphEdge{
				{ID: "gp1/section/0->gp1/tag/0", FromID: "gp1/section/0", ToID: "gp1/tag/0", EdgeType: storage.EdgeTypeContains,
					Metadata: map[string]any{storage.ProducedByKey: "sectioner"}},
				{ID: "plain", FromID: "gp1/tag/0", ToID: "gp1/section/0", EdgeType: storage.EdgeTypeReferences},
			},
		}
	}
	obj := &storage.KnowledgeObject{ID: "gp1", Type: "note", RawContent: "graph provenance", Graph: graph("first")}
	if err := drv.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}
	check := func(when string) {
		t.Helper()
		got, err := drv.Objects().Get(ctx, "gp1")
		if err != nil || got == nil || got.Graph == nil {
			t.Fatalf("%s: get: %v %v", when, got, err)
		}
		stamps := func(g *storage.ObjectGraph) []string {
			out := make([]string, 0, len(g.Nodes)+len(g.Edges))
			for _, n := range g.Nodes {
				out = append(out, n.ID+"="+n.ProducedBy())
			}
			for _, e := range g.Edges {
				out = append(out, e.ID+"="+e.ProducedBy())
			}
			return out
		}
		want := []string{"gp1/section/0=", "gp1/tag/0=tagger", "gp1/section/0->gp1/tag/0=sectioner", "plain="}
		if g := stamps(got.Graph); !reflect.DeepEqual(g, want) {
			t.Errorf("%s: stamps = %v, want %v", when, g, want)
		}
	}
	check("after create")
	obj.Graph = graph("second")
	if err := drv.Objects().Update(ctx, obj); err != nil {
		t.Fatalf("update: %v", err)
	}
	check("after update")
}

// GraphMarkerConformance pins that the Stamped mark of a graph is stored
// and read back with the graph, a graph with no node included, and that a
// graph without it reads back without it.
func GraphMarkerConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	cases := map[string]*storage.ObjectGraph{
		"gm-empty":    {Stamped: true},
		"gm-nodes":    {Stamped: true, Nodes: []storage.GraphNode{{ID: "gm-nodes/tag/0", NodeType: storage.NodeTypeTag, Label: "t"}}},
		"gm-unmarked": {Nodes: []storage.GraphNode{{ID: "gm-unmarked/tag/0", NodeType: storage.NodeTypeTag, Label: "t"}}},
	}
	for id, g := range cases {
		obj := &storage.KnowledgeObject{ID: id, Type: "note", RawContent: "marker " + id, Graph: g}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("%s: create: %v", id, err)
		}
		got, err := drv.Objects().Get(ctx, id)
		if err != nil || got == nil || got.Graph == nil {
			t.Fatalf("%s: get: %+v %v", id, got, err)
		}
		if got.Graph.Stamped != g.Stamped {
			t.Errorf("%s: Stamped = %v, want %v", id, got.Graph.Stamped, g.Stamped)
		}
		// An update that keeps the graph keeps the mark.
		got.RawContent += " edited"
		if err := drv.Objects().Update(ctx, got); err != nil {
			t.Fatalf("%s: update: %v", id, err)
		}
		again, err := drv.Objects().Get(ctx, id)
		if err != nil || again == nil || again.Graph == nil || again.Graph.Stamped != g.Stamped {
			t.Errorf("%s: after update graph = %+v (%v)", id, again, err)
		}
	}
}

// GraphMarkOwnershipConformance pins that the mark is the store's: an
// update whose graph carries no mark leaves a marked stored graph marked,
// and one that carries it marks an unmarked stored graph.
func GraphMarkOwnershipConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	obj := &storage.KnowledgeObject{ID: "go-1", Type: "note", RawContent: "owner", Graph: &storage.ObjectGraph{Stamped: true}}
	if err := drv.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}
	obj.Graph = &storage.ObjectGraph{Nodes: []storage.GraphNode{{ID: "go-1/tag/0", NodeType: storage.NodeTypeTag, Label: "client"}}}
	if err := drv.Objects().Update(ctx, obj); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := drv.Objects().Get(ctx, "go-1")
	if err != nil || got == nil || got.Graph == nil || !got.Graph.Stamped || len(got.Graph.Nodes) != 1 {
		t.Fatalf("an unmarked update unmarked the graph: %+v (%v)", got, err)
	}

	plain := &storage.KnowledgeObject{ID: "go-2", Type: "note", RawContent: "plain", Graph: &storage.ObjectGraph{}}
	if err := drv.Objects().Create(ctx, plain); err != nil {
		t.Fatalf("create: %v", err)
	}
	plain.Graph = &storage.ObjectGraph{Stamped: true}
	if err := drv.Objects().Update(ctx, plain); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = drv.Objects().Get(ctx, "go-2")
	if err != nil || got == nil || got.Graph == nil || !got.Graph.Stamped {
		t.Fatalf("a marked update did not mark the graph: %+v (%v)", got, err)
	}
}

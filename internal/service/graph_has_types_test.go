package service

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// graphHasTypes backs SearchObjectsNodeAware's in-process filter: a graph
// matches when it has a node of a wanted type and an edge of a wanted type,
// an empty set matches anything, and a nil graph never matches.
func TestGraphHasTypes(t *testing.T) {
	g := &storage.ObjectGraph{
		Nodes: []storage.GraphNode{{NodeType: storage.NodeTypeSection}, {NodeType: storage.NodeTypeTag}},
		Edges: []storage.GraphEdge{{EdgeType: storage.EdgeTypeContains}},
	}
	set := func(vs ...string) map[string]bool {
		m := map[string]bool{}
		for _, v := range vs {
			m[v] = true
		}
		return m
	}
	for _, tc := range []struct {
		name         string
		g            *storage.ObjectGraph
		nodes, edges map[string]bool
		want         bool
	}{
		{"nil graph", nil, set(), set(), false},
		{"empty sets", g, set(), set(), true},
		{"node type present", g, set(storage.NodeTypeTag), set(), true},
		{"node type absent", g, set(storage.NodeTypeDecision), set(), false},
		{"any node type present", g, set(storage.NodeTypeDecision, storage.NodeTypeSection), set(), true},
		{"edge type present", g, set(), set(storage.EdgeTypeContains), true},
		{"edge type absent", g, set(), set(storage.EdgeTypeReferences), false},
		{"node present, edge absent", g, set(storage.NodeTypeTag), set(storage.EdgeTypeReferences), false},
		{"node absent, edge present", g, set(storage.NodeTypeDecision), set(storage.EdgeTypeContains), false},
		{"both present", g, set(storage.NodeTypeSection), set(storage.EdgeTypeContains), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := graphHasTypes(tc.g, tc.nodes, tc.edges); got != tc.want {
				t.Errorf("graphHasTypes = %v, want %v", got, tc.want)
			}
		})
	}
}

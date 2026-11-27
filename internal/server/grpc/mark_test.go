package grpc

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A graph holding only the stamp mark is sent as no graph.
func TestKoToProtoOmitsAMarkOnlyGraph(t *testing.T) {
	if p := koToProto(&storage.KnowledgeObject{ID: "o", Graph: &storage.ObjectGraph{Stamped: true}}); p.Graph != nil {
		t.Errorf("graph = %+v", p.Graph)
	}
	p := koToProto(&storage.KnowledgeObject{ID: "o", Graph: &storage.ObjectGraph{Stamped: true, Nodes: []storage.GraphNode{{ID: "o/tag/0", NodeType: "tag"}}}})
	if p.Graph == nil || len(p.Graph.Nodes) != 1 {
		t.Errorf("graph = %+v", p.Graph)
	}
}

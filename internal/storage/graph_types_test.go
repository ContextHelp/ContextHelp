package storage_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestObjectGraph_AddAndFindNode(t *testing.T) {
	g := &storage.ObjectGraph{}
	node := storage.GraphNode{
		ID:       storage.NewNodeID("obj-1", storage.NodeTypeSection, 0),
		NodeType: storage.NodeTypeSection,
		Content:  "Introduction",
	}
	g.Nodes = append(g.Nodes, node)
	found := g.FindNode(node.ID)
	if found == nil {
		t.Fatal("node not found")
	}
	if found.Content != "Introduction" {
		t.Errorf("got %q", found.Content)
	}
}

func TestKnowledgeObject_HasGraphField(t *testing.T) {
	ko := storage.KnowledgeObject{}
	ko.Graph = &storage.ObjectGraph{}
	if ko.Graph == nil {
		t.Fatal("Graph field missing")
	}
}

func TestDocumentProjection_HasSections(t *testing.T) {
	dp := storage.DocumentProjection{
		Title:    "My Doc",
		Body:     "body text",
		Sections: []storage.Section{{Title: "S1", Content: "c1"}},
	}
	if dp.Title != "My Doc" || dp.Body != "body text" || len(dp.Sections) != 1 {
		t.Fatalf("DocumentProjection fields not retained: %+v", dp)
	}
}

func TestIndexProjection_HasFTSBody(t *testing.T) {
	ip := storage.IndexProjection{
		FTSBody:       "search text",
		Tags:          []storage.Tag{{Label: "go"}},
		EmbeddingText: "embed text",
	}
	if ip.FTSBody == "" {
		t.Fatal("FTSBody missing")
	}
	if len(ip.Tags) != 1 || ip.Tags[0].Label != "go" {
		t.Fatalf("Tags: got %+v", ip.Tags)
	}
	if ip.EmbeddingText != "embed text" {
		t.Fatalf("EmbeddingText: got %q", ip.EmbeddingText)
	}
}

func TestObjectGraph_FindNode_NilReceiver(t *testing.T) {
	var g *storage.ObjectGraph
	if g.FindNode("any-id") != nil {
		t.Fatal("expected nil from nil receiver")
	}
}

func TestObjectGraph_FindNode_NotFound(t *testing.T) {
	g := &storage.ObjectGraph{}
	if g.FindNode("missing") != nil {
		t.Fatal("expected nil for missing node")
	}
}

func TestGraphNode_HasOrderAndLabel(t *testing.T) {
	n := storage.GraphNode{
		ID:       storage.NewNodeID("obj-1", storage.NodeTypeSection, 2),
		NodeType: storage.NodeTypeSection,
		Label:    "Introduction",
		Order:    2,
	}
	if n.ID != "obj-1/section/2" {
		t.Errorf("ID: got %q", n.ID)
	}
	if n.NodeType != storage.NodeTypeSection {
		t.Errorf("NodeType: got %q", n.NodeType)
	}
	if n.Order != 2 {
		t.Errorf("Order: got %d", n.Order)
	}
	if n.Label != "Introduction" {
		t.Errorf("Label: got %q", n.Label)
	}
}

func TestIndexProjection_HasMentions(t *testing.T) {
	ip := storage.IndexProjection{
		Mentions: []string{"@people.alice", "@project.foo"},
	}
	if len(ip.Mentions) != 2 {
		t.Fatalf("want 2 mentions, got %d", len(ip.Mentions))
	}
}

package pluginapi_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestObjectGraph_AddAndFindNode(t *testing.T) {
	g := &pluginapi.ObjectGraph{}
	node := pluginapi.GraphNode{
		ID:       pluginapi.NewNodeID("obj-1", pluginapi.NodeTypeSection, 0),
		NodeType: pluginapi.NodeTypeSection,
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
	ko := pluginapi.KnowledgeObject{}
	ko.Graph = &pluginapi.ObjectGraph{}
	if ko.Graph == nil {
		t.Fatal("Graph field missing")
	}
}

func TestDocumentProjection_HasSections(t *testing.T) {
	dp := pluginapi.DocumentProjection{
		Title:    "My Doc",
		Body:     "body text",
		Sections: []pluginapi.Section{{Title: "S1", Content: "c1"}},
	}
	if len(dp.Sections) != 1 {
		t.Fatal("Sections missing")
	}
}

func TestIndexProjection_HasFTSBody(t *testing.T) {
	ip := pluginapi.IndexProjection{
		FTSBody:       "search text",
		Tags:          []pluginapi.Tag{{Label: "go"}},
		EmbeddingText: "embed text",
	}
	if ip.FTSBody == "" {
		t.Fatal("FTSBody missing")
	}
}

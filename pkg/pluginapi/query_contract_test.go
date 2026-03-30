package pluginapi_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestNodeHit_Fields(t *testing.T) {
	h := pluginapi.NodeHit{
		ObjectID: "obj-1",
		NodeRef:  pluginapi.NodeURI("obj-1", pluginapi.NodeTypeSection, 0),
		NodeType: pluginapi.NodeTypeSection,
		Snippet:  "some text",
		Score:    0.9,
	}
	if h.ObjectID == "" {
		t.Fatal("ObjectID empty")
	}
	if h.NodeRef == "" {
		t.Fatal("NodeRef empty")
	}
}

func TestNodeAwareFilter_Fields(t *testing.T) {
	f := pluginapi.NodeAwareFilter{
		NodeTypes:      []string{pluginapi.NodeTypeDecision},
		EdgeTypes:      []string{pluginapi.EdgeTypeContains},
		ReturnNodeHits: true,
	}
	if len(f.NodeTypes) != 1 {
		t.Fatal("NodeTypes not set")
	}
	if !f.ReturnNodeHits {
		t.Fatal("ReturnNodeHits not set")
	}
}

func TestNodeAwareResult_Fields(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{ID: "obj-1"}
	r := pluginapi.NodeAwareResult{
		Object: ko,
		NodeHits: []pluginapi.NodeHit{
			{ObjectID: "obj-1", NodeRef: "ctxt:node/obj-1/section/0"},
		},
	}
	if r.Object == nil {
		t.Fatal("Object nil")
	}
	if len(r.NodeHits) != 1 {
		t.Fatal("NodeHits empty")
	}
}

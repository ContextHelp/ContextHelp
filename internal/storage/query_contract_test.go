package storage_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestNodeHit_Fields(t *testing.T) {
	h := storage.NodeHit{
		ObjectID: "obj-1",
		NodeRef:  storage.NodeURI("obj-1", storage.NodeTypeSection, 0),
		NodeType: storage.NodeTypeSection,
		Snippet:  "some text",
		Score:    0.9,
	}
	if h.ObjectID == "" {
		t.Fatal("ObjectID empty")
	}
	if h.NodeRef == "" {
		t.Fatal("NodeRef empty")
	}
	if h.NodeType != storage.NodeTypeSection || h.Snippet != "some text" || h.Score != 0.9 {
		t.Fatalf("NodeHit fields: got %+v", h)
	}
}

func TestNodeAwareFilter_Fields(t *testing.T) {
	f := storage.NodeAwareFilter{
		NodeTypes:      []string{storage.NodeTypeDecision},
		EdgeTypes:      []string{storage.EdgeTypeContains},
		ReturnNodeHits: true,
	}
	if len(f.NodeTypes) != 1 {
		t.Fatal("NodeTypes not set")
	}
	if len(f.EdgeTypes) != 1 || f.EdgeTypes[0] != storage.EdgeTypeContains {
		t.Fatalf("EdgeTypes: got %v", f.EdgeTypes)
	}
	if !f.ReturnNodeHits {
		t.Fatal("ReturnNodeHits not set")
	}
}

func TestNodeAwareResult_Fields(t *testing.T) {
	ko := &storage.KnowledgeObject{ID: "obj-1"}
	r := storage.NodeAwareResult{
		Object: ko,
		NodeHits: []storage.NodeHit{
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

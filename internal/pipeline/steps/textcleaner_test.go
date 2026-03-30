package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestTextCleanerNormalizesSpaces(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "hello    world\t\ttabs"}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "hello world tabs" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerNormalizesNewlines(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "a\n\n\n\nb"}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "a\n\nb" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerTrimsWhitespace(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "  hello  "}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "hello" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerCRLF(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "line1\r\nline2"}
	got, _ := step.Run(context.Background(), draft)
	if got.RawContent != "line1\nline2" {
		t.Errorf("got %q", got.RawContent)
	}
}

func TestTextCleanerEmitsGraphSummaryNode(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{
		ID:         "obj-001",
		RawContent: "  hello   world  ",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected summary node")
	}
	wantID := pluginapi.NewNodeID("obj-001", pluginapi.NodeTypeSummary, 0)
	node := got.Graph.FindNode(wantID)
	if node == nil {
		t.Fatalf("summary node %q not found in graph", wantID)
	}
	if node.NodeType != pluginapi.NodeTypeSummary {
		t.Errorf("node type: got %q, want %q", node.NodeType, pluginapi.NodeTypeSummary)
	}
	if node.Content != "hello world" {
		t.Errorf("node content: got %q, want %q", node.Content, "hello world")
	}
}

func TestTextCleanerNoGraphWithoutID(t *testing.T) {
	step := NewTextCleaner()
	draft := &storage.KnowledgeObject{RawContent: "some text"}
	got, _ := step.Run(context.Background(), draft)
	if got.Graph != nil {
		t.Error("expected nil graph when ID is empty")
	}
}

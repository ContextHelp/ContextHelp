package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestCommentExtractorFindsAnnotations(t *testing.T) {
	step := NewCommentExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "package main\n\n// TODO: fix this later\nfunc main() {\n\t// FIXME: this crashes\n\t// HACK: workaround\n\t// NOTE: important\n}",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["annotation_count"] != 4 {
		t.Errorf("annotation_count: %v", got.Metadata["annotation_count"])
	}
	if len(got.Sections) != 4 {
		t.Errorf("expected 4 sections, got %d", len(got.Sections))
	}
	for _, sec := range got.Sections {
		if sec.Metadata["type"] != "comment" {
			t.Errorf("section type: %v", sec.Metadata["type"])
		}
	}
}

func TestCommentExtractorNoAnnotations(t *testing.T) {
	step := NewCommentExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "// regular comment\nvar x = 1",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["annotation_count"] != 0 {
		t.Errorf("annotation_count: %v", got.Metadata["annotation_count"])
	}
}

func TestCommentExtractorName(t *testing.T) {
	step := NewCommentExtractor()
	if step.Name() != "comment_extractor" {
		t.Errorf("name: %q", step.Name())
	}
}

func TestCommentExtractorEmitsGraphNodes(t *testing.T) {
	step := NewCommentExtractor()
	draft := &storage.KnowledgeObject{
		ID:         "obj-ce1",
		RawContent: "// TODO: fix this\n// FIXME: crashes here\n// NOTE: important",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected intra-object nodes")
	}
	// Three annotation nodes expected.
	if len(got.Graph.Nodes) != 3 {
		t.Errorf("nodes: got %d, want 3", len(got.Graph.Nodes))
	}
	for i, n := range got.Graph.Nodes {
		if n.NodeType != pluginapi.NodeTypeTask {
			t.Errorf("node[%d] type: got %q, want %q", i, n.NodeType, pluginapi.NodeTypeTask)
		}
		wantID := pluginapi.NewNodeID("obj-ce1", pluginapi.NodeTypeTask, i)
		if n.ID != wantID {
			t.Errorf("node[%d] id: got %q, want %q", i, n.ID, wantID)
		}
	}
	// No inter-object storage.Edge written — graph only (boundary: ADR-063).
	// (No edges table written in this step; tested by absence of edge store.)
}

func TestCommentExtractorNoGraphWithoutID(t *testing.T) {
	step := NewCommentExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "// TODO: something",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil {
		t.Error("expected nil graph when ID is empty")
	}
}

func TestCommentExtractorNoGraphWhenNoAnnotations(t *testing.T) {
	step := NewCommentExtractor()
	draft := &storage.KnowledgeObject{
		ID:         "obj-ce2",
		RawContent: "// regular comment\nvar x = 1",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil {
		t.Error("expected nil graph when no annotations found")
	}
}

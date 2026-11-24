package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestVisionAnalyzerAddsSection(t *testing.T) {
	step := NewVisionAnalyzer()
	draft := &storage.KnowledgeObject{
		RawContent:  "fake image",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	found := false
	for _, sec := range got.Sections {
		if sec.Title == "Vision Analysis" {
			found = true
		}
	}
	if !found {
		t.Error("expected Vision Analysis section")
	}
	if got.Metadata["vision_provider"] != "stub" {
		t.Errorf("vision_provider: %v", got.Metadata["vision_provider"])
	}
}

func TestVisionAnalyzerEmitsGraphNodes(t *testing.T) {
	step := NewVisionAnalyzer()
	draft := withOriginal(t, &storage.KnowledgeObject{
		ID:          "obj-vision-001",
		RawContent:  "fake image",
		ContentType: "image/png",
	}, "fake image")
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}
	artifactID := storage.NewNodeID("obj-vision-001", storage.NodeTypeArtifact, 0)
	if got.Graph.FindNode(artifactID) == nil {
		t.Errorf("artifact node %q not found", artifactID)
	}
	capID := storage.NewNodeID("obj-vision-001", storage.NodeTypeSection, 0)
	capNode := got.Graph.FindNode(capID)
	if capNode == nil {
		t.Fatalf("caption node %q not found", capID)
	}
	if capNode.Metadata["representation"] != "caption" || capNode.Metadata["provider"] != "stub" {
		t.Errorf("caption metadata: %v", capNode.Metadata)
	}
	if len(got.Graph.Edges) != 1 || got.Graph.Edges[0].FromID != capID || got.Graph.Edges[0].ToID != artifactID {
		t.Errorf("derives_from must point caption -> artifact: %+v", got.Graph.Edges)
	}
}

func TestVisionAnalyzerNoGraphWithoutID(t *testing.T) {
	step := NewVisionAnalyzer()
	draft := &storage.KnowledgeObject{
		RawContent:  "fake image",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil && len(got.Graph.Nodes) > 0 {
		t.Error("expected no graph nodes when ID is empty")
	}
}

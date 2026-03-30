package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
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
	draft := &storage.KnowledgeObject{
		ID:          "obj-vision-001",
		RawContent:  "fake image",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}
	artifactID := pluginapi.NewNodeID("obj-vision-001", pluginapi.NodeTypeArtifact, 0)
	if got.Graph.FindNode(artifactID) == nil {
		t.Errorf("artifact node %q not found", artifactID)
	}
	summaryID := pluginapi.NewNodeID("obj-vision-001", pluginapi.NodeTypeSummary, 0)
	if got.Graph.FindNode(summaryID) == nil {
		t.Errorf("summary node %q not found", summaryID)
	}
	if len(got.Graph.Edges) == 0 {
		t.Error("expected edge from artifact to summary")
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

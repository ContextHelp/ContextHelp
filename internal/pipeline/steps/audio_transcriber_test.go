package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestAudioTranscriberSetsTranscript(t *testing.T) {
	step := NewAudioTranscriber()
	draft := &storage.KnowledgeObject{
		Source:      "/tmp/test.mp3",
		ContentType: "audio/mpeg",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent == "" {
		t.Error("RawContent is empty after transcription")
	}
	if got.Metadata["transcription_provider"] != "stub" {
		t.Errorf("provider: %v", got.Metadata["transcription_provider"])
	}
}

func TestAudioTranscriberEmitsGraphNodes(t *testing.T) {
	step := NewAudioTranscriber()
	draft := &storage.KnowledgeObject{
		ID:          "obj-audio-001",
		Source:      "/tmp/test.mp3",
		ContentType: "audio/mpeg",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}
	rootID := pluginapi.NewNodeID("obj-audio-001", pluginapi.NodeTypeSummary, 0)
	if got.Graph.FindNode(rootID) == nil {
		t.Errorf("root summary node %q not found", rootID)
	}
	var sections []*pluginapi.GraphNode
	for i := range got.Graph.Nodes {
		if got.Graph.Nodes[i].NodeType == pluginapi.NodeTypeSection {
			sections = append(sections, &got.Graph.Nodes[i])
		}
	}
	if len(sections) == 0 {
		t.Error("expected at least one section node for transcript segments")
	}
	if sections[0].Label == "" {
		t.Error("section node label should not be empty")
	}
}

func TestAudioTranscriberNoGraphWithoutID(t *testing.T) {
	step := NewAudioTranscriber()
	draft := &storage.KnowledgeObject{
		Source:      "/tmp/test.mp3",
		ContentType: "audio/mpeg",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil && len(got.Graph.Nodes) > 0 {
		t.Error("expected no graph nodes when ID is empty")
	}
}

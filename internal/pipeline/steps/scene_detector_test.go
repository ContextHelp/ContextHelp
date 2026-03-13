package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestSceneDetectorSetsScenes(t *testing.T) {
	step := NewSceneDetector()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"frame_count": 10,
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["scene_count"] == nil {
		t.Fatal("expected scene_count to be set")
	}
	scenes, ok := got.Metadata["scenes"].([]map[string]any)
	if !ok {
		t.Fatal("scenes not []map[string]any")
	}
	if len(scenes) == 0 {
		t.Error("expected at least one scene")
	}
}

func TestSceneDetectorMinOneScene(t *testing.T) {
	step := NewSceneDetector()
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["scene_count"] != 1 {
		t.Errorf("scene_count: %v", got.Metadata["scene_count"])
	}
}

func TestSceneDetectorName(t *testing.T) {
	step := NewSceneDetector()
	if step.Name() != "scene_detector" {
		t.Errorf("name: %q", step.Name())
	}
}

package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SceneDetector detects scene boundaries based on frame metadata.
// Uses a simple stub-based approach — no external provider required.
type SceneDetector struct {
	pipeline.BaseContract
}

// NewSceneDetector creates a SceneDetector.
func NewSceneDetector() *SceneDetector {
	return &SceneDetector{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{},
			Produces: []string{},
		}),
	}
}

func (s *SceneDetector) Name() string { return "scene_detector" }

func (s *SceneDetector) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	frameCount := 0
	if v, ok := draft.Metadata["frame_count"]; ok {
		switch n := v.(type) {
		case int:
			frameCount = n
		case float64:
			frameCount = int(n)
		}
	}

	// Simple heuristic: one scene per 5 frames, minimum 1 scene.
	numScenes := frameCount / 5
	if numScenes < 1 {
		numScenes = 1
	}

	scenes := make([]map[string]any, numScenes)
	for i := range scenes {
		scenes[i] = map[string]any{
			"index":       i,
			"start_frame": i * 5,
			"end_frame":   min((i+1)*5, frameCount) - 1,
			"label":       fmt.Sprintf("scene_%d", i+1),
		}
	}

	draft.Metadata["scene_count"] = numScenes
	draft.Metadata["scenes"] = scenes

	return draft, nil
}


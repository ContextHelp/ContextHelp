package steps

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TimelineAssembler merges transcript sections, scene metadata, and frame OCR
// text into a unified, chronologically ordered set of Sections.
type TimelineAssembler struct {
	pipeline.BaseContract
}

// NewTimelineAssembler creates a TimelineAssembler.
func NewTimelineAssembler() *TimelineAssembler {
	return &TimelineAssembler{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{},
			Produces: []string{"Sections"},
		}),
	}
}

func (s *TimelineAssembler) Name() string { return "timeline_assembler" }

func (s *TimelineAssembler) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	// Collect existing transcript sections.
	assembled := make([]storage.Section, 0, len(draft.Sections))
	assembled = append(assembled, draft.Sections...)

	// Append scene sections if present.
	if scenes, ok := draft.Metadata["scenes"].([]map[string]any); ok {
		for _, scene := range scenes {
			label, _ := scene["label"].(string)
			if label == "" {
				label = "scene"
			}
			assembled = append(assembled, storage.Section{
				Title:   fmt.Sprintf("Scene: %s", label),
				Content: fmt.Sprintf("Scene detected: %v", scene),
				Order:   len(assembled),
				Metadata: map[string]any{
					"type":  "scene",
					"scene": scene,
				},
			})
		}
	}

	// Append frame OCR text as a section if present.
	if ocrText, ok := draft.Metadata["frame_ocr_text"].(string); ok && ocrText != "" {
		assembled = append(assembled, storage.Section{
			Title:   "Frame OCR",
			Content: ocrText,
			Order:   len(assembled),
			Metadata: map[string]any{
				"type": "frame_ocr",
			},
		})
	}

	// Re-number sections in order.
	for i := range assembled {
		assembled[i].Order = i
	}

	draft.Sections = assembled
	draft.Metadata["timeline_assembled"] = true

	return draft, nil
}

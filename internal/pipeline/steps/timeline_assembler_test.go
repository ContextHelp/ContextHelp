package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestTimelineAssemblerEmpty(t *testing.T) {
	step := NewTimelineAssembler()
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["timeline_assembled"] != true {
		t.Error("expected timeline_assembled = true")
	}
}

func TestTimelineAssemblerMergesAll(t *testing.T) {
	step := NewTimelineAssembler()
	draft := &storage.KnowledgeObject{
		Sections: []storage.Section{
			{Title: "Transcript", Content: "Hello world", Order: 0},
		},
		Metadata: map[string]any{
			"scenes": []map[string]any{
				{"label": "scene_1", "index": 0},
			},
			"frame_ocr_text": "OCR text from frame",
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Should have: 1 transcript + 1 scene + 1 frame_ocr = 3 sections
	if len(got.Sections) != 3 {
		t.Errorf("expected 3 sections, got %d", len(got.Sections))
	}
}

func TestTimelineAssemblerName(t *testing.T) {
	step := NewTimelineAssembler()
	if step.Name() != "timeline_assembler" {
		t.Errorf("name: %q", step.Name())
	}
}

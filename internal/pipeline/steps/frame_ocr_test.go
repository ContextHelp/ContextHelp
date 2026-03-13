package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFrameOCRNoFrames(t *testing.T) {
	step := NewFrameOCR()
	draft := &storage.KnowledgeObject{}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// No frames — frame_ocr_text should not exist or be empty.
	if v, ok := got.Metadata["frame_ocr_text"]; ok && v != "" {
		t.Errorf("unexpected frame_ocr_text: %v", v)
	}
}

func TestFrameOCRWithFrames(t *testing.T) {
	step := NewFrameOCR()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"frame_paths": []string{"/tmp/frame0.png", "/tmp/frame1.png"},
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["frame_ocr_text"] == nil {
		t.Error("expected frame_ocr_text to be set")
	}
}

func TestFrameOCRName(t *testing.T) {
	step := NewFrameOCR()
	if step.Name() != "frame_ocr" {
		t.Errorf("name: %q", step.Name())
	}
}

package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestAudioExtractorSetsPath(t *testing.T) {
	step := NewAudioExtractor()
	draft := &storage.KnowledgeObject{
		Source: "/tmp/test.mp4",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["audio_path"] == "" {
		t.Error("expected audio_path to be set")
	}
	if got.Metadata["audio_format"] == "" {
		t.Error("expected audio_format to be set")
	}
}

func TestAudioExtractorName(t *testing.T) {
	step := NewAudioExtractor()
	if step.Name() != "audio_extractor" {
		t.Errorf("name: %q", step.Name())
	}
}

package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
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

package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestSpeakerDiarizerDisabled(t *testing.T) {
	step := NewSpeakerDiarizer(false)
	draft := &storage.KnowledgeObject{RawContent: "test"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.RawContent != "test" {
		t.Error("draft should be unchanged when disabled")
	}
}

func TestSpeakerDiarizerEnabled(t *testing.T) {
	step := NewSpeakerDiarizer(true)
	draft := &storage.KnowledgeObject{
		Source: "/tmp/test.mp3",
		Metadata: map[string]any{
			"_raw_segments": []providers.TranscriptSegment{
				{Text: "hello"},
			},
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["speaker_count"] != 1 {
		t.Errorf("speaker_count: %v", got.Metadata["speaker_count"])
	}
}

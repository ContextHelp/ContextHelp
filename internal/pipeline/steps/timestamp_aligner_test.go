package steps

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestTimestampAlignerCreatesSections(t *testing.T) {
	step := NewTimestampAligner()
	draft := &storage.KnowledgeObject{
		Metadata: map[string]any{
			"_raw_segments": []providers.TranscriptSegment{
				{StartTime: 0, EndTime: 5 * time.Second, Text: "hello", Speaker: "S1"},
				{StartTime: 5 * time.Second, EndTime: 10 * time.Second, Text: "world"},
			},
		},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("sections: got %d, want 2", len(got.Sections))
	}
	if got.Sections[0].Metadata["speaker"] != "S1" {
		t.Errorf("speaker: %v", got.Sections[0].Metadata["speaker"])
	}
	if _, ok := got.Metadata["_raw_segments"]; ok {
		t.Error("_raw_segments should be cleaned up")
	}
}

func TestTimestampAlignerNoSegments(t *testing.T) {
	step := NewTimestampAligner()
	draft := &storage.KnowledgeObject{Metadata: map[string]any{}}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 0 {
		t.Errorf("sections: got %d, want 0", len(got.Sections))
	}
}

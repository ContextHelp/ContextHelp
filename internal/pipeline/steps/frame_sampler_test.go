package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestFrameSamplerSetsCount(t *testing.T) {
	step := NewFrameSampler(WithFrameCount(5))
	draft := &storage.KnowledgeObject{
		Source: "/tmp/test.mp4",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["frame_count"] != 5 {
		t.Errorf("frame_count: %v", got.Metadata["frame_count"])
	}
	paths, ok := got.Metadata["frame_paths"].([]string)
	if !ok {
		t.Fatal("frame_paths not []string")
	}
	if len(paths) != 5 {
		t.Errorf("frame_paths len: %d", len(paths))
	}
}

func TestFrameSamplerName(t *testing.T) {
	step := NewFrameSampler()
	if step.Name() != "frame_sampler" {
		t.Errorf("name: %q", step.Name())
	}
}

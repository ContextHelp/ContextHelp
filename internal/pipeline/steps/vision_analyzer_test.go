package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestVisionAnalyzerAddsSection(t *testing.T) {
	step := NewVisionAnalyzer()
	draft := &storage.KnowledgeObject{
		RawContent:  "fake image",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	found := false
	for _, sec := range got.Sections {
		if sec.Title == "Vision Analysis" {
			found = true
		}
	}
	if !found {
		t.Error("expected Vision Analysis section")
	}
	if got.Metadata["vision_provider"] != "stub" {
		t.Errorf("vision_provider: %v", got.Metadata["vision_provider"])
	}
}

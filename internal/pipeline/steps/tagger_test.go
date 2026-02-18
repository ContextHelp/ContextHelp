package steps

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestExtractTags(t *testing.T) {
	step := NewTagger()
	// Repeat "design" enough times to ensure it appears as a tag.
	draft := &storage.KnowledgeObject{
		RawContent: strings.Repeat("design pattern layout ", 5),
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	found := false
	for _, tag := range got.Tags {
		if tag.Label == "design" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected tag 'design' in %v", got.Tags)
	}
}

func TestNoTags(t *testing.T) {
	step := NewTagger()
	draft := &storage.KnowledgeObject{RawContent: "hi"}

	got, _ := step.Run(context.Background(), draft)
	if len(got.Tags) != 0 {
		t.Errorf("expected 0 tags, got %d: %v", len(got.Tags), got.Tags)
	}
}

func TestTagWeights(t *testing.T) {
	step := NewTagger()
	// "design" appears more than "pattern".
	draft := &storage.KnowledgeObject{
		RawContent: "design design design design pattern pattern layout",
	}

	got, _ := step.Run(context.Background(), draft)
	var designWeight, patternWeight float64
	for _, tag := range got.Tags {
		if tag.Label == "design" {
			designWeight = tag.Weight
		}
		if tag.Label == "pattern" {
			patternWeight = tag.Weight
		}
	}

	if designWeight <= patternWeight {
		t.Errorf("design weight (%v) should be > pattern weight (%v)", designWeight, patternWeight)
	}
}

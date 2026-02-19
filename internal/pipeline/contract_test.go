package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestCanonicalizeAlias(t *testing.T) {
	tests := []struct{ input, want string }{
		{"text", "RawContent"},
		{"content_type", "ContentType"},
		{"tags", "Tags"},
		{"metadata", "Metadata"},
		{"vector_indexed", "VectorIndexed"},
	}
	for _, tt := range tests {
		if got := Canonicalize(tt.input); got != tt.want {
			t.Errorf("Canonicalize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCanonicalizePassthrough(t *testing.T) {
	// Non-alias keys pass through unchanged.
	tests := []string{"RawContent", "Type", "Sections", "UnknownField"}
	for _, key := range tests {
		if got := Canonicalize(key); got != key {
			t.Errorf("Canonicalize(%q) = %q, want passthrough", key, got)
		}
	}
}

func TestCanonicalizeDotNotation(t *testing.T) {
	// Metadata sub-keys pass through (not aliased individually).
	key := "Metadata.ocr_confidence"
	if got := Canonicalize(key); got != key {
		t.Errorf("Canonicalize(%q) = %q, want passthrough", key, got)
	}
}

func TestBaseContract(t *testing.T) {
	c := NewBaseContract(StepContract{
		Requires:     []string{"RawContent"},
		Produces:     []string{"Tags"},
		Capabilities: []string{"llm"},
	})
	got := c.Contract()
	if len(got.Requires) != 1 || got.Requires[0] != "RawContent" {
		t.Errorf("Requires: %v", got.Requires)
	}
	if len(got.Produces) != 1 || got.Produces[0] != "Tags" {
		t.Errorf("Produces: %v", got.Produces)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0] != "llm" {
		t.Errorf("Capabilities: %v", got.Capabilities)
	}
}

func TestEmptyBaseContract(t *testing.T) {
	c := NewBaseContract(StepContract{})
	got := c.Contract()
	if got.Requires != nil || got.Produces != nil || got.Capabilities != nil {
		t.Errorf("empty contract should have nil slices: %+v", got)
	}
}

// --- composability tests ---

func TestValidateComposability_Valid(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"RawContent"}, Produces: []string{"Type"}}},
		&stubStep{name: "b", contract: StepContract{Requires: []string{"Type"}, Produces: []string{"Tags"}}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposability_SeedState(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"RawContent", "Source", "Pipeline"}, Produces: []string{"Type"}}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposability_MissingRequires(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"RawContent"}, Produces: []string{"Type"}}},
		&stubStep{name: "b", contract: StepContract{Requires: []string{"Sections"}, Produces: []string{"Tags"}}},
	}
	err := ValidateComposability(steps)
	if err == nil {
		t.Fatal("expected error for missing Sections requirement")
	}
	if !strings.Contains(err.Error(), "Sections") {
		t.Errorf("error should mention missing key: %v", err)
	}
	if !strings.Contains(err.Error(), "step 1") {
		t.Errorf("error should mention step index: %v", err)
	}
}

func TestValidateComposability_MetadataPrefix(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"RawContent"}, Produces: []string{"Metadata"}}},
		&stubStep{name: "b", contract: StepContract{Requires: []string{"Metadata.ocr_confidence"}, Produces: []string{"Tags"}}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposability_EmptyContracts(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{}},
		&stubStep{name: "b", contract: StepContract{}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateComposability_AliasResolution(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Requires: []string{"text"}, Produces: []string{"tags"}}},
	}
	if err := ValidateComposability(steps); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- capability validation tests ---

func TestValidateCapabilities_AllPresent(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Capabilities: []string{"ocr"}}},
		&stubStep{name: "b", contract: StepContract{Capabilities: []string{"vision"}}},
	}
	caps := CapabilitySet{"ocr": true, "vision": true}
	unsatisfied := ValidateCapabilities(steps, caps)
	if len(unsatisfied) != 0 {
		t.Errorf("expected no unsatisfied, got %v", unsatisfied)
	}
}

func TestValidateCapabilities_MissingCap(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{Capabilities: []string{"ocr"}}},
		&stubStep{name: "b", contract: StepContract{}},
		&stubStep{name: "c", contract: StepContract{Capabilities: []string{"vision"}}},
	}
	caps := CapabilitySet{"ocr": true}
	unsatisfied := ValidateCapabilities(steps, caps)
	if len(unsatisfied) != 1 || unsatisfied[0] != 2 {
		t.Errorf("expected [2], got %v", unsatisfied)
	}
}

func TestValidateCapabilities_NoCaps(t *testing.T) {
	steps := []PipelineStep{
		&stubStep{name: "a", contract: StepContract{}},
	}
	unsatisfied := ValidateCapabilities(steps, CapabilitySet{})
	if len(unsatisfied) != 0 {
		t.Errorf("expected no unsatisfied, got %v", unsatisfied)
	}
}

func TestRemoveIndices(t *testing.T) {
	a := &stubStep{name: "a"}
	b := &stubStep{name: "b"}
	c := &stubStep{name: "c"}
	steps := []PipelineStep{a, b, c}
	result := RemoveIndices(steps, []int{1})
	if len(result) != 2 || result[0].Name() != "a" || result[1].Name() != "c" {
		t.Errorf("unexpected result: %v", result)
	}
}

// --- test helper ---

type stubStep struct {
	BaseContract
	name     string
	contract StepContract
}

func (s *stubStep) Name() string          { return s.name }
func (s *stubStep) Contract() StepContract { return s.contract }
func (s *stubStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return d, nil
}

package pipeline

import "testing"

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

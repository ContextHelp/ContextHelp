package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stubLLM returns a canned response for testing.
type stubLLM struct {
	resp string
	err  error
}

func (s *stubLLM) Generate(_ context.Context, _ string) (string, error) {
	return s.resp, s.err
}
func (s *stubLLM) Name() string { return "stub" }

func TestStructuredMetadataNoLLM(t *testing.T) {
	step := NewStructuredMetadataExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "Some test content",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, ok := got.Metadata["metadata_status"]
	if !ok || status != "pending" {
		t.Errorf("expected metadata_status=pending, got %v", status)
	}
}

func TestStructuredMetadataEmptyContent(t *testing.T) {
	llm := &stubLLM{resp: `{}`}
	step := NewStructuredMetadataExtractorWithLLM(llm)
	draft := &storage.KnowledgeObject{RawContent: "   "}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Metadata["metadata_status"] != "pending" {
		t.Error("expected pending for whitespace-only content")
	}
}

func TestStructuredMetadataLLMFailure(t *testing.T) {
	llm := &stubLLM{err: errors.New("provider down")}
	step := NewStructuredMetadataExtractorWithLLM(llm)
	draft := &storage.KnowledgeObject{RawContent: "real content"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Metadata["metadata_status"] != "pending" {
		t.Error("expected pending on LLM failure")
	}
}

func TestStructuredMetadataInvalidJSON(t *testing.T) {
	llm := &stubLLM{resp: "not json at all"}
	step := NewStructuredMetadataExtractorWithLLM(llm)
	draft := &storage.KnowledgeObject{RawContent: "test content"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Metadata["metadata_status"] != "pending" {
		t.Error("expected pending on invalid JSON")
	}
}

func TestStructuredMetadataSuccess(t *testing.T) {
	resp := `{"type":"task","topics":["auth","security"],"people":["@person.alice"],"action_items":["review PR #42"],"dates_mentioned":["2026-05-01"],"source_type":"text","confidence":0.85}`
	llm := &stubLLM{resp: resp}
	step := NewStructuredMetadataExtractorWithLLM(llm)
	draft := &storage.KnowledgeObject{RawContent: "Alice needs to review PR #42 by May 1st"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Metadata["metadata_status"] != "complete" {
		t.Errorf("expected metadata_status=complete, got %v", got.Metadata["metadata_status"])
	}
	meta, ok := got.Metadata["enrichment.structured_metadata"].(*StructuredMetadata)
	if !ok {
		t.Fatalf("expected *StructuredMetadata, got %T", got.Metadata["enrichment.structured_metadata"])
	}
	if meta.Type != "task" {
		t.Errorf("type: got %q, want %q", meta.Type, "task")
	}
	if len(meta.Topics) != 2 {
		t.Errorf("topics: got %d, want 2", len(meta.Topics))
	}
	if len(meta.People) != 1 || meta.People[0] != "@person.alice" {
		t.Errorf("people: got %v", meta.People)
	}
	if len(meta.ActionItems) != 1 {
		t.Errorf("action_items: got %d, want 1", len(meta.ActionItems))
	}
	if len(meta.DatesMentioned) != 1 || meta.DatesMentioned[0] != "2026-05-01" {
		t.Errorf("dates_mentioned: got %v", meta.DatesMentioned)
	}
	if meta.Confidence != 0.85 {
		t.Errorf("confidence: got %f, want 0.85", meta.Confidence)
	}
}

func TestStructuredMetadataTypeConstraint(t *testing.T) {
	resp := `{"type":"invalid_type","topics":[],"confidence":0.5}`
	llm := &stubLLM{resp: resp}
	step := NewStructuredMetadataExtractorWithLLM(llm)
	draft := &storage.KnowledgeObject{RawContent: "test content"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	meta := got.Metadata["enrichment.structured_metadata"].(*StructuredMetadata)
	if meta.Type != "observation" {
		t.Errorf("invalid type should default to observation, got %q", meta.Type)
	}
}

func TestStructuredMetadataConfidenceClamp(t *testing.T) {
	tests := []struct {
		name string
		resp string
		want float64
	}{
		{"negative", `{"type":"task","confidence":-0.5}`, 0},
		{"over_one", `{"type":"task","confidence":1.5}`, 1},
		{"normal", `{"type":"task","confidence":0.7}`, 0.7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			llm := &stubLLM{resp: tc.resp}
			step := NewStructuredMetadataExtractorWithLLM(llm)
			draft := &storage.KnowledgeObject{RawContent: "test"}
			got, _ := step.Run(context.Background(), draft)
			meta := got.Metadata["enrichment.structured_metadata"].(*StructuredMetadata)
			if meta.Confidence != tc.want {
				t.Errorf("confidence: got %f, want %f", meta.Confidence, tc.want)
			}
		})
	}
}

func TestStructuredMetadataSourceTypeInference(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"url", "https://example.com/article", "url"},
		{"file_md", "notes.md", "file"},
		{"file_txt", "data.txt", "file"},
		{"file_pdf", "report.pdf", "file"},
		{"file_uri", "file:///tmp/foo", "file"},
		{"webhook", "webhook-endpoint", "webhook"},
		{"api", "api-service", "api"},
		{"text_default", "some random source", "text"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// LLM returns empty source_type so inferSourceType kicks in.
			llm := &stubLLM{resp: `{"type":"observation","source_type":"","confidence":0.5}`}
			step := NewStructuredMetadataExtractorWithLLM(llm)
			draft := &storage.KnowledgeObject{
				RawContent: "test content",
				Source:     tc.source,
			}
			got, _ := step.Run(context.Background(), draft)
			meta := got.Metadata["enrichment.structured_metadata"].(*StructuredMetadata)
			if meta.SourceType != tc.want {
				t.Errorf("source_type: got %q, want %q", meta.SourceType, tc.want)
			}
		})
	}
}

func TestStructuredMetadataNilSlicesNormalized(t *testing.T) {
	llm := &stubLLM{resp: `{"type":"idea","confidence":0.8}`}
	step := NewStructuredMetadataExtractorWithLLM(llm)
	draft := &storage.KnowledgeObject{RawContent: "a cool idea"}
	got, _ := step.Run(context.Background(), draft)
	meta := got.Metadata["enrichment.structured_metadata"].(*StructuredMetadata)
	if meta.Topics == nil {
		t.Error("topics should be empty slice, not nil")
	}
	if meta.People == nil {
		t.Error("people should be empty slice, not nil")
	}
	if meta.ActionItems == nil {
		t.Error("action_items should be empty slice, not nil")
	}
	if meta.DatesMentioned == nil {
		t.Error("dates_mentioned should be empty slice, not nil")
	}
}

func TestStructuredMetadataJSONWithPreamble(t *testing.T) {
	// LLM sometimes wraps JSON in markdown or preamble text.
	resp := "Here is the metadata:\n```json\n" +
		`{"type":"decision","topics":["auth"],"confidence":0.9}` +
		"\n```"
	llm := &stubLLM{resp: resp}
	step := NewStructuredMetadataExtractorWithLLM(llm)
	draft := &storage.KnowledgeObject{RawContent: "We decided to use OAuth2"}
	got, _ := step.Run(context.Background(), draft)
	meta, ok := got.Metadata["enrichment.structured_metadata"].(*StructuredMetadata)
	if !ok {
		t.Fatal("expected successful extraction despite preamble")
	}
	if meta.Type != "decision" {
		t.Errorf("type: got %q, want %q", meta.Type, "decision")
	}
}

func TestStructuredMetadataName(t *testing.T) {
	step := NewStructuredMetadataExtractor()
	if step.Name() != "structured_metadata" {
		t.Errorf("name: got %q, want %q", step.Name(), "structured_metadata")
	}
}

func TestStructuredMetadataContract(t *testing.T) {
	step := NewStructuredMetadataExtractor()
	c := step.Contract()
	if len(c.Requires) == 0 || c.Requires[0] != "RawContent" {
		t.Errorf("requires: got %v, want [RawContent]", c.Requires)
	}
	if len(c.Produces) == 0 || c.Produces[0] != "Metadata" {
		t.Errorf("produces: got %v, want [Metadata]", c.Produces)
	}
}

func TestStructuredMetadataIdempotent(t *testing.T) {
	resp := `{"type":"task","topics":["deploy"],"people":["@person.bob"],"action_items":["deploy v2"],"dates_mentioned":["2026-06-01"],"source_type":"text","confidence":0.9}`
	llm := &stubLLM{resp: resp}
	step := NewStructuredMetadataExtractorWithLLM(llm)
	draft := &storage.KnowledgeObject{RawContent: "Bob needs to deploy v2 by June 1st"}

	got1, _ := step.Run(context.Background(), draft)
	meta1 := got1.Metadata["enrichment.structured_metadata"].(*StructuredMetadata)

	// Reset metadata to simulate re-run.
	draft.Metadata = nil
	got2, _ := step.Run(context.Background(), draft)
	meta2 := got2.Metadata["enrichment.structured_metadata"].(*StructuredMetadata)

	if meta1.Type != meta2.Type {
		t.Errorf("idempotency: type mismatch %q vs %q", meta1.Type, meta2.Type)
	}
	if meta1.Confidence != meta2.Confidence {
		t.Errorf("idempotency: confidence mismatch %f vs %f", meta1.Confidence, meta2.Confidence)
	}
}

func TestStructuredMetadataContentTruncation(t *testing.T) {
	// Content longer than 4000 chars should be truncated.
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'a'
	}
	llm := &stubLLM{resp: `{"type":"reference","confidence":0.5}`}
	step := NewStructuredMetadataExtractorWithLLM(llm)
	draft := &storage.KnowledgeObject{RawContent: string(long)}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Metadata["metadata_status"] != "complete" {
		t.Error("long content should still be processed (truncated)")
	}
}

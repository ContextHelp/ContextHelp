package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestApplySchemaConstraintsEntityTypes(t *testing.T) {
	schema := &config.ProfileSchema{
		EntityTypes: []string{"task", "decision"},
	}

	meta := &StructuredMetadata{Type: "observation", Topics: []string{}}
	applySchemaConstraints(meta, schema, "some content")

	// "observation" not in schema → falls back to first type.
	if meta.Type != "task" {
		t.Errorf("type: got %q, want %q", meta.Type, "task")
	}
}

func TestApplySchemaConstraintsEntityTypesAllowed(t *testing.T) {
	schema := &config.ProfileSchema{
		EntityTypes: []string{"task", "decision"},
	}

	meta := &StructuredMetadata{Type: "decision", Topics: []string{}}
	applySchemaConstraints(meta, schema, "content")

	if meta.Type != "decision" {
		t.Errorf("type: got %q, want %q", meta.Type, "decision")
	}
}

func TestApplySchemaConstraintsTopicVocabulary(t *testing.T) {
	schema := &config.ProfileSchema{
		TopicVocabulary: []string{"auth", "deploy"},
	}

	meta := &StructuredMetadata{
		Type:   "task",
		Topics: []string{"auth", "unknown", "deploy", "random"},
	}
	applySchemaConstraints(meta, schema, "content")

	if len(meta.Topics) != 2 {
		t.Fatalf("topics: got %d, want 2", len(meta.Topics))
	}
	if meta.Topics[0] != "auth" || meta.Topics[1] != "deploy" {
		t.Errorf("topics: got %v", meta.Topics)
	}
}

func TestApplySchemaConstraintsClassificationRule(t *testing.T) {
	schema := &config.ProfileSchema{
		EntityTypes: []string{"task", "incident"},
		ClassificationRules: []config.ClassificationRule{
			{Pattern: "(?i)deploy", Type: "task"},
			{Pattern: "(?i)outage", Type: "incident"},
		},
	}

	meta := &StructuredMetadata{Type: "observation", Topics: []string{}}
	applySchemaConstraints(meta, schema, "There was a production outage")

	if meta.Type != "incident" {
		t.Errorf("type: got %q, want %q", meta.Type, "incident")
	}
}

func TestApplySchemaConstraintsRuleFirstMatchWins(t *testing.T) {
	schema := &config.ProfileSchema{
		ClassificationRules: []config.ClassificationRule{
			{Pattern: "deploy", Type: "task"},
			{Pattern: "deploy", Type: "decision"},
		},
	}

	meta := &StructuredMetadata{Type: "observation", Topics: []string{}}
	applySchemaConstraints(meta, schema, "we need to deploy")

	if meta.Type != "task" {
		t.Errorf("first match should win: got %q, want %q", meta.Type, "task")
	}
}

func TestApplySchemaConstraintsInvalidRegexSkipped(t *testing.T) {
	schema := &config.ProfileSchema{
		ClassificationRules: []config.ClassificationRule{
			{Pattern: "[invalid", Type: "task"},
		},
	}

	meta := &StructuredMetadata{Type: "observation", Topics: []string{}}
	applySchemaConstraints(meta, schema, "content")

	// Invalid regex skipped, type unchanged.
	if meta.Type != "observation" {
		t.Errorf("type: got %q, want %q", meta.Type, "observation")
	}
}

func TestApplySchemaConstraintsEmptySchemaNoChange(t *testing.T) {
	schema := &config.ProfileSchema{}

	meta := &StructuredMetadata{
		Type:   "observation",
		Topics: []string{"auth", "security"},
	}
	applySchemaConstraints(meta, schema, "content")

	if meta.Type != "observation" {
		t.Errorf("type: got %q, want %q", meta.Type, "observation")
	}
	if len(meta.Topics) != 2 {
		t.Errorf("topics: got %d, want 2", len(meta.Topics))
	}
}

func TestStructuredMetadataWithSchema(t *testing.T) {
	resp := `{"type":"idea","topics":["auth","random","security"],"confidence":0.8}`
	llm := &stubLLM{resp: resp}
	schema := &config.ProfileSchema{
		EntityTypes:     []string{"task", "decision"},
		TopicVocabulary: []string{"auth", "security"},
	}

	step := NewStructuredMetadataExtractorWithSchema(llm, schema)
	draft := &storage.KnowledgeObject{RawContent: "test content"}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := got.Metadata["enrichment.structured_metadata"].(*StructuredMetadata)

	// "idea" not in entity types → fallback to "task" (first).
	if meta.Type != "task" {
		t.Errorf("type: got %q, want %q", meta.Type, "task")
	}

	// "random" not in vocabulary → filtered out.
	if len(meta.Topics) != 2 {
		t.Errorf("topics: got %v, want [auth security]", meta.Topics)
	}
}

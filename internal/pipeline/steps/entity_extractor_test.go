package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestEntityExtractorLiteralMentions(t *testing.T) {
	step := NewEntityExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "Working on @project.signup-redesign with @person.alice and @org.acme today.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := map[string]bool{
		"ctxt://entity/project/signup-redesign": true,
		"ctxt://entity/person/alice":            true,
		"ctxt://entity/org/acme":                true,
	}
	if len(got.MentionURIs) != len(want) {
		t.Fatalf("expected %d mentions, got %d: %v", len(want), len(got.MentionURIs), got.MentionURIs)
	}
	for _, u := range got.MentionURIs {
		if !want[u.String()] {
			t.Errorf("unexpected mention %q", u.String())
		}
	}
}

func TestEntityExtractorDeduplicates(t *testing.T) {
	step := NewEntityExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "@project.alpha and @project.alpha again and @project.alpha once more",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.MentionURIs) != 1 {
		t.Errorf("expected 1 deduplicated mention, got %d: %v", len(got.MentionURIs), got.MentionURIs)
	}
}

func TestEntityExtractorNoMentions(t *testing.T) {
	step := NewEntityExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "This content has no entity mentions at all.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.MentionURIs) != 0 {
		t.Errorf("expected 0 mentions, got %d: %v", len(got.MentionURIs), got.MentionURIs)
	}
}

func TestEntityExtractorInvalidFormatIgnored(t *testing.T) {
	step := NewEntityExtractor()
	// @foo alone (no dot) and @123.bar (starts with digit) are not valid
	draft := &storage.KnowledgeObject{
		RawContent: "@foo plain email@example.com valid @project.real-thing",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.MentionURIs) != 1 || got.MentionURIs[0].String() != "ctxt://entity/project/real-thing" {
		t.Errorf("expected only 'ctxt://entity/project/real-thing', got %v", got.MentionURIs)
	}
}

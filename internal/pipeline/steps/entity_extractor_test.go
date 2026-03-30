package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
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
	if len(got.Mentions) != len(want) {
		t.Fatalf("expected %d mentions, got %d: %v", len(want), len(got.Mentions), got.Mentions)
	}
	for _, u := range got.Mentions {
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
	if len(got.Mentions) != 1 {
		t.Errorf("expected 1 deduplicated mention, got %d: %v", len(got.Mentions), got.Mentions)
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
	if len(got.Mentions) != 0 {
		t.Errorf("expected 0 mentions, got %d: %v", len(got.Mentions), got.Mentions)
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
	if len(got.Mentions) != 1 || got.Mentions[0].String() != "ctxt://entity/project/real-thing" {
		t.Errorf("expected only 'ctxt://entity/project/real-thing', got %v", got.Mentions)
	}
}

func TestEntityExtractorMultiDotSlug(t *testing.T) {
	step := NewEntityExtractor()
	// @stripe.api.checkout should become ctxt://entity/stripe/api/checkout (all dots → slashes)
	draft := &storage.KnowledgeObject{
		RawContent: "Integrate with @stripe.api.checkout for payments.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Mentions) != 1 || got.Mentions[0].String() != "ctxt://entity/stripe/api/checkout" {
		t.Errorf("expected 'ctxt://entity/stripe/api/checkout', got %v", got.Mentions)
	}
}

func TestEntityExtractorEmitsIntraObjectGraphNodes(t *testing.T) {
	step := NewEntityExtractor()
	draft := &storage.KnowledgeObject{
		ID:         "obj-ee1",
		RawContent: "Working on @project.alpha and @org.acme today.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected intra-object nodes")
	}
	// Two mentions → two NodeTypeEntityMention nodes + two EdgeTypeReferences edges.
	if len(got.Graph.Nodes) != 2 {
		t.Errorf("nodes: got %d, want 2", len(got.Graph.Nodes))
	}
	for i, n := range got.Graph.Nodes {
		if n.NodeType != pluginapi.NodeTypeEntityMention {
			t.Errorf("node[%d] type: got %q, want %q", i, n.NodeType, pluginapi.NodeTypeEntityMention)
		}
	}
	if len(got.Graph.Edges) != 2 {
		t.Errorf("edges: got %d, want 2", len(got.Graph.Edges))
	}
	for i, e := range got.Graph.Edges {
		if e.EdgeType != pluginapi.EdgeTypeReferences {
			t.Errorf("edge[%d] type: got %q, want %q", i, e.EdgeType, pluginapi.EdgeTypeReferences)
		}
	}
}

func TestEntityExtractorNoGraphWithoutID(t *testing.T) {
	step := NewEntityExtractor()
	draft := &storage.KnowledgeObject{
		RawContent: "Mentioning @project.foo here.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil {
		t.Error("expected nil graph when ID is empty")
	}
}

func TestEntityExtractorNoGraphWhenNoMentions(t *testing.T) {
	step := NewEntityExtractor()
	draft := &storage.KnowledgeObject{
		ID:         "obj-ee2",
		RawContent: "No entity mentions here.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil {
		t.Error("expected nil graph when no mentions found")
	}
}


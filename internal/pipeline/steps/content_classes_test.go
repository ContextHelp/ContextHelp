package steps

import (
	"context"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestContentClasses_Run(t *testing.T) {
	rules := append(sharing.DefaultClassRules(), sharing.ClassRule{Class: "financial", Tag: "invoices"})
	step := NewContentClasses(rules)
	if step.Name() != "content_classes" || !slices.Equal(step.Contract().Produces, []string{"Classes"}) {
		t.Fatalf("step = %s %+v", step.Name(), step.Contract())
	}
	for _, tc := range []struct {
		name  string
		draft *storage.KnowledgeObject
		want  []string
	}{
		{"person note", &storage.KnowledgeObject{Type: "note", Metadata: map[string]any{"enrichment.structured_metadata": StructuredMetadata{Type: "person_note"}}}, []string{"personal"}},
		{"personal profile with an invoice tag", &storage.KnowledgeObject{Type: "note", ProfileID: "personal", Tags: []storage.Tag{{Label: "invoices"}}}, []string{"financial", "personal"}},
		{"keeps earlier classes", &storage.KnowledgeObject{Type: "note", Classes: []string{"derived"}}, []string{"derived"}},
		{"nothing matches", &storage.KnowledgeObject{Type: "note", ProfileID: "work"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := step.Run(context.Background(), tc.draft)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.Classes, tc.want) {
				t.Fatalf("classes = %v, want %v", got.Classes, tc.want)
			}
		})
	}
	// Without rules the draft is left alone.
	got, _ := NewContentClasses(nil).Run(context.Background(), &storage.KnowledgeObject{ProfileID: "personal", Classes: []string{"x"}})
	if !slices.Equal(got.Classes, []string{"x"}) {
		t.Errorf("no rules: classes = %v", got.Classes)
	}
}

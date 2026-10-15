package sharing_test

import (
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// structured is the structured_metadata step's value as the draft holds
// it: a struct, not a map.
type structured struct {
	Type   string   `json:"type"`
	Topics []string `json:"topics"`
}

func TestDefaultClassRules(t *testing.T) {
	rules := sharing.DefaultClassRules()
	if err := sharing.ValidateClassRules(rules); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		obj  *storage.KnowledgeObject
		want []string
	}{
		{"person note from the draft", &storage.KnowledgeObject{Type: "note", Metadata: map[string]any{"enrichment.structured_metadata": structured{Type: "person_note"}}}, []string{"personal"}},
		{"person note from storage", &storage.KnowledgeObject{Type: "note", Metadata: map[string]any{"enrichment.structured_metadata": map[string]any{"type": "person_note"}}}, []string{"personal"}},
		{"idea", &storage.KnowledgeObject{Type: "note", Metadata: map[string]any{"enrichment.structured_metadata": structured{Type: "idea"}}}, nil},
		{"personal profile", &storage.KnowledgeObject{Type: "contact", ProfileID: "personal"}, []string{"personal"}},
		{"work profile", &storage.KnowledgeObject{Type: "contact", ProfileID: "work"}, nil},
		{"both rules, one class", &storage.KnowledgeObject{ProfileID: "personal", Metadata: map[string]any{"enrichment.structured_metadata": map[string]any{"type": "person_note"}}}, []string{"personal"}},
		{"keeps existing classes", &storage.KnowledgeObject{ProfileID: "personal", Classes: []string{"financial"}}, []string{"financial", "personal"}},
		{"no metadata", &storage.KnowledgeObject{}, nil},
		{"nil object", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sharing.ApplyClassRules(rules, tc.obj); !slices.Equal(got, tc.want) {
				t.Fatalf("ApplyClassRules = %v, want %v", got, tc.want)
			}
		})
	}
}

// Every matcher ANDs with the others; an unset one matches anything.
func TestClassRule_Matches(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Type: "note", Subtype: "md", Source: "watch:/notes", Pipeline: "text.long@v2", ProfileID: "work",
		Tags: []storage.Tag{{Label: "invoices"}, {Label: "q3"}},
	}
	for _, tc := range []struct {
		name string
		rule sharing.ClassRule
		want bool
	}{
		{"type", sharing.ClassRule{Class: "x", Type: "note"}, true},
		{"other type", sharing.ClassRule{Class: "x", Type: "contact"}, false},
		{"subtype", sharing.ClassRule{Class: "x", Subtype: "md"}, true},
		{"source", sharing.ClassRule{Class: "x", Source: "watch:/notes"}, true},
		{"pipeline bare", sharing.ClassRule{Class: "x", Pipeline: "text.long"}, true},
		{"pipeline versioned", sharing.ClassRule{Class: "x", Pipeline: "text.long@v2"}, true},
		{"pipeline other version", sharing.ClassRule{Class: "x", Pipeline: "text.long@v1"}, true},
		{"other pipeline", sharing.ClassRule{Class: "x", Pipeline: "text.short"}, false},
		{"tag", sharing.ClassRule{Class: "x", Tag: "q3"}, true},
		{"other tag", sharing.ClassRule{Class: "x", Tag: "q4"}, false},
		{"profile", sharing.ClassRule{Class: "x", Profile: "work"}, true},
		{"all together", sharing.ClassRule{Class: "x", Type: "note", Tag: "invoices", Profile: "work", Pipeline: "text.long"}, true},
		{"one mismatch among many", sharing.ClassRule{Class: "x", Type: "note", Tag: "invoices", Profile: "personal"}, false},
		{"structured type absent", sharing.ClassRule{Class: "x", StructuredType: "person_note"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rule.Matches(obj); got != tc.want {
				t.Fatalf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
	if (sharing.ClassRule{Class: "x", Type: "note"}).Matches(nil) {
		t.Error("nil object matched")
	}
}

func TestClassRule_Validate(t *testing.T) {
	if err := (sharing.ClassRule{Class: "financial", Tag: "invoices"}).Validate(); err != nil {
		t.Errorf("valid rule: %v", err)
	}
	if err := (sharing.ClassRule{Class: "Bad Class", Tag: "x"}).Validate(); err == nil {
		t.Error("bad class accepted")
	}
	if err := (sharing.ClassRule{Class: "financial"}).Validate(); err == nil {
		t.Error("rule without a matcher accepted")
	}
	if err := sharing.ValidateClassRules([]sharing.ClassRule{{Class: "a", Type: "x"}, {Class: "b"}}); err == nil {
		t.Error("ValidateClassRules missed the second rule")
	}
}

func TestStructuredType(t *testing.T) {
	for name, v := range map[string]any{
		"struct":  structured{Type: "idea"},
		"pointer": &structured{Type: "idea"},
		"map":     map[string]any{"type": "idea", "topics": []any{"x"}},
	} {
		if got := sharing.StructuredType(&storage.KnowledgeObject{Metadata: map[string]any{"enrichment.structured_metadata": v}}); got != "idea" {
			t.Errorf("%s: StructuredType = %q, want idea", name, got)
		}
	}
	for name, v := range map[string]any{"nil": nil, "string": "idea", "number": 3} {
		if got := sharing.StructuredType(&storage.KnowledgeObject{Metadata: map[string]any{"enrichment.structured_metadata": v}}); got != "" {
			t.Errorf("%s: StructuredType = %q, want empty", name, got)
		}
	}
}

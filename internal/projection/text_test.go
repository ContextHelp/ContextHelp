package projection_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestBodyText_DefaultsToRawContent(t *testing.T) {
	cases := map[string]struct {
		ko   *storage.KnowledgeObject
		want string
	}{
		"nil":           {nil, ""},
		"empty":         {&storage.KnowledgeObject{}, ""},
		"raw only":      {&storage.KnowledgeObject{RawContent: "raw body"}, "raw body"},
		"text wins":     {&storage.KnowledgeObject{RawContent: "raw body", TextContent: "clean body"}, "clean body"},
		"text only":     {&storage.KnowledgeObject{TextContent: "clean body"}, "clean body"},
		"blank text ok": {&storage.KnowledgeObject{RawContent: "raw", TextContent: " "}, " "},
		"raw ref":       {&storage.KnowledgeObject{RawContent: "blob://abc123"}, ""},
		"raw ref, text": {&storage.KnowledgeObject{RawContent: "blob://abc123", TextContent: "clean body"}, "clean body"},
		"text ref":      {&storage.KnowledgeObject{RawContent: "raw body", TextContent: "blob://abc123"}, "raw body"},
		"both refs":     {&storage.KnowledgeObject{RawContent: "blob://abc123", TextContent: "blob://abc123"}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := projection.BodyText(tc.ko); got != tc.want {
				t.Errorf("BodyText = %q, want %q", got, tc.want)
			}
		})
	}
}

// A draft still in the pipeline has only RawContent: storage defaults
// TextContent later, so the embedding text must apply the same rule now.
func TestEmbeddingText_RawContentBeforeStorageDefaults(t *testing.T) {
	ko := &storage.KnowledgeObject{ID: "obj-raw", RawContent: "short note about numbats"}
	if got := projection.EmbeddingText(ko); got != "short note about numbats" {
		t.Errorf("EmbeddingText = %q, want the raw body", got)
	}
	if ko.TextContent != "" {
		t.Error("EmbeddingText mutated the draft's TextContent")
	}
}

// text.short's graph carries tag and mention nodes but no summary or
// section: the body still comes from RawContent.
func TestEmbeddingText_TagOnlyGraphFallsBackToRawContent(t *testing.T) {
	ko := &storage.KnowledgeObject{
		ID:         "obj-tags",
		RawContent: "call alice about the quokka survey",
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{NodeType: storage.NodeTypeTag, Label: "quokka"},
			{NodeType: storage.NodeTypeEntityMention, Content: "@people.alice"},
		}},
	}
	if got := projection.EmbeddingText(ko); got != "call alice about the quokka survey" {
		t.Errorf("EmbeddingText = %q, want the raw body", got)
	}
}

// Summaries and sections keep contributing exactly as ProjectIndex does.
func TestEmbeddingText_MatchesProjectIndexOnceDefaulted(t *testing.T) {
	cases := map[string]*storage.KnowledgeObject{
		"flat with sections": {
			RawContent: "raw",
			Summaries:  []string{"summary"},
			Sections:   []storage.Section{{Content: "section"}},
		},
		"graph with sections": {
			RawContent: "raw",
			Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
				{NodeType: storage.NodeTypeSummary, Content: "root"},
				{NodeType: storage.NodeTypeSection, Content: "section"},
			}},
		},
		"text content set": {TextContent: "clean", RawContent: "raw", Summaries: []string{"summary"}},
	}
	for name, ko := range cases {
		t.Run(name, func(t *testing.T) {
			defaulted := *ko
			defaulted.TextContent = projection.BodyText(ko)
			want := projection.ProjectIndex(&defaulted).EmbeddingText
			if want == "" {
				t.Fatal("fixture projects to empty text")
			}
			if got := projection.EmbeddingText(ko); got != want {
				t.Errorf("EmbeddingText = %q, want %q", got, want)
			}
		})
	}
}

func TestEmbeddingText_EmptyObject(t *testing.T) {
	if got := projection.EmbeddingText(nil); got != "" {
		t.Errorf("nil: %q", got)
	}
	if got := projection.EmbeddingText(&storage.KnowledgeObject{}); got != "" {
		t.Errorf("empty: %q", got)
	}
}

// A blob:// ref points at content; it is never indexed or embedded,
// wherever it sits.
func TestIndexText_NeverABlobRef(t *testing.T) {
	cases := map[string]*storage.KnowledgeObject{
		"raw ref, no text": {RawContent: "blob://abc123", Summaries: []string{"numbat gist"}},
		"text ref":         {RawContent: "blob://abc123", TextContent: "blob://abc123", Summaries: []string{"numbat gist"}},
		"section ref":      {TextContent: "numbat gist", Sections: []storage.Section{{Content: "blob://abc123"}}},
		"graph section ref": {Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{NodeType: storage.NodeTypeSummary, Content: "numbat gist"},
			{NodeType: storage.NodeTypeSection, Content: "blob://abc123"},
		}}},
	}
	for name, ko := range cases {
		t.Run(name, func(t *testing.T) {
			idx := projection.ProjectIndex(ko)
			for field, got := range map[string]string{
				"FTSBody":                    idx.FTSBody,
				"ProjectIndex.EmbeddingText": idx.EmbeddingText,
				"EmbeddingText":              projection.EmbeddingText(ko),
			} {
				if got != "numbat gist" {
					t.Errorf("%s = %q, want only the text", field, got)
				}
			}
		})
	}
}

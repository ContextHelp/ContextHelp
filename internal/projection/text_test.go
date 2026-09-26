package projection_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestBodyText_DefaultsToRawContent(t *testing.T) {
	cases := map[string]struct {
		ko   *pluginapi.KnowledgeObject
		want string
	}{
		"nil":           {nil, ""},
		"empty":         {&pluginapi.KnowledgeObject{}, ""},
		"raw only":      {&pluginapi.KnowledgeObject{RawContent: "raw body"}, "raw body"},
		"text wins":     {&pluginapi.KnowledgeObject{RawContent: "raw body", TextContent: "clean body"}, "clean body"},
		"text only":     {&pluginapi.KnowledgeObject{TextContent: "clean body"}, "clean body"},
		"blank text ok": {&pluginapi.KnowledgeObject{RawContent: "raw", TextContent: " "}, " "},
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
	ko := &pluginapi.KnowledgeObject{ID: "obj-raw", RawContent: "short note about numbats"}
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
	ko := &pluginapi.KnowledgeObject{
		ID:         "obj-tags",
		RawContent: "call alice about the quokka survey",
		Graph: &pluginapi.ObjectGraph{Nodes: []pluginapi.GraphNode{
			{NodeType: pluginapi.NodeTypeTag, Label: "quokka"},
			{NodeType: pluginapi.NodeTypeEntityMention, Content: "@people.alice"},
		}},
	}
	if got := projection.EmbeddingText(ko); got != "call alice about the quokka survey" {
		t.Errorf("EmbeddingText = %q, want the raw body", got)
	}
}

// Summaries and sections keep contributing exactly as ProjectIndex does.
func TestEmbeddingText_MatchesProjectIndexOnceDefaulted(t *testing.T) {
	cases := map[string]*pluginapi.KnowledgeObject{
		"flat with sections": {
			RawContent: "raw",
			Summaries:  []string{"summary"},
			Sections:   []pluginapi.Section{{Content: "section"}},
		},
		"graph with sections": {
			RawContent: "raw",
			Graph: &pluginapi.ObjectGraph{Nodes: []pluginapi.GraphNode{
				{NodeType: pluginapi.NodeTypeSummary, Content: "root"},
				{NodeType: pluginapi.NodeTypeSection, Content: "section"},
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
	if got := projection.EmbeddingText(&pluginapi.KnowledgeObject{}); got != "" {
		t.Errorf("empty: %q", got)
	}
}

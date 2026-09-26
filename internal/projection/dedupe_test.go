package projection_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func summaryNode(s string) pluginapi.GraphNode {
	return pluginapi.GraphNode{NodeType: pluginapi.NodeTypeSummary, Content: s}
}

func sectionNode(s string, order int) pluginapi.GraphNode {
	return pluginapi.GraphNode{NodeType: pluginapi.NodeTypeSection, Content: s, Order: order}
}

// A root summary carrying the same text as the only section must be
// projected once, not as "X\nX".
func TestProjectIndex_SummaryEqualToSectionProjectedOnce(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID: "obj-dup",
		Graph: &pluginapi.ObjectGraph{Nodes: []pluginapi.GraphNode{
			summaryNode("the numbat eats termites"),
			sectionNode("the numbat eats termites", 0),
		}},
	}
	idx := projection.ProjectIndex(ko)
	if idx.EmbeddingText != "the numbat eats termites" {
		t.Errorf("EmbeddingText = %q, want the text once", idx.EmbeddingText)
	}
	if idx.FTSBody != "the numbat eats termites" {
		t.Errorf("FTSBody = %q, want the text once", idx.FTSBody)
	}
}

// Repeated sections collapse to their first occurrence; distinct segments
// keep their original order.
func TestProjectIndex_RepeatedSegmentsKeepFirstOccurrence(t *testing.T) {
	cases := map[string]struct {
		nodes []pluginapi.GraphNode
		want  []string
	}{
		"adjacent sections": {
			nodes: []pluginapi.GraphNode{sectionNode("alpha", 0), sectionNode("alpha", 1), sectionNode("beta", 2)},
			want:  []string{"alpha", "beta"},
		},
		"non-adjacent sections": {
			nodes: []pluginapi.GraphNode{sectionNode("alpha", 0), sectionNode("beta", 1), sectionNode("alpha", 2)},
			want:  []string{"alpha", "beta"},
		},
		"summary repeated later": {
			nodes: []pluginapi.GraphNode{summaryNode("root"), sectionNode("alpha", 0), sectionNode("root", 1), sectionNode("beta", 2)},
			want:  []string{"root", "alpha", "beta"},
		},
		"surrounding whitespace ignored": {
			nodes: []pluginapi.GraphNode{summaryNode("root\n"), sectionNode("  root", 0)},
			want:  []string{"root\n"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ko := &pluginapi.KnowledgeObject{ID: "obj", Graph: &pluginapi.ObjectGraph{Nodes: tc.nodes}}
			idx := projection.ProjectIndex(ko)
			if want := strings.Join(tc.want, "\n"); idx.EmbeddingText != want {
				t.Errorf("EmbeddingText = %q, want %q", idx.EmbeddingText, want)
			}
			if want := strings.Join(tc.want, " "); idx.FTSBody != want {
				t.Errorf("FTSBody = %q, want %q", idx.FTSBody, want)
			}
		})
	}
}

// Flat fields: a body equal to its summary or a section is projected once.
func TestProjectIndex_FlatBodyEqualToSummaryAndSection(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID:          "obj-flat",
		TextContent: "short note",
		Summaries:   []string{"short note", "gist"},
		Sections:    []pluginapi.Section{{Content: "short note"}, {Content: "gist"}, {Content: "tail"}},
	}
	idx := projection.ProjectIndex(ko)
	if want := "short note\ngist\ntail"; idx.EmbeddingText != want {
		t.Errorf("EmbeddingText = %q, want %q", idx.EmbeddingText, want)
	}
	if want := "short note gist tail"; idx.FTSBody != want {
		t.Errorf("FTSBody = %q, want %q", idx.FTSBody, want)
	}
}

// The body default (RawContent standing in for TextContent) must not add a
// second copy of a summary that repeats it.
func TestEmbeddingText_DefaultedBodyEqualToSummaryNotRepeated(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID:         "obj-raw",
		RawContent: "call alice",
		Summaries:  []string{"call alice"},
	}
	if got := projection.EmbeddingText(ko); got != "call alice" {
		t.Errorf("EmbeddingText = %q, want %q", got, "call alice")
	}
}

// projectionGolden digests ProjectIndex's FTS body and embedding text over
// projectionCorpus, keyed by indexsig.ProjectionVersion. Stored FTS bodies
// were projected by the code of their day; only a version bump tells an
// existing index its bodies are stale. When this test fails, the projection
// changed: bump indexsig.ProjectionVersion and add its digest here.
var projectionGolden = map[string]string{
	"v2": "909d8cb69b6e32369ec136ee652f1c0149ae8a792fff40e1d249a548e471359a",
}

var projectionCorpus = []*pluginapi.KnowledgeObject{
	{TextContent: "body", Summaries: []string{"body", "gist"}, Sections: []pluginapi.Section{{Content: "gist"}, {Content: " tail "}}},
	{RawContent: "raw only"},
	{TextContent: "text", Graph: &pluginapi.ObjectGraph{Nodes: []pluginapi.GraphNode{
		{NodeType: pluginapi.NodeTypeTag, Label: "tag"},
		{NodeType: pluginapi.NodeTypeEntityMention, Content: "@people.alice"},
	}}},
	{Graph: &pluginapi.ObjectGraph{Nodes: []pluginapi.GraphNode{
		summaryNode("root"), sectionNode("root", 0), sectionNode("alpha", 1),
		sectionNode("", 2), sectionNode("alpha", 3), sectionNode("beta", 4),
	}}},
}

func TestProjectIndex_OutputPinnedToProjectionVersion(t *testing.T) {
	h := sha256.New()
	for _, ko := range projectionCorpus {
		idx := projection.ProjectIndex(ko)
		h.Write([]byte(idx.FTSBody + "\x00" + idx.EmbeddingText + "\x00"))
	}
	got := hex.EncodeToString(h.Sum(nil))
	want, ok := projectionGolden[indexsig.ProjectionVersion]
	if !ok {
		t.Fatalf("no golden digest for projection %s; add %q", indexsig.ProjectionVersion, got)
	}
	if got != want {
		t.Fatalf("projection output changed under %s (digest %s, want %s): bump indexsig.ProjectionVersion",
			indexsig.ProjectionVersion, got, want)
	}
}

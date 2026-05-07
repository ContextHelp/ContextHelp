package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestMarkdownParserCreatesHeadingSections(t *testing.T) {
	step := NewMarkdownParser()
	draft := &storage.KnowledgeObject{
		RawContent: "# Title\n\nSome content\n\n## Section\n\nMore content",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 2 {
		t.Errorf("expected 2 sections, got %d", len(got.Sections))
	}
	if got.Sections[0].Title != "Title" {
		t.Errorf("first section title: %q", got.Sections[0].Title)
	}
	if got.Metadata["markdown_heading_count"] != 2 {
		t.Errorf("heading count: %v", got.Metadata["markdown_heading_count"])
	}
}

func TestMarkdownParserNoHeadings(t *testing.T) {
	step := NewMarkdownParser()
	draft := &storage.KnowledgeObject{
		RawContent: "Just plain text, no headings.",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(got.Sections))
	}
}

func TestMarkdownParserName(t *testing.T) {
	step := NewMarkdownParser()
	if step.Name() != "markdown_parser" {
		t.Errorf("name: %q", step.Name())
	}
}

func TestMarkdownParserEmitsGraphNodes(t *testing.T) {
	step := NewMarkdownParser()
	draft := &storage.KnowledgeObject{
		ID:         "obj-004",
		RawContent: "# Title\n\nSome content\n\n## Section\n\nMore content",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}

	rootID := pluginapi.NewNodeID("obj-004", pluginapi.NodeTypeSummary, 0)
	if got.Graph.FindNode(rootID) == nil {
		t.Errorf("root summary node %q not found", rootID)
	}

	// Two headings → two section nodes.
	for i := 0; i < 2; i++ {
		secID := pluginapi.NewNodeID("obj-004", pluginapi.NodeTypeSection, i)
		if got.Graph.FindNode(secID) == nil {
			t.Errorf("section node %q not found", secID)
		}
	}

	if len(got.Graph.Edges) != 2 {
		t.Errorf("edges: got %d, want 2", len(got.Graph.Edges))
	}
	for _, e := range got.Graph.Edges {
		if e.EdgeType != pluginapi.EdgeTypeContains {
			t.Errorf("edge type: got %q, want contains", e.EdgeType)
		}
	}
}

func TestMarkdownParserNoGraphWithoutID(t *testing.T) {
	step := NewMarkdownParser()
	draft := &storage.KnowledgeObject{
		RawContent: "# Title\n\ncontent",
	}
	got, _ := step.Run(context.Background(), draft)
	if got.Graph != nil {
		t.Error("expected nil graph when ID is empty")
	}
}

func TestMarkdownParserNoGraphOnNoHeadings(t *testing.T) {
	step := NewMarkdownParser()
	draft := &storage.KnowledgeObject{
		ID:         "obj-005",
		RawContent: "Just plain text.",
	}
	got, _ := step.Run(context.Background(), draft)
	if got.Graph != nil {
		t.Error("expected nil graph when there are no headings")
	}
}

// TestHasMarkdownStructure exercises the predicate consumed by the
// pipeline selector (T-0575). True positives: any heading, or >=5
// bullets. True negatives: prose, single-bullet snippets, hyphen-only
// lines that are not real bullets.
func TestHasMarkdownStructure(t *testing.T) {
	cases := []struct {
		desc string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"plain prose", "Just a regular sentence about today's weather.", false},
		{"single heading", "# Title\n\nbody", true},
		{"second-level heading", "## Notes\nbody", true},
		{"heading mid-document", "intro line\n\n### Mid heading\n\nmore", true},
		{"four bullets (under threshold)", "- a\n- b\n- c\n- d\n", false},
		{"five bullets (threshold)", "- a\n- b\n- c\n- d\n- e\n", true},
		{"asterisk bullets", "* one\n* two\n* three\n* four\n* five\n", true},
		{"plus bullets", "+ one\n+ two\n+ three\n+ four\n+ five\n", true},
		{"indented bullets", "  - a\n  - b\n  - c\n  - d\n  - e\n", true},
		{"em dash prose, not bullets", "Some text — with em dashes — and more text — and more — and more — text.", false},
		{"hyphen with no space, not a bullet", "-foo\n-bar\n-baz\n-quux\n-zot\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := HasMarkdownStructure(tc.in)
			if got != tc.want {
				t.Errorf("HasMarkdownStructure(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

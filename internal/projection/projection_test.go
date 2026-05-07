package projection_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestProjectDocument_SectionsFromGraph(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID:   "obj-1",
		Type: "note",
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID("obj-1", pluginapi.NodeTypeSection, 0),
					NodeType: pluginapi.NodeTypeSection,
					Label:    "Intro",
					Content:  "Intro body",
					Order:    0,
				},
				{
					ID:       pluginapi.NewNodeID("obj-1", pluginapi.NodeTypeSection, 1),
					NodeType: pluginapi.NodeTypeSection,
					Label:    "Conclusion",
					Content:  "Conclusion body",
					Order:    1,
				},
			},
		},
	}
	doc := projection.ProjectDocument(ko)
	if len(doc.Sections) != 2 {
		t.Fatalf("want 2 sections, got %d", len(doc.Sections))
	}
	// sections must be sorted by Order
	if doc.Sections[0].Title != "Intro" {
		t.Errorf("section[0] title: got %q", doc.Sections[0].Title)
	}
	if doc.Sections[1].Title != "Conclusion" {
		t.Errorf("section[1] title: got %q", doc.Sections[1].Title)
	}
}

func TestProjectDocument_FallbackToFlatFields(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID:       "obj-2",
		Sections: []pluginapi.Section{{Title: "Old", Content: "old body", Order: 0}},
	}
	doc := projection.ProjectDocument(ko)
	if len(doc.Sections) != 1 {
		t.Fatalf("want 1 section (fallback), got %d", len(doc.Sections))
	}
	if doc.Sections[0].Title != "Old" {
		t.Errorf("got %q", doc.Sections[0].Title)
	}
}

func TestProjectIndex_FTSBodyFromGraph(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID: "obj-3",
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{NodeType: pluginapi.NodeTypeSummary, Content: "summary text"},
				{NodeType: pluginapi.NodeTypeSection, Content: "section text"},
			},
		},
	}
	idx := projection.ProjectIndex(ko)
	if idx.FTSBody == "" {
		t.Fatal("FTSBody empty")
	}
	// both summary and section content must appear
	if !contains(idx.FTSBody, "summary text") {
		t.Errorf("FTSBody missing summary: %q", idx.FTSBody)
	}
	if !contains(idx.FTSBody, "section text") {
		t.Errorf("FTSBody missing section: %q", idx.FTSBody)
	}
}

func TestProjectIndex_TagsFromGraph(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID: "obj-4",
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{NodeType: pluginapi.NodeTypeTag, Label: "go", Content: "go"},
				{NodeType: pluginapi.NodeTypeTag, Label: "storage", Content: "storage"},
			},
		},
	}
	idx := projection.ProjectIndex(ko)
	if len(idx.Tags) != 2 {
		t.Fatalf("want 2 tags, got %d", len(idx.Tags))
	}
}

func TestProjectIndex_MentionsFromGraph(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID: "obj-5",
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{NodeType: pluginapi.NodeTypeEntityMention, Content: "@people.alice"},
				{NodeType: pluginapi.NodeTypeEntityMention, Content: "@project.foo"},
			},
		},
	}
	idx := projection.ProjectIndex(ko)
	if len(idx.Mentions) != 2 {
		t.Fatalf("want 2 mentions, got %d", len(idx.Mentions))
	}
}

func TestProjectIndex_FallbackToFlatFields(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID:          "obj-6",
		TextContent: "flat text",
		Tags:        []pluginapi.Tag{{Label: "flat-tag"}},
	}
	idx := projection.ProjectIndex(ko)
	if !contains(idx.FTSBody, "flat text") {
		t.Errorf("fallback FTSBody missing text: %q", idx.FTSBody)
	}
	if len(idx.Tags) != 1 {
		t.Fatalf("want 1 tag (fallback), got %d", len(idx.Tags))
	}
}

// TestProjectIndex_GraphWithoutSummaryStillIndexesText covers the T-0565 bug:
// pipelines like text.short produce a graph that has Tag and EntityMention
// nodes but no Summary or Section nodes. Before the fix, ProjectIndex took
// the graph branch and produced an empty FTSBody — the document was invisible
// to FTS even though TextContent and RawContent held the full body.
func TestProjectIndex_GraphWithoutSummaryStillIndexesText(t *testing.T) {
	ko := &pluginapi.KnowledgeObject{
		ID:          "obj-7",
		TextContent: "- AWS SageMaker\n- AWS Lambda\n- VMware Cloud",
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{NodeType: pluginapi.NodeTypeTag, Label: "aws", Content: "aws"},
				{NodeType: pluginapi.NodeTypeEntityMention, Content: "@vendor.aws"},
			},
		},
	}
	idx := projection.ProjectIndex(ko)
	if !contains(idx.FTSBody, "SageMaker") {
		t.Errorf("FTSBody missing list item — graph-without-summary case must fall back to TextContent\nFTSBody=%q", idx.FTSBody)
	}
	if !contains(idx.FTSBody, "VMware") {
		t.Errorf("FTSBody missing list item: %q", idx.FTSBody)
	}
	// Tags from the graph must still be picked up (no regression).
	if len(idx.Tags) != 1 || idx.Tags[0].Label != "aws" {
		t.Errorf("tags from graph lost: got %+v", idx.Tags)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

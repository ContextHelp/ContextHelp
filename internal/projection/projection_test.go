package projection_test

import (
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestProjectDocument_SectionsFromGraph(t *testing.T) {
	ko := &storage.KnowledgeObject{
		ID:   "obj-1",
		Type: "note",
		Graph: &storage.ObjectGraph{
			Nodes: []storage.GraphNode{
				{
					ID:       storage.NewNodeID("obj-1", storage.NodeTypeSection, 0),
					NodeType: storage.NodeTypeSection,
					Label:    "Intro",
					Content:  "Intro body",
					Order:    0,
				},
				{
					ID:       storage.NewNodeID("obj-1", storage.NodeTypeSection, 1),
					NodeType: storage.NodeTypeSection,
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
	ko := &storage.KnowledgeObject{
		ID:       "obj-2",
		Sections: []storage.Section{{Title: "Old", Content: "old body", Order: 0}},
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
	ko := &storage.KnowledgeObject{
		ID: "obj-3",
		Graph: &storage.ObjectGraph{
			Nodes: []storage.GraphNode{
				{NodeType: storage.NodeTypeSummary, Content: "summary text"},
				{NodeType: storage.NodeTypeSection, Content: "section text"},
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
	ko := &storage.KnowledgeObject{
		ID: "obj-4",
		Graph: &storage.ObjectGraph{
			Nodes: []storage.GraphNode{
				{NodeType: storage.NodeTypeTag, Label: "go", Content: "go"},
				{NodeType: storage.NodeTypeTag, Label: "storage", Content: "storage"},
			},
		},
	}
	idx := projection.ProjectIndex(ko)
	if len(idx.Tags) != 2 {
		t.Fatalf("want 2 tags, got %d", len(idx.Tags))
	}
}

func TestProjectIndex_MentionsFromGraph(t *testing.T) {
	ko := &storage.KnowledgeObject{
		ID: "obj-5",
		Graph: &storage.ObjectGraph{
			Nodes: []storage.GraphNode{
				{NodeType: storage.NodeTypeEntityMention, Content: "@people.alice"},
				{NodeType: storage.NodeTypeEntityMention, Content: "@project.foo"},
			},
		},
	}
	idx := projection.ProjectIndex(ko)
	if len(idx.Mentions) != 2 {
		t.Fatalf("want 2 mentions, got %d", len(idx.Mentions))
	}
}

func TestProjectIndex_FallbackToFlatFields(t *testing.T) {
	ko := &storage.KnowledgeObject{
		ID:          "obj-6",
		TextContent: "flat text",
		Tags:        []storage.Tag{{Label: "flat-tag"}},
	}
	idx := projection.ProjectIndex(ko)
	if !contains(idx.FTSBody, "flat text") {
		t.Errorf("fallback FTSBody missing text: %q", idx.FTSBody)
	}
	if len(idx.Tags) != 1 {
		t.Fatalf("want 1 tag (fallback), got %d", len(idx.Tags))
	}
}

// TestProjectIndex_GraphWithoutSummaryStillIndexesText covers a regression:
// pipelines like text.short produce a graph that has Tag and EntityMention
// nodes but no Summary or Section nodes. Before the fix, ProjectIndex took
// the graph branch and produced an empty FTSBody — the document was invisible
// to FTS even though TextContent and RawContent held the full body.
func TestProjectIndex_GraphWithoutSummaryStillIndexesText(t *testing.T) {
	ko := &storage.KnowledgeObject{
		ID:          "obj-7",
		TextContent: "- AWS SageMaker\n- AWS Lambda\n- VMware Cloud",
		Graph: &storage.ObjectGraph{
			Nodes: []storage.GraphNode{
				{NodeType: storage.NodeTypeTag, Label: "aws", Content: "aws"},
				{NodeType: storage.NodeTypeEntityMention, Content: "@vendor.aws"},
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

func mediaDraft(t *testing.T) *storage.KnowledgeObject {
	t.Helper()
	d := &storage.KnowledgeObject{ID: "vid-1"}
	emit := func(s representation.Spec) {
		t.Helper()
		if _, err := representation.Emit(d, s); err != nil {
			t.Fatal(err)
		}
	}
	emit(representation.Spec{Kind: representation.Transcript, Label: "Segment 1", Content: "alpha spoken", Anchor: representation.Anchor{}.WithSpan(0, 1000)})
	emit(representation.Spec{Kind: representation.Transcript, Label: "Segment 2", Content: "beta spoken", Anchor: representation.Anchor{}.WithSpan(1000, 2000)})
	emit(representation.Spec{Kind: representation.FrameOCR, Label: "Frame 0 OCR", Content: "slide text", Anchor: representation.Anchor{}.WithFrame(0, 500)})
	emit(representation.Spec{Kind: representation.Summary, Label: "Summary", Content: "overview of talk"})
	return d
}

func TestProjectIndex_RepresentationOrderAndNoDoubling(t *testing.T) {
	d := mediaDraft(t)
	p := projection.ProjectIndex(d)
	want := "overview of talk\nalpha spoken\nbeta spoken\nslide text"
	if p.EmbeddingText != want {
		t.Errorf("EmbeddingText:\n got %q\nwant %q", p.EmbeddingText, want)
	}
	if strings.Count(p.EmbeddingText, "alpha spoken") != 1 {
		t.Error("transcript text indexed more than once")
	}
	if p.FTSBody != "overview of talk alpha spoken beta spoken slide text" {
		t.Errorf("FTSBody: %q", p.FTSBody)
	}
}

func TestProjectIndex_FrameOCRNotDropped(t *testing.T) {
	// Regression: with graph nodes present, flat-section-only producers
	// (timeline_assembler frame OCR) were never indexed.
	d := mediaDraft(t)
	d.Sections = []storage.Section{{Title: "Frame OCR", Content: "flat only", Order: 0}}
	p := projection.ProjectIndex(d)
	if !strings.Contains(p.EmbeddingText, "slide text") {
		t.Errorf("frame OCR node missing: %q", p.EmbeddingText)
	}
	if strings.Contains(p.EmbeddingText, "flat only") {
		t.Error("graph-first: flat sections are not read when nodes exist")
	}
}

func TestProjectDocument_RepresentationMetadata(t *testing.T) {
	d := mediaDraft(t)
	doc := projection.ProjectDocument(d)
	if len(doc.Sections) != 3 { // summary nodes are not document sections
		t.Fatalf("sections: %d", len(doc.Sections))
	}
	if doc.Sections[0].Metadata["representation"] != "transcript" || doc.Sections[0].Metadata["start_ms"] != int64(0) {
		t.Errorf("metadata not carried: %v", doc.Sections[0].Metadata)
	}
}

func frameDraft(t *testing.T) *storage.KnowledgeObject {
	t.Helper()
	d := &storage.KnowledgeObject{ID: "vid-9"}
	for _, s := range []representation.Spec{
		{Kind: representation.FrameOCR, Label: "f1 ocr", Content: "f1 ocr", Anchor: representation.Anchor{}.WithFrameIndex(1)},
		{Kind: representation.FrameCaption, Label: "f0 cap", Content: "f0 cap", Anchor: representation.Anchor{}.WithFrameIndex(0)},
		{Kind: representation.Transcript, Label: "t", Content: "spoken", Anchor: representation.Anchor{}.WithSpan(0, 1)},
	} {
		if _, err := representation.Emit(d, s); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestProjectDocument_SectionsInRepresentationOrder(t *testing.T) {
	var got []string
	for _, s := range projection.ProjectDocument(frameDraft(t)).Sections {
		got = append(got, s.Content)
	}
	want := []string{"spoken", "f0 cap", "f1 ocr"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("document order %v, want %v", got, want)
	}
}

func TestMaskedProjection_RepresentationOrder(t *testing.T) {
	d := frameDraft(t)
	// Withhold the frame 0 caption; the rest keeps projection order.
	for i := range d.Graph.Nodes {
		if d.Graph.Nodes[i].Content == "f0 cap" {
			d.Graph.Nodes[i].Metadata[storage.NodeMetadataClasses] = []string{storage.ClassPersonal}
		}
	}
	if got := projection.ProjectIndex(d).MaskedEmbeddingText; got != "spoken\nf1 ocr" {
		t.Errorf("masked = %q", got)
	}
}

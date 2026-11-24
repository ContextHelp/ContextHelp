package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

func requireUniqueIDs(t *testing.T, g *storage.ObjectGraph) {
	t.Helper()
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		if seen[n.ID] {
			t.Errorf("duplicate node id %q", n.ID)
		}
		seen[n.ID] = true
	}
}

func sectionNodes(g *storage.ObjectGraph) []storage.GraphNode {
	var out []storage.GraphNode
	for _, n := range g.Nodes {
		if n.NodeType == storage.NodeTypeSection {
			out = append(out, n)
		}
	}
	return out
}

// Regression: heading_splitter walked every draft section, so sections an
// earlier step had already emitted were emitted again under new ordinals.
func TestHeadingSplitterAfterSectionerEmitsEachSectionOnce(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "h-1", RawContent: "## A\nalpha\n## B\nbeta\n"}
	d = run(t, NewSectioner(), d)
	d = run(t, NewHeadingSplitter(), d)
	if got, want := len(sectionNodes(d.Graph)), len(d.Sections); got != want {
		t.Errorf("section nodes = %d, want one per section (%d)", got, want)
	}
	requireUniqueIDs(t, d.Graph)
}

// A section already in the graph is not emitted again by markdown_parser.
func TestMarkdownParserSkipsSectionsAlreadyInGraph(t *testing.T) {
	d := &storage.KnowledgeObject{
		ID:         "m-1",
		RawContent: "## A\nalpha\n## B\nbeta\n",
		Sections:   []storage.Section{{Title: "Pre", Content: "pre", Order: 0}},
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{ID: "m-1/section/0", NodeType: storage.NodeTypeSection, Label: "Pre", Content: "pre", Order: 0},
		}},
	}
	d = run(t, NewMarkdownParser(), d)
	if got := len(sectionNodes(d.Graph)); got != 3 {
		t.Errorf("section nodes = %d, want 3 (Pre kept once, A, B)", got)
	}
	requireUniqueIDs(t, d.Graph)
}

// Every step that adds artifact nodes leaves artifact/0 to the stored
// original: with one attached it survives, without one the first artifact
// is ordinal 1.
func TestArtifactEmittersLeaveOrdinalZeroToTheOriginal(t *testing.T) {
	attach := func(d *storage.KnowledgeObject) {
		o := blob.Original{Key: blob.OriginalKey("x"), SHA256: blob.OriginalKey("x"), Size: 1}
		if err := blob.AttachOriginal(d, o, "x"); err != nil {
			t.Fatal(err)
		}
	}
	deps := map[string]any{"dep_files": map[string]any{"package.json": `{"dependencies": {"express": "^4"}}`}}
	llm := &mockLLM{resp: `{"summary":"s","topics":[],"artifacts":["go.mod"]}`}
	for name, run := range map[string]func(*storage.KnowledgeObject) (*storage.KnowledgeObject, error){
		"dependency_enricher": func(d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
			d.Metadata = deps
			return NewDependencyEnricher().Run(context.Background(), d)
		},
		"graph_extractor": func(d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
			d.RawContent = "some content"
			return NewGraphExtractorWithLLM(llm).Run(context.Background(), d)
		},
	} {
		d := &storage.KnowledgeObject{ID: "a-1-" + name}
		attach(d)
		got, err := run(d)
		if err != nil {
			t.Fatal(name, err)
		}
		requireUniqueIDs(t, got.Graph)
		if o, ok := blob.OriginalOf(got); !ok || o.Key != blob.OriginalKey("x") {
			t.Errorf("%s: original lost: %+v %v", name, o, ok)
		}

		bare := &storage.KnowledgeObject{ID: "a-2-" + name}
		got, err = run(bare)
		if err != nil {
			t.Fatal(name, err)
		}
		for _, n := range got.Graph.Nodes {
			if n.NodeType == storage.NodeTypeArtifact && n.ID == storage.NewNodeID(bare.ID, storage.NodeTypeArtifact, 0) {
				t.Errorf("%s took artifact/0 without an original", name)
			}
		}
	}
}

func TestFeedParserSectionsDoNotCollideWithExistingNodes(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "f-1", RawContent: testRSS, Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
		{ID: "f-1/section/0", NodeType: storage.NodeTypeSection, Content: "earlier", Order: 0},
	}}}
	d = run(t, NewFeedParser(), d)
	requireUniqueIDs(t, d.Graph)
	if len(sectionNodes(d.Graph)) != 3 {
		t.Errorf("section nodes = %d, want earlier + 2 items", len(sectionNodes(d.Graph)))
	}
}

// A representation summary at summary/0 is not the object's root: the
// cleaner must not add a second summary/0 and the sectioner must not hang
// its sections off the representation.
func TestSummaryOnlyRepresentationGraphKeepsRootsSeparate(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "r-1", RawContent: "alpha\n\n## H\nbeta"}
	if _, err := representation.Emit(d, representation.Spec{Kind: representation.Summary, Content: "an overview"}); err != nil {
		t.Fatal(err)
	}
	d = run(t, NewTextCleaner(), d)
	d = run(t, NewSectioner(), d)
	requireUniqueIDs(t, d.Graph)
	repSummary := "r-1/summary/0"
	if n := d.Graph.FindNode(repSummary); n == nil || n.Metadata[representation.KeyRepresentation] != "summary" {
		t.Fatalf("the representation summary must stay at %s: %+v", repSummary, n)
	}
	for _, e := range d.Graph.Edges {
		if e.FromID == repSummary {
			t.Errorf("sections hang off the representation summary: %+v", e)
		}
	}
	if len(sectionNodes(d.Graph)) == 0 {
		t.Error("sectioner emitted no sections")
	}
}

// Representation steps never create artifact nodes or edges themselves;
// the stored original is the blob store's.
func TestRepresentationStepsCreateNoArtifactWithoutOriginal(t *testing.T) {
	mk := func(id string) *storage.KnowledgeObject {
		return &storage.KnowledgeObject{ID: id, RawContent: "img", ContentType: "image/png"}
	}
	cases := map[string]func() (*storage.KnowledgeObject, error){
		"ocr": func() (*storage.KnowledgeObject, error) { return NewOCRExtractor().Run(context.Background(), mk("n1")) },
		"vision": func() (*storage.KnowledgeObject, error) {
			return NewVisionAnalyzer().Run(context.Background(), mk("n2"))
		},
		"transcriber": func() (*storage.KnowledgeObject, error) {
			d := mk("n3")
			d.Source = "/x.mp3"
			return NewAudioTranscriber(WithTranscriptionProvider(fakeTranscriber{})).Run(context.Background(), d)
		},
		"frame_ocr": func() (*storage.KnowledgeObject, error) {
			d := mk("n4")
			d.Metadata = map[string]any{"frame_paths": []string{"/f.png"}}
			return NewFrameOCR(WithFrameOCRProvider(fakeOCR{})).Run(context.Background(), d)
		},
		"pdf": func() (*storage.KnowledgeObject, error) {
			d := mk("n5")
			d.Subtype = subtypePDF
			return NewPDFExtractor(WithDocumentProvider(fakeDoc{})).Run(context.Background(), d)
		},
	}
	for name, run := range cases {
		got, err := run()
		if err != nil {
			t.Fatal(name, err)
		}
		for _, n := range got.Graph.Nodes {
			if n.NodeType == storage.NodeTypeArtifact {
				t.Errorf("%s created artifact %s", name, n.ID)
			}
		}
		if len(got.Graph.Edges) != 0 {
			t.Errorf("%s: edges %+v without an original", name, got.Graph.Edges)
		}
	}
}

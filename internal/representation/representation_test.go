package representation_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func draftWithID() *storage.KnowledgeObject {
	return &storage.KnowledgeObject{ID: "obj-1"}
}

func TestKindsClosedSet(t *testing.T) {
	want := []representation.Kind{
		"caption", "ocr", "transcript", "frame_caption", "frame_ocr", "summary", "extracted_text",
	}
	got := representation.Kinds()
	if len(got) != len(want) {
		t.Fatalf("kinds: got %v want %v", got, want)
	}
	for i, k := range want {
		if got[i] != k || !k.Valid() {
			t.Errorf("kind[%d]: got %q want %q", i, got[i], k)
		}
	}
	// sound_description was dropped from the set (owner decision 6).
	for _, bad := range []representation.Kind{"", "sound_description", "Caption", "audio"} {
		if bad.Valid() {
			t.Errorf("kind %q must be invalid", bad)
		}
	}
}

func TestKindNodeType(t *testing.T) {
	if representation.Summary.NodeType() != storage.NodeTypeSummary {
		t.Error("summary representations are summary nodes")
	}
	for _, k := range representation.Kinds() {
		if k == representation.Summary {
			continue
		}
		if k.NodeType() != storage.NodeTypeSection {
			t.Errorf("%s: want section node", k)
		}
	}
}

func TestEmitWritesProvenanceKeys(t *testing.T) {
	d := draftWithID()
	conf := 0.8
	n, err := representation.Emit(d, representation.Spec{
		Kind:       representation.Transcript,
		Label:      "Segment 1",
		Content:    "hello world",
		Anchor:     representation.Anchor{}.WithSpan(0, 5000).WithSpeaker("S1"),
		Provider:   "whisper",
		Model:      "large-v3-turbo",
		Confidence: &conf,
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if n.NodeType != storage.NodeTypeSection || n.ID != "obj-1/section/0" {
		t.Errorf("node: %+v", n)
	}
	if d.Graph.FindNode(n.ID) == nil {
		t.Fatal("node not appended to the draft graph")
	}
	m := n.Metadata
	if m["representation"] != "transcript" || m["provider"] != "whisper" ||
		m["model"] != "large-v3-turbo" || m["speaker"] != "S1" {
		t.Errorf("metadata: %v", m)
	}
	if m["start_ms"] != int64(0) || m["end_ms"] != int64(5000) {
		t.Errorf("zero start_ms must be written: %v", m)
	}
	if m["confidence"] != 0.8 {
		t.Errorf("confidence: %v", m["confidence"])
	}
	if _, ok := m["origin"]; ok {
		t.Error("origin must be absent unless set")
	}
	if _, ok := m["page_number"]; ok {
		t.Error("unset anchors must be absent")
	}
}

func TestEmitSharedOrdinalAllocator(t *testing.T) {
	d := draftWithID()
	// A prior step (markdown, sectioner...) already took section/0..2.
	for i := range 3 {
		d.Graph = appendNode(d.Graph, storage.GraphNode{
			ID: storage.NewNodeID("obj-1", storage.NodeTypeSection, i), NodeType: storage.NodeTypeSection,
			Content: "s", Order: i,
		})
	}
	a, err := representation.Emit(d, representation.Spec{Kind: representation.OCR, Content: "ocr"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := representation.Emit(d, representation.Spec{Kind: representation.Caption, Content: "cap"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "obj-1/section/3" || b.ID != "obj-1/section/4" {
		t.Errorf("ids: %q %q", a.ID, b.ID)
	}
	ids := map[string]bool{}
	for _, n := range d.Graph.Nodes {
		if ids[n.ID] {
			t.Fatalf("duplicate node id %q", n.ID)
		}
		ids[n.ID] = true
	}
	// A summary representation allocates from the summary counter.
	s, err := representation.Emit(d, representation.Spec{Kind: representation.Summary, Content: "sum"})
	if err != nil || s.ID != "obj-1/summary/0" || s.NodeType != storage.NodeTypeSummary {
		t.Errorf("summary: %+v %v", s, err)
	}
}

func TestNextOrdinalReadsIDsNotCounts(t *testing.T) {
	g := &storage.ObjectGraph{Nodes: []storage.GraphNode{
		{ID: "o/section/5", NodeType: storage.NodeTypeSection},
	}}
	if got := storage.NextOrdinal(g, storage.NodeTypeSection); got != 6 {
		t.Errorf("got %d want 6 (max ordinal + 1, never a count)", got)
	}
	if got := storage.NextOrdinal(nil, storage.NodeTypeSection); got != 0 {
		t.Errorf("nil graph: got %d", got)
	}
}

func TestEmitRefusesBadSpecs(t *testing.T) {
	bad := 1.5
	neg := int64(-1)
	cases := map[string]representation.Spec{
		"unknown kind":      {Kind: "sound_description", Content: "x"},
		"empty kind":        {Content: "x"},
		"empty content":     {Kind: representation.OCR, Content: "  \n"},
		"confidence range":  {Kind: representation.OCR, Content: "x", Confidence: &bad},
		"negative start":    {Kind: representation.Transcript, Content: "x", Anchor: representation.Anchor{StartMS: &neg}},
		"end before start":  {Kind: representation.Transcript, Content: "x", Anchor: representation.Anchor{}.WithSpan(9, 3)},
		"frame needs index": {Kind: representation.FrameOCR, Content: "x"},
		"reserved extra":    {Kind: representation.OCR, Content: "x", Extra: map[string]any{"provider": "evil"}},
		"unknown origin":    {Kind: representation.Transcript, Content: "x", Origin: "cloud"},
	}
	for name, spec := range cases {
		d := draftWithID()
		if _, err := representation.Emit(d, spec); err == nil {
			t.Errorf("%s: want error", name)
		}
		if d.Graph != nil && len(d.Graph.Nodes) != 0 {
			t.Errorf("%s: a refused spec must not touch the graph", name)
		}
	}
	if _, err := representation.Emit(&storage.KnowledgeObject{}, representation.Spec{Kind: representation.OCR, Content: "x"}); !errors.Is(err, representation.ErrNoObjectID) {
		t.Errorf("no id: %v", err)
	}
}

func TestEmitFrameAndSidecarAnchors(t *testing.T) {
	d := draftWithID()
	n, err := representation.Emit(d, representation.Spec{
		Kind: representation.FrameOCR, Content: "slide title",
		Anchor: representation.Anchor{}.WithFrame(7, 12000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.Metadata["frame_index"] != 7 || n.Metadata["frame_ms"] != int64(12000) {
		t.Errorf("frame: %v", n.Metadata)
	}
	v, err := representation.Emit(d, representation.Spec{
		Kind: representation.Transcript, Content: "cue", Origin: representation.OriginSidecar,
		Anchor: representation.Anchor{}.WithSpan(1, 2),
	})
	if err != nil || v.Metadata["origin"] != "sidecar" {
		t.Errorf("sidecar: %v %v", v.Metadata, err)
	}
	p, err := representation.Emit(d, representation.Spec{
		Kind: representation.ExtractedText, Content: "page", Anchor: representation.Anchor{}.WithPage(3).WithImage(0),
	})
	if err != nil || p.Metadata["page_number"] != 3 || p.Metadata["image_index"] != 0 {
		t.Errorf("page/image: %v %v", p.Metadata, err)
	}
}

func TestEmitDerivesFromOriginal(t *testing.T) {
	d := draftWithID()
	art := storage.GraphNode{
		ID: "obj-1/artifact/0", NodeType: storage.NodeTypeArtifact, Label: "original",
		Metadata: map[string]any{storage.ArtifactMetaBlobKey: "sha256/ab/cd"},
	}
	d.Graph = appendNode(d.Graph, art)
	if got := representation.FindOriginal(d.Graph); got != art.ID {
		t.Fatalf("FindOriginal: %q", got)
	}
	n, err := representation.Emit(d, representation.Spec{Kind: representation.OCR, Content: "text"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Graph.Edges) != 1 {
		t.Fatalf("edges: %+v", d.Graph.Edges)
	}
	e := d.Graph.Edges[0]
	if e.EdgeType != storage.EdgeTypeDerivedFrom || e.FromID != n.ID || e.ToID != art.ID {
		t.Errorf("derives_from must point representation -> artifact: %+v", e)
	}
	// Explicit source that is not in the graph is an error.
	if _, err := representation.Emit(d, representation.Spec{Kind: representation.OCR, Content: "t2", DerivesFrom: "obj-1/artifact/9"}); err == nil {
		t.Error("missing explicit source must error")
	}
}

func TestEmitNoEdgeWithoutOriginal(t *testing.T) {
	d := draftWithID()
	// An artifact node with no blob key is not a stored original.
	d.Graph = appendNode(d.Graph, storage.GraphNode{ID: "obj-1/artifact/0", NodeType: storage.NodeTypeArtifact})
	if got := representation.FindOriginal(d.Graph); got != "" {
		t.Errorf("FindOriginal: %q", got)
	}
	if _, err := representation.Emit(d, representation.Spec{Kind: representation.OCR, Content: "t"}); err != nil {
		t.Fatal(err)
	}
	if len(d.Graph.Edges) != 0 {
		t.Errorf("edges: %+v", d.Graph.Edges)
	}
}

func TestMetaOfSurvivesJSONRoundTrip(t *testing.T) {
	d := draftWithID()
	conf := 0.5
	n, _ := representation.Emit(d, representation.Spec{
		Kind: representation.Transcript, Content: "x", Provider: "p", Model: "m", Confidence: &conf,
		Anchor: representation.Anchor{}.WithSpan(0, 2500).WithSpeaker("S2").WithPage(2).WithFrame(1, 40).WithImage(3),
	})
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	var back storage.GraphNode
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	m, ok := representation.MetaOf(back)
	if !ok {
		t.Fatal("MetaOf: not a representation after round trip")
	}
	requireRoundTripMeta(t, m)
}

// requireRoundTripMeta checks the meta of the node TestMetaOfSurvivesJSONRoundTrip
// emitted.
func requireRoundTripMeta(t *testing.T, m representation.Meta) {
	t.Helper()
	if m.Kind != representation.Transcript || m.Provider != "p" || m.Model != "m" || m.Speaker != "S2" {
		t.Errorf("meta: %+v", m)
	}
	if m.StartMS == nil || *m.StartMS != 0 || m.EndMS == nil || *m.EndMS != 2500 {
		t.Errorf("span: %+v", m)
	}
	if m.PageNumber == nil || *m.PageNumber != 2 || m.FrameIndex == nil || *m.FrameIndex != 1 ||
		m.FrameMS == nil || *m.FrameMS != 40 || m.ImageIndex == nil || *m.ImageIndex != 3 {
		t.Errorf("anchors: %+v", m)
	}
	if m.Confidence == nil || *m.Confidence != 0.5 {
		t.Errorf("confidence: %+v", m)
	}
}

func TestMetaOfRejectsNonRepresentations(t *testing.T) {
	for name, n := range map[string]storage.GraphNode{
		"no metadata":  {NodeType: storage.NodeTypeSection},
		"no key":       {Metadata: map[string]any{"page_number": 1}},
		"unknown kind": {Metadata: map[string]any{"representation": "sound_description"}},
		"wrong type":   {Metadata: map[string]any{"representation": 4}},
	} {
		if _, ok := representation.MetaOf(n); ok {
			t.Errorf("%s: must not be a representation", name)
		}
	}
}

func TestOrderedProjectionOrder(t *testing.T) {
	d := draftWithID()
	// Emission order is not reading order: the summary lands last, frames are
	// interleaved caption/ocr, a plain section predates all of them.
	d.Graph = appendNode(d.Graph, storage.GraphNode{
		ID: "obj-1/section/0", NodeType: storage.NodeTypeSection, Content: "plain", Order: 0,
	})
	emit := func(s representation.Spec) {
		t.Helper()
		if _, err := representation.Emit(d, s); err != nil {
			t.Fatal(err)
		}
	}
	emit(representation.Spec{Kind: representation.Transcript, Content: "t1", Anchor: representation.Anchor{}.WithSpan(0, 1)})
	emit(representation.Spec{Kind: representation.Transcript, Content: "t2", Anchor: representation.Anchor{}.WithSpan(1, 2)})
	emit(representation.Spec{Kind: representation.FrameOCR, Content: "f1 ocr", Anchor: representation.Anchor{}.WithFrame(1, 10)})
	emit(representation.Spec{Kind: representation.FrameCaption, Content: "f1 cap", Anchor: representation.Anchor{}.WithFrame(1, 10)})
	emit(representation.Spec{Kind: representation.FrameCaption, Content: "f0 cap", Anchor: representation.Anchor{}.WithFrame(0, 0)})
	emit(representation.Spec{Kind: representation.Summary, Content: "sum"})

	var got []string
	for _, n := range representation.Ordered(d.Graph) {
		got = append(got, n.Content)
	}
	want := []string{"plain", "sum", "t1", "t2", "f0 cap", "f1 cap", "f1 ocr"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestOrderedSkipsOtherNodeTypes(t *testing.T) {
	g := &storage.ObjectGraph{Nodes: []storage.GraphNode{
		{ID: "o/tag/0", NodeType: storage.NodeTypeTag, Content: "tag"},
		{ID: "o/section/0", NodeType: storage.NodeTypeSection, Content: "s"},
		{ID: "o/entity_mention/0", NodeType: storage.NodeTypeEntityMention, Content: "e"},
	}}
	got := representation.Ordered(g)
	if len(got) != 1 || got[0].Content != "s" {
		t.Errorf("got %+v", got)
	}
	if representation.Ordered(nil) != nil {
		t.Error("nil graph")
	}
}

func TestHas(t *testing.T) {
	d := draftWithID()
	if representation.Has(d.Graph) {
		t.Error("nil graph has no representations")
	}
	_, _ = representation.Emit(d, representation.Spec{Kind: representation.OCR, Content: "x"})
	if !representation.Has(d.Graph) || !representation.Has(d.Graph, representation.OCR) {
		t.Error("OCR node not found")
	}
	if representation.Has(d.Graph, representation.Transcript) {
		t.Error("no transcript emitted")
	}
}

func appendNode(g *storage.ObjectGraph, n storage.GraphNode) *storage.ObjectGraph {
	if g == nil {
		g = &storage.ObjectGraph{}
	}
	g.Nodes = append(g.Nodes, n)
	return g
}

func TestWithFrameIndexOmitsUnknownFrameMS(t *testing.T) {
	d := draftWithID()
	n, err := representation.Emit(d, representation.Spec{
		Kind: representation.FrameOCR, Content: "x", Anchor: representation.Anchor{}.WithFrameIndex(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.Metadata["frame_index"] != 3 {
		t.Errorf("frame_index: %v", n.Metadata)
	}
	if _, ok := n.Metadata["frame_ms"]; ok {
		t.Error("an unknown frame_ms must be absent, not 0")
	}
}

func TestHasContentIgnoresSummary(t *testing.T) {
	d := draftWithID()
	_, _ = representation.Emit(d, representation.Spec{Kind: representation.Summary, Content: "s"})
	if representation.HasContent(d.Graph) {
		t.Error("a summary alone is not content")
	}
	_, _ = representation.Emit(d, representation.Spec{Kind: representation.OCR, Content: "o"})
	if !representation.HasContent(d.Graph) {
		t.Error("ocr is content")
	}
}

func TestEmitIsIdempotent(t *testing.T) {
	d := draftWithID()
	spec := representation.Spec{
		Kind: representation.Transcript, Content: "same", Anchor: representation.Anchor{}.WithSpan(0, 5),
	}
	a, err := representation.Emit(d, spec)
	if err != nil {
		t.Fatal(err)
	}
	b, err := representation.Emit(d, spec)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || len(d.Graph.Nodes) != 1 {
		t.Errorf("re-emit duplicated: %q %q nodes=%d", a.ID, b.ID, len(d.Graph.Nodes))
	}
	// A different anchor, content or kind is a different representation.
	for name, s := range map[string]representation.Spec{
		"anchor":  {Kind: representation.Transcript, Content: "same", Anchor: representation.Anchor{}.WithSpan(5, 9)},
		"content": {Kind: representation.Transcript, Content: "other", Anchor: representation.Anchor{}.WithSpan(0, 5)},
		"kind":    {Kind: representation.ExtractedText, Content: "same", Anchor: representation.Anchor{}.WithSpan(0, 5)},
	} {
		before := len(d.Graph.Nodes)
		if _, err := representation.Emit(d, s); err != nil {
			t.Fatal(err)
		}
		if len(d.Graph.Nodes) != before+1 {
			t.Errorf("%s: want a new node", name)
		}
	}
}

func TestFindOriginalIgnoresLaterArtifacts(t *testing.T) {
	g := &storage.ObjectGraph{Nodes: []storage.GraphNode{
		{ID: "o/artifact/1", NodeType: storage.NodeTypeArtifact, Metadata: map[string]any{storage.ArtifactMetaBlobKey: "frame-blob"}},
	}}
	if got := representation.FindOriginal(g); got != "" {
		t.Errorf("a frame artifact is not the original: %q", got)
	}
	g.Nodes = append(g.Nodes, storage.GraphNode{ID: "o/artifact/0", NodeType: storage.NodeTypeArtifact, Metadata: map[string]any{storage.ArtifactMetaBlobKey: "orig"}})
	if got := representation.FindOriginal(g); got != "o/artifact/0" {
		t.Errorf("got %q", got)
	}
}

func TestEmitSpeakerDistinguishesWhenBothSet(t *testing.T) {
	d := draftWithID()
	base := representation.Spec{Kind: representation.Transcript, Content: "same", Anchor: representation.Anchor{}.WithSpan(0, 5)}
	a := base
	a.Anchor = a.Anchor.WithSpeaker("S1")
	b := base
	b.Anchor = b.Anchor.WithSpeaker("S2")
	if _, err := representation.Emit(d, a); err != nil {
		t.Fatal(err)
	}
	if _, err := representation.Emit(d, b); err != nil {
		t.Fatal(err)
	}
	if len(d.Graph.Nodes) != 2 {
		t.Errorf("two speakers, two nodes: %d", len(d.Graph.Nodes))
	}
	// An unlabelled re-emit still finds the labelled node.
	if _, err := representation.Emit(d, base); err != nil || len(d.Graph.Nodes) != 2 {
		t.Errorf("unlabelled re-emit duplicated: %v %d", err, len(d.Graph.Nodes))
	}
}

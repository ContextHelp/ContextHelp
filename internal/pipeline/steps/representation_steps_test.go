package steps

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type fakeTranscriber struct{}

func (fakeTranscriber) Name() string  { return "whisper" }
func (fakeTranscriber) Model() string { return "large-v3-turbo" }
func (fakeTranscriber) Transcribe(context.Context, string, providers.TranscribeOptions) (*providers.TranscriptResult, error) {
	return &providers.TranscriptResult{
		FullText:   "first words second words",
		Confidence: 0.9,
		Segments: []providers.TranscriptSegment{
			{StartTime: 0, EndTime: 2 * time.Second, Text: "first words", Speaker: "S1"},
			{StartTime: 2 * time.Second, EndTime: 4 * time.Second, Text: "second words"},
		},
	}, nil
}

type fakeOCR struct{}

func (fakeOCR) Name() string { return "tesseract" }
func (fakeOCR) Extract(_ context.Context, _ []byte, _ string) (*providers.OCRResult, error) {
	return &providers.OCRResult{Text: "frame text", Confidence: 0.7}, nil
}

func repNodes(d *storage.KnowledgeObject, k representation.Kind) []storage.GraphNode {
	var out []storage.GraphNode
	if d.Graph == nil {
		return nil
	}
	for _, n := range d.Graph.Nodes {
		if m, ok := representation.MetaOf(n); ok && m.Kind == k {
			out = append(out, n)
		}
	}
	return out
}

func run(t *testing.T, s pipelineStep, d *storage.KnowledgeObject) *storage.KnowledgeObject {
	t.Helper()
	got, err := s.Run(context.Background(), d)
	if err != nil {
		t.Fatalf("%s: %v", s.Name(), err)
	}
	return got
}

type pipelineStep interface {
	Name() string
	Run(context.Context, *storage.KnowledgeObject) (*storage.KnowledgeObject, error)
}

func TestAudioTranscriberEmitsTranscriptNodesWithProvenance(t *testing.T) {
	d := run(t, NewAudioTranscriber(WithTranscriptionProvider(fakeTranscriber{})),
		&storage.KnowledgeObject{ID: "aud-1", Source: "/x.mp3", ContentType: "audio/mpeg"})
	ts := repNodes(d, representation.Transcript)
	if len(ts) != 2 {
		t.Fatalf("transcript nodes: %d", len(ts))
	}
	m, _ := representation.MetaOf(ts[0])
	if m.Provider != "whisper" || m.Model != "large-v3-turbo" || m.Speaker != "S1" ||
		m.StartMS == nil || *m.StartMS != 0 || m.EndMS == nil || *m.EndMS != 2000 {
		t.Errorf("meta: %+v", m)
	}
	if _, has := ts[1].Metadata[representation.KeySpeaker]; has {
		t.Error("an undiarized segment carries no speaker key")
	}
}

func TestAudioRootSummaryNoLongerHoldsTranscript(t *testing.T) {
	// Regression: the root summary held the full transcript and each segment
	// repeated a slice of it, so EmbeddingText held the transcript twice.
	d := run(t, NewAudioTranscriber(WithTranscriptionProvider(fakeTranscriber{})),
		&storage.KnowledgeObject{ID: "aud-1", Source: "/x.mp3", ContentType: "audio/mpeg"})
	for _, n := range d.Graph.Nodes {
		if n.NodeType == storage.NodeTypeSummary {
			t.Errorf("unexpected summary node %+v", n)
		}
	}
	text := projection.ProjectIndex(d).EmbeddingText
	if text != "first words\nsecond words" {
		t.Errorf("EmbeddingText = %q", text)
	}
}

func TestAudioPipelineStepsKeepOneCopyAndUniqueIDs(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "aud-2", Source: "/x.mp3", ContentType: "audio/mpeg"}
	d = run(t, NewAudioTranscriber(WithTranscriptionProvider(fakeTranscriber{})), d)
	d = run(t, NewTimestampAligner(), d)
	d = run(t, NewSectioner(), d)
	seen := map[string]bool{}
	for _, n := range d.Graph.Nodes {
		if seen[n.ID] {
			t.Fatalf("duplicate node id %q", n.ID)
		}
		seen[n.ID] = true
	}
	if got := projection.ProjectIndex(d).EmbeddingText; got != "first words\nsecond words" {
		t.Errorf("EmbeddingText = %q", got)
	}
}

func TestTimestampAlignerEmitsNodesWhenNoneExist(t *testing.T) {
	d := run(t, NewTimestampAligner(), &storage.KnowledgeObject{
		ID: "aud-3",
		Metadata: map[string]any{"_raw_segments": []providers.TranscriptSegment{
			{StartTime: time.Second, EndTime: 2 * time.Second, Text: "hello"},
		}},
	})
	ts := repNodes(d, representation.Transcript)
	if len(ts) != 1 || ts[0].Content != "hello" {
		t.Fatalf("nodes: %+v", ts)
	}
	if got := d.Sections[0].Metadata["start_ms"]; got != int64(1000) {
		t.Errorf("flat section start_ms: %v", got)
	}
}

func TestFrameOCREmitsOneNodePerFrame(t *testing.T) {
	d := run(t, NewFrameOCR(WithFrameOCRProvider(fakeOCR{})), &storage.KnowledgeObject{
		ID:       "vid-1",
		Metadata: map[string]any{"frame_paths": []string{"/f0.png", "/f1.png"}},
	})
	ns := repNodes(d, representation.FrameOCR)
	if len(ns) != 2 {
		t.Fatalf("frame_ocr nodes: %d", len(ns))
	}
	m, _ := representation.MetaOf(ns[1])
	if m.FrameIndex == nil || *m.FrameIndex != 1 || m.Provider != "tesseract" ||
		m.Confidence == nil || *m.Confidence != 0.7 {
		t.Errorf("meta: %+v", m)
	}
	// No producer on this path reports frame timestamps, so frame_ms is
	// absent rather than a misleading 0.
	if _, has := ns[1].Metadata[representation.KeyFrameMS]; has {
		t.Errorf("frame_ms must be absent when unknown: %v", ns[1].Metadata)
	}
	if ns[1].Label != "Frame 2 OCR" {
		t.Errorf("label %q: labels are 1-based", ns[1].Label)
	}
	if d.Metadata["frame_ocr_text"] == nil {
		t.Error("flat frame_ocr_text kept for drafts without graph")
	}
}

func TestVideoTextReachesIndexOnce(t *testing.T) {
	// Regression: frame OCR from timeline_assembler's flat section was
	// never indexed because the graph-first projection ignored it.
	d := &storage.KnowledgeObject{ID: "vid-2", Source: "/v.mp4", ContentType: "video/mp4",
		Metadata: map[string]any{"frame_paths": []string{"/f0.png"}}}
	d = run(t, NewAudioTranscriber(WithTranscriptionProvider(fakeTranscriber{})), d)
	d = run(t, NewTimestampAligner(), d)
	d = run(t, NewFrameOCR(WithFrameOCRProvider(fakeOCR{})), d)
	d = run(t, NewTimelineAssembler(), d)
	got := projection.ProjectIndex(d).EmbeddingText
	if got != "first words\nsecond words\nframe text" {
		t.Errorf("EmbeddingText = %q", got)
	}
}

func TestTimelineAssemblerSkipsFlatFrameOCRWhenNodesExist(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "vid-3", Metadata: map[string]any{
		"frame_paths": []string{"/f0.png"}}}
	d = run(t, NewFrameOCR(WithFrameOCRProvider(fakeOCR{})), d)
	d = run(t, NewTimelineAssembler(), d)
	for _, s := range d.Sections {
		if strings.Contains(s.Content, "frame text") {
			t.Errorf("frame OCR duplicated as flat section: %+v", s)
		}
	}
}

func TestSectionerLeavesRepresentationDraftAlone(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "img-1", RawContent: "one\n\n## H\ntwo"}
	if _, err := representation.Emit(d, representation.Spec{Kind: representation.OCR, Content: "one two"}); err != nil {
		t.Fatal(err)
	}
	before := len(d.Graph.Nodes)
	d = run(t, NewSectioner(), d)
	if len(d.Graph.Nodes) != before {
		t.Errorf("sectioner added nodes to a representation draft: %d -> %d", before, len(d.Graph.Nodes))
	}
}

func TestTextCleanerSkipsSummaryNodeForRepresentationDraft(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "img-2", RawContent: "ocr words"}
	if _, err := representation.Emit(d, representation.Spec{Kind: representation.OCR, Content: "ocr words"}); err != nil {
		t.Fatal(err)
	}
	d = run(t, NewTextCleaner(), d)
	for _, n := range d.Graph.Nodes {
		if n.NodeType == storage.NodeTypeSummary {
			t.Errorf("textcleaner re-emitted the text as a summary: %+v", n)
		}
	}
}

func TestPDFExtractorEmitsExtractedTextWithPage(t *testing.T) {
	d := run(t, NewPDFExtractor(WithDocumentProvider(fakeDoc{})), &storage.KnowledgeObject{
		ID: "pdf-9", Subtype: subtypePDF, Source: "/a.pdf"})
	ns := repNodes(d, representation.ExtractedText)
	if len(ns) != 2 {
		t.Fatalf("nodes: %d", len(ns))
	}
	m, _ := representation.MetaOf(ns[1])
	if m.PageNumber == nil || *m.PageNumber != 2 || m.Provider != "fakedoc" {
		t.Errorf("meta: %+v", m)
	}
	if got := projection.ProjectIndex(d).EmbeddingText; got != "page one\npage two" {
		t.Errorf("EmbeddingText = %q", got)
	}
}

type fakeDoc struct{}

func (fakeDoc) Name() string { return "fakedoc" }
func (fakeDoc) ExtractPDF(context.Context, string) (*providers.DocumentResult, error) {
	return &providers.DocumentResult{
		FullText: "page one page two", PageCount: 2,
		Pages: []providers.DocumentPage{{Number: 1, Content: "page one"}, {Number: 2, Content: "page two"}},
	}, nil
}
func (fakeDoc) ExtractOffice(ctx context.Context, p string) (*providers.DocumentResult, error) {
	return fakeDoc{}.ExtractPDF(ctx, p)
}

func (fakeDoc) ParseMarkdown(context.Context, string) (*providers.DocumentResult, error) {
	return nil, nil
}

type fakeDiarizer struct{}

func (fakeDiarizer) Name() string { return "fakepyannote" }
func (fakeDiarizer) Diarize(_ context.Context, _ string, segs []providers.TranscriptSegment) (*providers.DiarizedResult, error) {
	out := append([]providers.TranscriptSegment(nil), segs...)
	for i := range out {
		out[i].Speaker = []string{"SPEAKER_00", "SPEAKER_01"}[i%2]
	}
	return &providers.DiarizedResult{Segments: out, SpeakerCount: 2}, nil
}

func TestDiarizedSpeakerReachesTranscriptNodes(t *testing.T) {
	// Regression: the transcriber had already emitted the nodes, so the
	// aligner skipped emission and the diarized speaker never landed on them.
	d := &storage.KnowledgeObject{ID: "aud-4", Source: "/x.mp3", ContentType: "audio/mpeg"}
	d = run(t, NewAudioTranscriber(WithTranscriptionProvider(fakeTranscriber{})), d)
	d = run(t, NewSpeakerDiarizer(true, WithDiarizationProvider(fakeDiarizer{})), d)
	d = run(t, NewTimestampAligner(), d)
	ts := repNodes(d, representation.Transcript)
	if len(ts) != 2 {
		t.Fatalf("transcript nodes: %d", len(ts))
	}
	if ts[0].Metadata["speaker"] != "SPEAKER_00" || ts[1].Metadata["speaker"] != "SPEAKER_01" {
		t.Errorf("speakers: %v %v", ts[0].Metadata["speaker"], ts[1].Metadata["speaker"])
	}
}

type badAnchorTranscriber struct{ fakeTranscriber }

func (badAnchorTranscriber) Transcribe(context.Context, string, providers.TranscribeOptions) (*providers.TranscriptResult, error) {
	return &providers.TranscriptResult{Segments: []providers.TranscriptSegment{
		{StartTime: -time.Second, EndTime: 2 * time.Second, Text: "negative start"},
		{StartTime: 5 * time.Second, EndTime: 3 * time.Second, Text: "end before start"},
	}}, nil
}

func TestBadSegmentAnchorsAreClampedNotFatal(t *testing.T) {
	d := run(t, NewAudioTranscriber(WithTranscriptionProvider(badAnchorTranscriber{})),
		&storage.KnowledgeObject{ID: "aud-5", Source: "/x.mp3", ContentType: "audio/mpeg"})
	ts := repNodes(d, representation.Transcript)
	if len(ts) != 2 {
		t.Fatalf("both segments must be kept: %d", len(ts))
	}
	m0, _ := representation.MetaOf(ts[0])
	m1, _ := representation.MetaOf(ts[1])
	if *m0.StartMS != 0 || *m0.EndMS != 2000 {
		t.Errorf("negative start not clamped to 0: %d-%d", *m0.StartMS, *m0.EndMS)
	}
	if *m1.StartMS != 5000 || *m1.EndMS != 5000 {
		t.Errorf("end not raised to start: %d-%d", *m1.StartMS, *m1.EndMS)
	}
}

func TestEmitLenientRecordsRepresentationError(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "x-1"}
	emitLenient(d, representation.Spec{Kind: representation.OCR, Content: "ok"})
	emitLenient(d, representation.Spec{Kind: representation.OCR, Content: "bad", Confidence: ptr(2.0)})
	emitLenient(d, representation.Spec{Kind: representation.OCR, Content: "bad2", Confidence: ptr(3.0)})
	if len(repNodes(d, representation.OCR)) != 1 {
		t.Error("the bad specs must be skipped, the good one kept")
	}
	msg, _ := d.Metadata["ocr_error"].(string)
	if !strings.Contains(msg, "confidence") || !strings.Contains(msg, ";") {
		t.Errorf("ocr_error = %q: want both skips recorded", msg)
	}
}

func ptr(f float64) *float64 { return &f }

type messyDoc struct{ fakeDoc }

func (messyDoc) ExtractPDF(context.Context, string) (*providers.DocumentResult, error) {
	return &providers.DocumentResult{Pages: []providers.DocumentPage{
		{Number: 1, Content: "  spaced   out\r\n\r\n\r\n\r\ntext  "},
		{Number: -2, Content: "neg page"},
	}}, nil
}
func (m messyDoc) ExtractOffice(c context.Context, p string) (*providers.DocumentResult, error) {
	return m.ExtractPDF(c, p)
}

func TestExtractorsCleanPageTextAndClampPages(t *testing.T) {
	for name, step := range map[string]pipelineStep{
		"pdf":    NewPDFExtractor(WithDocumentProvider(messyDoc{})),
		"office": NewOfficeExtractor(WithOfficeDocumentProvider(messyDoc{})),
	} {
		sub := map[string]string{"pdf": subtypePDF, "office": "office"}[name]
		d := run(t, step, &storage.KnowledgeObject{ID: "doc-" + name, Subtype: sub, Source: "/a"})
		ns := repNodes(d, representation.ExtractedText)
		if len(ns) != 2 {
			t.Fatalf("%s: nodes %d", name, len(ns))
		}
		if ns[0].Content != "spaced out\n\ntext" {
			t.Errorf("%s: page text not cleaned: %q", name, ns[0].Content)
		}
		if m, _ := representation.MetaOf(ns[1]); *m.PageNumber != 0 {
			t.Errorf("%s: negative page not clamped: %d", name, *m.PageNumber)
		}
	}
}

func TestSectionerAndCleanerStillRunOnSummaryOnlyDraft(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "s-1", RawContent: "alpha\n\n## H\nbeta"}
	if _, err := representation.Emit(d, representation.Spec{Kind: representation.Summary, Content: "sum"}); err != nil {
		t.Fatal(err)
	}
	before := len(d.Graph.Nodes)
	d = run(t, NewSectioner(), d)
	if len(d.Graph.Nodes) <= before {
		t.Error("a summary is not content: sectioner must still section RawContent")
	}
}

func TestTextCleanerStillRewritesRawContentForRepresentationDraft(t *testing.T) {
	d := &storage.KnowledgeObject{ID: "s-2", RawContent: "a   b"}
	if _, err := representation.Emit(d, representation.Spec{Kind: representation.OCR, Content: "a b"}); err != nil {
		t.Fatal(err)
	}
	d = run(t, NewTextCleaner(), d)
	if d.RawContent != "a b" {
		t.Errorf("RawContent = %q", d.RawContent)
	}
}

func TestAlignerClampsFlatSectionSpans(t *testing.T) {
	d := run(t, NewTimestampAligner(), &storage.KnowledgeObject{
		Metadata: map[string]any{"_raw_segments": []providers.TranscriptSegment{
			{StartTime: -time.Second, EndTime: -2 * time.Second, Text: "neg"},
			{StartTime: 5 * time.Second, EndTime: 3 * time.Second, Text: "rev"},
		}},
	})
	if m := d.Sections[0].Metadata; m["start_ms"] != int64(0) || m["end_ms"] != int64(0) {
		t.Errorf("negative span: %v", m)
	}
	if m := d.Sections[1].Metadata; m["start_ms"] != int64(5000) || m["end_ms"] != int64(5000) {
		t.Errorf("reversed span: %v", m)
	}
}

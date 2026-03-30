package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func TestOCRExtractorSetsText(t *testing.T) {
	step := NewOCRExtractor()
	draft := &storage.KnowledgeObject{
		RawContent:  "fake image bytes",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Sections) == 0 {
		t.Fatal("no sections created")
	}
	if got.Sections[0].Title != "OCR Text" {
		t.Errorf("section title: %q", got.Sections[0].Title)
	}
	if got.Metadata["ocr_provider"] != "stub" {
		t.Errorf("ocr_provider: %v", got.Metadata["ocr_provider"])
	}
}

func TestOCRExtractorLowConfidence(t *testing.T) {
	lowConf := &lowConfOCR{}
	step := NewOCRExtractor(WithOCRProvider(lowConf), WithOCRConfidenceThreshold(0.80))
	draft := &storage.KnowledgeObject{
		RawContent:  "fake",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Metadata["ocr_low_confidence"] != true {
		t.Error("expected ocr_low_confidence = true")
	}
	hasReview := false
	for _, tag := range got.Tags {
		if tag.Label == "needs-review" {
			hasReview = true
		}
	}
	if !hasReview {
		t.Error("expected needs-review tag")
	}
}

func TestOCRExtractorEmitsGraphNodes(t *testing.T) {
	step := NewOCRExtractor()
	draft := &storage.KnowledgeObject{
		ID:          "obj-ocr-001",
		RawContent:  "fake image bytes",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph == nil {
		t.Fatal("graph is nil; expected nodes")
	}
	artifactID := pluginapi.NewNodeID("obj-ocr-001", pluginapi.NodeTypeArtifact, 0)
	if got.Graph.FindNode(artifactID) == nil {
		t.Errorf("artifact node %q not found", artifactID)
	}
	var sectionNode *pluginapi.GraphNode
	for i := range got.Graph.Nodes {
		if got.Graph.Nodes[i].NodeType == pluginapi.NodeTypeSection {
			sectionNode = &got.Graph.Nodes[i]
			break
		}
	}
	if sectionNode == nil {
		t.Error("expected a section node for OCR text")
	} else if sectionNode.Label != "OCR Text" {
		t.Errorf("section label: got %q", sectionNode.Label)
	}
	if len(got.Graph.Edges) == 0 {
		t.Error("expected edge from artifact to section")
	}
}

func TestOCRExtractorNoGraphWithoutID(t *testing.T) {
	step := NewOCRExtractor()
	draft := &storage.KnowledgeObject{
		RawContent:  "fake image bytes",
		ContentType: "image/png",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.Graph != nil && len(got.Graph.Nodes) > 0 {
		t.Error("expected no graph nodes when ID is empty")
	}
}

type lowConfOCR struct{}

func (p *lowConfOCR) Name() string { return "low" }
func (p *lowConfOCR) Extract(_ context.Context, _ []byte, _ string) (*providers.OCRResult, error) {
	return &providers.OCRResult{Text: "blurry text", Confidence: 0.30}, nil
}

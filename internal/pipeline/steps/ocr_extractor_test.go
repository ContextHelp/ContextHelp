package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
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

type lowConfOCR struct{}

func (p *lowConfOCR) Name() string { return "low" }
func (p *lowConfOCR) Extract(_ context.Context, _ []byte, _ string) (*providers.OCRResult, error) {
	return &providers.OCRResult{Text: "blurry text", Confidence: 0.30}, nil
}

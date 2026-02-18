package providers

import "context"

// OCRResult holds the output of an OCR extraction.
type OCRResult struct {
	Text       string  // Extracted text
	Confidence float64 // 0.0 to 1.0
}

// OCRProvider extracts text from images.
type OCRProvider interface {
	Name() string
	Extract(ctx context.Context, imageData []byte, contentType string) (*OCRResult, error)
}

// StubOCRProvider returns placeholder text for testing and development.
type StubOCRProvider struct{}

func NewStubOCRProvider() *StubOCRProvider { return &StubOCRProvider{} }

func (p *StubOCRProvider) Name() string { return "stub" }

func (p *StubOCRProvider) Extract(_ context.Context, imageData []byte, contentType string) (*OCRResult, error) {
	return &OCRResult{
		Text:       "[OCR text extracted from image]",
		Confidence: 0.95,
	}, nil
}

package providers

import "context"

// VisionResult holds the output of a vision analysis.
type VisionResult struct {
	Description string   // Scene description
	Labels      []string // Detected labels/objects
	Confidence  float64
}

// VisionProvider analyzes image content beyond OCR.
type VisionProvider interface {
	Name() string
	Analyze(ctx context.Context, imageData []byte, contentType string) (*VisionResult, error)
}

// StubVisionProvider returns placeholder analysis.
type StubVisionProvider struct{}

func NewStubVisionProvider() *StubVisionProvider { return &StubVisionProvider{} }

func (p *StubVisionProvider) Name() string { return "stub" }

func (p *StubVisionProvider) Analyze(_ context.Context, _ []byte, _ string) (*VisionResult, error) {
	return &VisionResult{
		Description: "[Vision analysis of image content]",
		Labels:      []string{"image"},
		Confidence:  0.90,
	}, nil
}

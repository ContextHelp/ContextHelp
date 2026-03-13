package vision

import (
	"context"
	"strings"
)

// Result holds the output of a vision analysis.
type Result struct {
	Description string   // Scene description
	Labels      []string // Detected labels/objects
	Confidence  float64
}

// Provider analyzes image content beyond OCR.
type Provider interface {
	Name() string
	Analyze(ctx context.Context, imageData []byte, contentType string) (*Result, error)
}

// StubProvider returns placeholder analysis.
type StubProvider struct{}

func NewStubProvider() *StubProvider { return &StubProvider{} }

func (p *StubProvider) Name() string { return "stub" }

func (p *StubProvider) Analyze(_ context.Context, _ []byte, _ string) (*Result, error) {
	return &Result{
		Description: "[Vision analysis of image content]",
		Labels:      []string{"image"},
		Confidence:  0.90,
	}, nil
}

// prompt is the shared prompt used by all vision providers.
const prompt = "Describe this image in detail. List the main objects, text, colors, and overall scene. Then list detected labels as a comma-separated list after 'Labels:'."

// parseResponse splits an LLM response into description and labels.
func parseResponse(response string) (string, []string) {
	lower := strings.ToLower(response)
	idx := strings.Index(lower, "labels:")
	if idx < 0 {
		return strings.TrimSpace(response), []string{}
	}

	description := strings.TrimSpace(response[:idx])
	labelsStr := strings.TrimSpace(response[idx+7:])

	var labels []string
	for _, l := range strings.Split(labelsStr, ",") {
		l = strings.TrimSpace(l)
		if l != "" {
			labels = append(labels, l)
		}
	}

	return description, labels
}

package providers

import (
	"testing"
)

func TestParseVisionResponse(t *testing.T) {
	response := `This image shows a cat sitting on a desk next to a laptop. The cat is orange and white.

Labels: cat, desk, laptop, orange, white`

	desc, labels := parseVisionResponse(response)
	if desc == "" {
		t.Error("description is empty")
	}
	if len(labels) != 5 {
		t.Errorf("labels: got %d, want 5", len(labels))
	}
	if labels[0] != "cat" {
		t.Errorf("labels[0]: got %q", labels[0])
	}
}

func TestParseVisionResponseNoLabels(t *testing.T) {
	response := "A scenic mountain landscape with snow-capped peaks."
	desc, labels := parseVisionResponse(response)
	if desc != response {
		t.Errorf("description: got %q", desc)
	}
	if len(labels) != 0 {
		t.Errorf("labels: got %d, want 0", len(labels))
	}
}

func TestOllamaVisionProviderName(t *testing.T) {
	p := NewOllamaVisionProvider("http://localhost:11434", "llava")
	if p.Name() != "ollama" {
		t.Errorf("Name: got %q", p.Name())
	}
}

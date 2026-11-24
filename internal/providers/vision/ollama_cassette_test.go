package vision_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
)

// The cassette was recorded against a real local Ollama with the model
// pulled. Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run OllamaVisionCassette ./internal/providers/vision/
//
// See docs/testing-media-cassettes.md.
func TestOllamaVisionCassette_DescribesFixtureImage(t *testing.T) {
	client, calls := providertest.OllamaClient(t, "testdata/cassettes/ollama-vision")
	p := vision.NewOllamaProvider("http://127.0.0.1:11434", "qwen2.5vl:3b").WithHTTPClient(client)

	res, err := p.Analyze(context.Background(), providertest.MediaBytes(t, providertest.MediaImage), "image/png")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if strings.TrimSpace(res.Description) == "" {
		t.Fatal("empty description")
	}
	if got := calls.URLs(); len(got) != 1 || !strings.HasSuffix(got[0], "/api/generate") {
		t.Errorf("calls = %v, want one /api/generate", got)
	}
	if !strings.Contains(res.Description, "4821") {
		t.Errorf("description %q does not mention the fixture's 4821", res.Description)
	}
	// The original bytes reach the model, not a placeholder.
	if !strings.Contains(calls.Bodies()[0], `"images":["iVBOR`) {
		t.Errorf("request body lacks base64 PNG image bytes")
	}
}

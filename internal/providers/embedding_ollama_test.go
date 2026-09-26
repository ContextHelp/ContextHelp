package providers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
)

// Cassettes under testdata/cassettes/ollama-embed were recorded against a
// real local Ollama with snowflake-arctic-embed2 pulled. Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run TestOllamaEmbedding ./internal/providers/
const ollamaCassettes = "testdata/cassettes/ollama-embed"

func TestOllamaEmbedding_EmbedRecorded(t *testing.T) {
	client, calls := providertest.OllamaClient(t, ollamaCassettes)
	p := providers.NewOllamaEmbeddingProvider("http://127.0.0.1:11434", "snowflake-arctic-embed2",
		providers.WithOllamaEmbeddingHTTPClient(client))

	vec, err := p.Embed(context.Background(), "ctxt embedding provider resolution")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 1024 {
		t.Fatalf("snowflake-arctic-embed2 dimension = %d, want 1024", len(vec))
	}
	if got := calls.URLs(); len(got) != 1 || got[0] != "http://127.0.0.1:11434/api/embeddings" {
		t.Fatalf("request URLs = %v", got)
	}
}

func TestOllamaEmbedding_ModelNotPulledRecorded(t *testing.T) {
	client, _ := providertest.OllamaClient(t, ollamaCassettes)
	p := providers.NewOllamaEmbeddingProvider("http://127.0.0.1:11434", "ctxt-missing-embed-model",
		providers.WithOllamaEmbeddingHTTPClient(client))

	_, err := p.Embed(context.Background(), "ctxt embedding provider resolution")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Embed with an unpulled model: err = %v, want Ollama's not-found error", err)
	}
}

func TestOllamaEmbedding_TrailingSlashEndpoint(t *testing.T) {
	client, calls := providertest.OllamaClient(t, ollamaCassettes)
	p := providers.NewOllamaEmbeddingProvider("http://127.0.0.1:11434/", "snowflake-arctic-embed2",
		providers.WithOllamaEmbeddingHTTPClient(client))

	if _, err := p.Embed(context.Background(), "ctxt embedding provider resolution"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got := calls.URLs(); len(got) != 1 || got[0] != "http://127.0.0.1:11434/api/embeddings" {
		t.Fatalf("request URLs = %v, want a single slash before api/embeddings", got)
	}
}

func TestOllamaEmbedding_Dimensions(t *testing.T) {
	if got := providers.NewOllamaEmbeddingProvider("http://x", "m").Dimensions(); got != 0 {
		t.Errorf("Dimensions() without a configured dimension = %d, want 0 (unknown)", got)
	}
	p := providers.NewOllamaEmbeddingProvider("http://x", "m", providers.WithOllamaEmbeddingDimension(1024))
	if got := p.Dimensions(); got != 1024 {
		t.Errorf("Dimensions() = %d, want 1024", got)
	}
}

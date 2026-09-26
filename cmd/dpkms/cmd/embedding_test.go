package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
)

// serve reports the provider it resolved at startup, with sources, so an
// operator can see a one-run env override took effect.
func TestServeReportsResolvedEmbeddingProvider(t *testing.T) {
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{Providers: config.ProvidersConfig{
		Embedding: config.EmbeddingProviderConfig{Backend: "ollama", Model: "cfg-model"},
	}}
	t.Setenv(embeddings.EnvEndpoint, "http://127.0.0.1:11555")

	var buf bytes.Buffer
	reportEmbeddingProvider(&buf)
	out := buf.String()
	for _, want := range []string{"backend=ollama (config)", "model=cfg-model (config)", "endpoint=http://127.0.0.1:11555 (env)"} {
		if !strings.Contains(out, want) {
			t.Errorf("startup line missing %q: %s", want, out)
		}
	}

	t.Setenv(embeddings.EnvProvider, "openai")
	buf.Reset()
	reportEmbeddingProvider(&buf)
	if !strings.Contains(buf.String(), "warning") || !strings.Contains(buf.String(), "not supported") {
		t.Errorf("unsupported backend should warn, got: %s", buf.String())
	}
}

func TestBackupRecordsResolvedEmbedding(t *testing.T) {
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &config.Config{Providers: config.ProvidersConfig{
		Embedding: config.EmbeddingProviderConfig{Model: "cfg-model", Dimension: 1024},
	}}
	t.Setenv(embeddings.EnvModel, "env-model")

	info := backupEmbeddingInfo()
	if info.Backend != embeddings.DefaultBackend || info.Model != "env-model" || info.Dimension != 1024 {
		t.Fatalf("EmbeddingInfo = %+v", info)
	}
}

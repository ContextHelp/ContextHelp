package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
)

func TestDevReindexVectors_EmbeddingFlagsReachTheResolver(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.run("dev", "reindex-vectors", "--embedding-endpoint", "127.0.0.1:11555")
	if err == nil || !strings.Contains(err.Error(), "--embedding-endpoint") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	out, err = db.run("dev", "reindex-vectors", "--embedding-provider", "stub")
	if err != nil || !strings.Contains(out, "indexed=0") {
		t.Fatalf("stub provider: err = %v\n%s", err, out)
	}
}

func TestEmbeddingFlags_DpkmsSignatureClean(t *testing.T) {
	resetAllFlags(rootCmd)
	target, _, err := rootCmd.Find([]string{"dev", "reindex-vectors"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{embeddings.FlagProvider, embeddings.FlagModel, embeddings.FlagEndpoint} {
		if target.LocalFlags().Lookup(f) == nil {
			t.Errorf("dev reindex-vectors: missing local --%s", f)
		}
	}
	for _, v := range root.ValidateSignature().Violations {
		if strings.Contains(v.Path, "reindex-vectors") {
			t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
		}
	}
}

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

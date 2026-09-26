package storage

import (
	"regexp"
	"testing"
)

func TestValidateEmbeddingModelID(t *testing.T) {
	valid := []string{
		"openai-text-embedding-3-small@2025-01-15",
		"ollama-snowflake-arctic-embed2@2026-09-26",
		"nomic-embed-text:v1.5@2026-09-26",
		"hf.co/org/model+q8@2026-09-26",
		"a",
	}
	for _, id := range valid {
		if err := ValidateEmbeddingModelID(id); err != nil {
			t.Errorf("ValidateEmbeddingModelID(%q) = %v, want nil", id, err)
		}
	}
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	invalid := []string{
		"",
		"-leading-dash",
		"has space@2026",
		"quote'@2026",
		`dq"@2026`,
		"semi;colon",
		"new\nline",
		string(long),
	}
	for _, id := range invalid {
		if err := ValidateEmbeddingModelID(id); err == nil {
			t.Errorf("ValidateEmbeddingModelID(%q) = nil, want error", id)
		}
	}
}

func TestEmbeddingIndexName(t *testing.T) {
	ident := regexp.MustCompile(`^emb_[0-9a-f]{16}$`)
	a := EmbeddingIndexName("ollama-nomic-embed-text@2026-09-26")
	b := EmbeddingIndexName("ollama-snowflake-arctic-embed2@2026-09-26")
	if !ident.MatchString(a) || !ident.MatchString(b) {
		t.Fatalf("names not bare identifiers: %q %q", a, b)
	}
	if a == b {
		t.Fatalf("distinct model_ids share index name %q", a)
	}
	if again := EmbeddingIndexName("ollama-nomic-embed-text@2026-09-26"); again != a {
		t.Fatalf("not deterministic: %q then %q", a, again)
	}
	// Pinned: the name is persisted DDL; changing the derivation orphans
	// every existing per-model index.
	if got, want := EmbeddingIndexName("m@1"), "emb_4de95e4949e4e6b6"; got != want {
		t.Fatalf("EmbeddingIndexName(m@1) = %q, want %q", got, want)
	}
}

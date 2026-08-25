package indexsig

import (
	"strings"
	"testing"
)

// TestHashFTSInputs_ByteStableAcrossPackageMove pins the SQLite FTS hash
// formula to the exact value the pre-package sqlite driver computed. Existing
// installations carry stored hashes; a formula drift here would flag every
// healthy index as mismatched on upgrade and trigger spurious rebuilds.
// Golden value captured from the original sqlite implementation.
func TestHashFTSInputs_ByteStableAcrossPackageMove(t *testing.T) {
	const golden = "abd3a3ef51eb220ec218ff07b660907e6345e6c0fbdf1e405e38d4013a9a1299"
	got := HashFTSInputs("fts5-default", "v1", "CREATE VIRTUAL TABLE objects_fts USING fts5(x)")
	if got != golden {
		t.Fatalf("SQLite FTS hash formula drifted:\n  got  %s\n  want %s", got, golden)
	}
}

func TestHashFTSInputs_ChangesWhenInputsChange(t *testing.T) {
	base := HashFTSInputs("fts5-default", "v1", "ddl")
	if HashFTSInputs("icu", "v1", "ddl") == base {
		t.Error("tokenizer change must change hash")
	}
	if HashFTSInputs("fts5-default", "v2", "ddl") == base {
		t.Error("projection change must change hash")
	}
	if HashFTSInputs("fts5-default", "v1", "other") == base {
		t.Error("ddl change must change hash")
	}
}

// TestComputeEmbedding_ExtendedInputs pins the extended embedding signature:
// the index description (method, ops class, build params) participates in the
// hash, so a rebuilt index with different tuning no longer verifies against
// the old stamp — and the summary names every input for operators.
func TestComputeEmbedding_ExtendedInputs(t *testing.T) {
	base, summary := ComputeEmbedding("m1", "openai", 1536, "hnsw", "vector_cosine_ops", "m='16', ef_construction='64'")
	for _, part := range []string{
		"model_id=m1", "provider=openai", "dimension=1536",
		"method=hnsw", "ops=vector_cosine_ops", "params=m='16', ef_construction='64'",
	} {
		if !strings.Contains(summary, part) {
			t.Errorf("summary missing %q: %s", part, summary)
		}
	}

	if h, _ := ComputeEmbedding("m1", "openai", 1536, "ivfflat", "vector_cosine_ops", "m='16', ef_construction='64'"); h == base {
		t.Error("method change must change hash")
	}
	if h, _ := ComputeEmbedding("m1", "openai", 1536, "hnsw", "vector_l2_ops", "m='16', ef_construction='64'"); h == base {
		t.Error("ops class change must change hash")
	}
	if h, _ := ComputeEmbedding("m1", "openai", 1536, "hnsw", "vector_cosine_ops", ""); h == base {
		t.Error("build params change must change hash")
	}
	if h, _ := ComputeEmbedding("m1", "openai", 768, "hnsw", "vector_cosine_ops", "m='16', ef_construction='64'"); h == base {
		t.Error("dimension change must change hash")
	}
}

func TestComputeEmbedding_Deterministic(t *testing.T) {
	h1, s1 := ComputeEmbedding("m", "p", 4, "hnsw", "vector_cosine_ops", "")
	h2, s2 := ComputeEmbedding("m", "p", 4, "hnsw", "vector_cosine_ops", "")
	if h1 != h2 || s1 != s2 {
		t.Errorf("not deterministic: %s/%s vs %s/%s", h1, s1, h2, s2)
	}
}

func TestEmbeddingSignatureID(t *testing.T) {
	if got := EmbeddingSignatureID("m1"); got != "embeddings_m1" {
		t.Errorf("EmbeddingSignatureID: got %q", got)
	}
}

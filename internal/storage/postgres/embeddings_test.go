package postgres

import (
	"math"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EmbeddingSearchQueryForTest exposes the per-model KNN query so the
// integration tests can EXPLAIN exactly what Search runs.
func EmbeddingSearchQueryForTest(modelID string, dim int) string {
	return pgIndexFor(modelID).render(pgSearchTmpl, modelID, dim)
}

// EmbeddingIndexNamesForTest exposes the per-model index and CHECK names.
func EmbeddingIndexNamesForTest(modelID string) (index, check string) {
	ix := pgIndexFor(modelID)
	return ix.index, ix.check
}

// TestPgVectorLiteral_RoundTrip pins float32 round-trip precision through
// pgvector's text form.
func TestPgVectorLiteral_RoundTrip(t *testing.T) {
	in := []float32{1, -0.5, 1e-7, 3.4028235e38, float32(math.Pi), 0}
	lit := pgVectorLiteral(in)
	if !strings.HasPrefix(lit, "[") || !strings.HasSuffix(lit, "]") {
		t.Fatalf("literal %q", lit)
	}
	out, err := parsePgVectorLiteral(lit)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("round trip %v -> %v", in, out)
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("component %d: %v -> %v", i, in[i], out[i])
		}
	}
	if _, err := parsePgVectorLiteral("[1,x]"); err == nil {
		t.Error("malformed literal parsed")
	}
}

// TestPgEmbeddingTemplates_LiteralAndCast pins the per-model DDL and query
// shape: the model_id is a literal (so the planner can match the partial
// index under any plan), and the cast carries the registry dimension.
func TestPgEmbeddingTemplates_LiteralAndCast(t *testing.T) {
	const id = "ollama-bge-m3@2026-09-26"
	ix := pgIndexFor(id)
	if ix.index != "idx_"+storage.EmbeddingIndexName(id) || ix.check != "embeddings_dim_"+storage.EmbeddingIndexName(id) {
		t.Fatalf("names: %+v", ix)
	}
	q := ix.render(pgSearchTmpl, id, 1024)
	for _, part := range []string{
		"WHERE model_id = '" + id + "'",
		"ORDER BY vector::vector(1024) <=> $1::vector(1024)",
		"LIMIT $2",
	} {
		if !strings.Contains(q, part) {
			t.Errorf("search query missing %q:\n%s", part, q)
		}
	}
	ddl := ix.render(pgIndexTmpl, id, 1024)
	for _, part := range []string{
		"USING hnsw ((vector::vector(1024)) vector_cosine_ops)",
		"WHERE model_id = '" + id + "'",
	} {
		if !strings.Contains(ddl, part) {
			t.Errorf("index DDL missing %q:\n%s", part, ddl)
		}
	}
	if chk := ix.render(pgCheckTmpl, id, 1024); !strings.Contains(chk, "CHECK (model_id <> '"+id+"' OR vector_dims(vector) = 1024)") {
		t.Errorf("check DDL: %s", chk)
	}
}

func TestPgCollapseChunks(t *testing.T) {
	hits := []storage.EmbeddingHit{
		{ObjectID: "a", ChunkIdx: 2, Distance: 0.1},
		{ObjectID: "a", ChunkIdx: 0, Distance: 0.2},
		{ObjectID: "b", ChunkIdx: 1, Distance: 0.3},
		{ObjectID: "c", ChunkIdx: 0, Distance: 0.4},
	}
	got := collapseChunks(hits, 2)
	if len(got) != 2 || got[0].ObjectID != "a" || got[0].ChunkIdx != 2 || got[1].ObjectID != "b" {
		t.Errorf("collapse = %+v", got)
	}
}

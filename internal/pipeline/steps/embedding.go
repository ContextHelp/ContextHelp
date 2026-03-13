package steps

import (
	"context"
	"encoding/binary"
	"math"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EmbeddingGenerator calls an EmbeddingProvider and stores the result on the draft.
type EmbeddingGenerator struct {
	pipeline.BaseContract
	provider providers.EmbeddingProvider
}

// NewEmbeddingGenerator creates an EmbeddingGenerator using the given provider.
// Passing nil uses a stub (no-op) provider.
func NewEmbeddingGenerator(provider providers.EmbeddingProvider) *EmbeddingGenerator {
	if provider == nil {
		provider = providers.NewStubEmbeddingProvider()
	}
	return &EmbeddingGenerator{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Embeddings", "VectorIndexed"},
		}),
		provider: provider,
	}
}

func (s *EmbeddingGenerator) Name() string { return "embedding_generator" }

// Run embeds the draft's raw content and sets Embeddings + VectorIndexed.
// Errors from the embedding provider are non-fatal — the draft is returned
// with VectorIndexed=false so the pipeline can continue.
func (s *EmbeddingGenerator) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	text := draft.RawContent
	if text == "" && len(draft.Summaries) > 0 {
		text = draft.Summaries[0]
	}
	if text == "" {
		draft.VectorIndexed = false
		return draft, nil
	}

	vec, err := s.provider.Embed(ctx, text)
	if err != nil || len(vec) == 0 {
		// Non-fatal: skip vector indexing for this object.
		draft.VectorIndexed = false
		return draft, nil
	}

	draft.Embeddings = vec
	draft.VectorIndexed = true
	return draft, nil
}

// Float32SliceToBytes encodes a []float32 as little-endian bytes for SQLite BLOB storage.
func Float32SliceToBytes(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// BytesToFloat32Slice decodes little-endian bytes from a SQLite BLOB into []float32.
func BytesToFloat32Slice(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 if the vectors are of different lengths or if either has zero norm.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

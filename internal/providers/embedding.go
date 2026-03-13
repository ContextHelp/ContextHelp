package providers

import "context"

// EmbeddingProvider generates vector embeddings for text.
type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
	Name() string
}

// NewStubEmbeddingProvider returns a stub embedding provider.
func NewStubEmbeddingProvider() EmbeddingProvider { return &stubEmbeddingProvider{} }

type stubEmbeddingProvider struct{}

func (s *stubEmbeddingProvider) Name() string    { return "stub" }
func (s *stubEmbeddingProvider) Dimensions() int { return 0 }
func (s *stubEmbeddingProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}

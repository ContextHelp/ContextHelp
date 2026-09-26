package cmd

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// newEmbeddingResolver builds the embedding provider resolver from the
// loaded config and the raw -c overrides. Every ctxt command that embeds
// goes through it.
func newEmbeddingResolver() *embeddings.Resolver {
	r := &embeddings.Resolver{}
	if cfg != nil {
		r.Config = cfg.Providers.Embedding
	}
	// A -c parse error already failed the run in initConfig.
	if _, overrides, err := root.ConfigArgs(); err == nil {
		r.ConfigOverrides = overrides
	}
	return r
}

// resolveEmbeddingProvider resolves the default (untargeted) embedding
// provider with the command's flag overrides applied.
func resolveEmbeddingProvider(ctx context.Context, o embeddings.Overrides) (providers.EmbeddingProvider, error) {
	res, err := newEmbeddingResolver().Resolve(ctx, embeddings.Request{Overrides: o})
	if err != nil {
		return nil, err
	}
	return res.Provider()
}

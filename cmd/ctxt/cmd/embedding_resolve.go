package cmd

import (
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
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

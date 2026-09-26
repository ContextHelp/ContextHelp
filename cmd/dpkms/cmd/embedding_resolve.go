package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// newEmbeddingResolver builds the embedding provider resolver from the
// loaded config and the raw -c overrides. Every dpkms path that embeds, or
// reports the embedding provider, goes through it.
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

// reportEmbeddingProvider writes serve's startup line: the resolved
// embedding provider with each setting's source. A resolution that cannot
// build a provider is a warning, not a startup failure.
func reportEmbeddingProvider(w io.Writer) {
	res, err := newEmbeddingResolver().Resolve(context.Background(), embeddings.Request{})
	if err != nil {
		fmt.Fprintf(w, "warning: embedding provider: %v\n", err)
		return
	}
	parts := make([]string, 0, len(embeddings.Fields))
	for _, e := range res.Explain() {
		if e.Value == "" || (e.Field == embeddings.FieldDimension && res.Dimension == 0) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s (%s)", e.Field, e.Value, e.Layer))
	}
	fmt.Fprintf(w, "Embedding provider: %s\n", strings.Join(parts, " "))
	if _, err := res.Provider(); err != nil {
		fmt.Fprintf(w, "warning: embedding provider: %v\n", err)
	}
}

// backupEmbeddingInfo records the resolved embedding provider in a backup
// manifest. A resolution error records nothing rather than failing the
// backup.
func backupEmbeddingInfo() service.EmbeddingInfo {
	res, err := newEmbeddingResolver().Resolve(context.Background(), embeddings.Request{})
	if err != nil {
		return service.EmbeddingInfo{}
	}
	return service.EmbeddingInfo{Backend: res.Backend, Model: res.Model, Dimension: res.Dimension}
}

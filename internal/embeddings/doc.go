// Package embeddings resolves the embedding provider. It is the single path
// every embedding consumer takes (ingest's embedding step, `ctxt find` and
// other retrieval, `ctxt embeddings *`, `dpkms serve`, `dpkms dev
// reindex-vectors`); nothing reads providers.embedding from config directly.
//
// # API
//
//	r := &embeddings.Resolver{
//		Config:          cfg.Providers.Embedding, // config file layer
//		ConfigOverrides: overrides,               // raw -c key=value map
//		Registry:        registryStore,           // optional; needed for ModelID
//	}
//	res, err := r.Resolve(ctx, embeddings.Request{
//		ModelID:   "",                      // optional: a registered model_id
//		Overrides: embeddings.Overrides{},  // per-invocation flag values
//	})
//	p, err := res.Provider() // providers.EmbeddingProvider
//
// Resolved carries the effective backend, model, endpoint, api_key_env and
// dimension, plus the layer each one came from (Source, Explain).
//
// # Precedence
//
// Resolution is per field: a layer that sets only the endpoint leaves the
// model to lower layers. Highest first:
//
//  1. flags: --embedding-provider, --embedding-model, --embedding-endpoint
//  2. env: CTXT_EMBEDDING_PROVIDER, CTXT_EMBEDDING_MODEL,
//     CTXT_EMBEDDING_ENDPOINT, CTXT_EMBEDDING_API_KEY_ENV
//  3. -c providers.embedding.<field>=<value>
//  4. the model registry entry, only when Request.ModelID names one:
//     backend from its provider column, dimension from its dimension
//     column, model / endpoint / api_key_env from its config_json
//  5. providers.embedding in the config files
//  6. built-in defaults (ollama, nomic-embed-text, http://localhost:11434)
//
// Env outranks -c here, although for other config keys kit applies -c
// after env. The resolver reads both layers itself, so the order above
// holds regardless of kit's layering.
//
// An empty string (or a zero dimension) never counts as set, so it falls
// through to the next layer. api_key_env is the NAME of an environment
// variable; the resolver never reads or reports the key itself.
package embeddings

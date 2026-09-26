package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// providerResolver adapts a Resolver to ProviderResolver. Every call
// applies the Resolver's env, -c, config and Flags layers.
//
// ForModel returns one provider per distinct resolved setting set, so
// state a provider learns once (the Ollama provider's model context
// length) is kept across the per-object embed calls of a run.
type providerResolver struct {
	r *Resolver

	mu     sync.Mutex
	cached map[providerKey]providers.EmbeddingProvider
}

// providerKey is every resolved setting that shapes a provider.
type providerKey struct {
	backend, model, endpoint, apiKeyEnv string
	dimension                           int
}

var _ ProviderResolver = (*providerResolver)(nil)

// NewProviderResolver returns the ProviderResolver backed by r. r.Flags
// carries the per-invocation flag values.
func NewProviderResolver(r *Resolver) ProviderResolver {
	return &providerResolver{r: r}
}

// ForModel resolves m from its own entry: backend, model and dimension are
// fixed by m (config_json and the dimension column); flags, env and -c may
// change only the endpoint and api_key_env, and fail if they would change
// the identity.
func (p *providerResolver) ForModel(_ context.Context, m registry.Model) (providers.EmbeddingProvider, error) {
	res, err := p.r.resolve(p.r.Flags, &m)
	if err != nil {
		return nil, err
	}
	key := providerKey{res.Backend, res.Model, res.Endpoint, res.APIKeyEnv, res.Dimension}
	p.mu.Lock()
	defer p.mu.Unlock()
	if prov, ok := p.cached[key]; ok {
		return prov, nil
	}
	prov, err := res.Provider()
	if err != nil {
		return nil, err
	}
	if p.cached == nil {
		p.cached = map[providerKey]providers.EmbeddingProvider{}
	}
	p.cached[key] = prov
	return prov, nil
}

// ForRegistration resolves with no registered model (full per-field
// overrides) and returns the config_json to store on the new entry:
// backend, model, endpoint and the api_key_env variable NAME. The key value
// and the dimension are never part of it.
func (p *providerResolver) ForRegistration(_ context.Context) (providers.EmbeddingProvider, json.RawMessage, error) {
	res, err := p.r.resolve(p.r.Flags, nil)
	if err != nil {
		return nil, nil, err
	}
	prov, err := res.Provider()
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(ModelConfig{
		Backend:   res.Backend,
		Model:     res.Model,
		Endpoint:  res.Endpoint,
		APIKeyEnv: res.APIKeyEnv,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("embedding registration config_json: %w", err)
	}
	return prov, raw, nil
}

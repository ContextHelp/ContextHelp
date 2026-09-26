package embeddings

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ModelSource answers which registered models the query path reads and the
// ingest path writes.
type ModelSource interface {
	// Default returns the is_default = 1 model, or registry.ErrNoDefaultModel.
	Default(ctx context.Context) (*registry.Model, error)
	// Populating returns the default plus every model whose deprecation is
	// not effective at now, excluding models with no measured dimension.
	Populating(ctx context.Context, now time.Time) ([]registry.Model, error)
}

var _ ModelSource = (*registry.Store)(nil)

// ProviderResolver builds embedding providers. A registered model's
// config_json is its vector-space identity (backend + model); runtime
// overrides may change transport only (endpoint, credentials). An override
// that would change a registered model's backend or model must fail rather
// than silently write or query foreign vectors under that model_id.
type ProviderResolver interface {
	// ForModel returns the provider that produces vectors for m: m's
	// config_json overlaid with transport-only runtime overrides.
	ForModel(ctx context.Context, m registry.Model) (providers.EmbeddingProvider, error)
	// ForRegistration returns the provider resolved from configuration plus
	// runtime overrides (no registry input) and the config_json document to
	// persist on the model being registered.
	ForRegistration(ctx context.Context) (providers.EmbeddingProvider, json.RawMessage, error)
}

// SpecFor maps a registry row to the index spec an EmbeddingStore builds from.
func SpecFor(m registry.Model) storage.EmbeddingModelSpec {
	return storage.EmbeddingModelSpec{
		ModelID:   m.ModelID,
		Provider:  m.Provider,
		Dimension: m.Dimension,
	}
}

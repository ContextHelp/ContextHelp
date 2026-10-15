package steps

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// noDefaultWarning limits the "no default model" warning to once per process.
var noDefaultWarning sync.Once

// EmbeddingGenerator embeds the draft once per populating model (ADR-071,
// amendment 2026-09-26) and attaches the results to draft.Vectors. It never
// touches storage: whoever persists the draft writes the vectors through
// EmbeddingStore.Put once the object ID is final.
type EmbeddingGenerator struct {
	pipeline.BaseContract
	models   embeddings.ModelSource
	resolver embeddings.ProviderResolver
	// masked also embeds the masked projection (ADR-080 §7) as the
	// masked variant, for objects whose masked text differs from the raw
	// one. On when the instance shares (protected or public).
	masked bool
}

// EmbeddingOption configures NewEmbeddingGenerator.
type EmbeddingOption func(*EmbeddingGenerator)

// WithMaskedVariant turns the masked variant on or off: on, every object
// whose masked projection (a structured type's searchable minimum, or
// the body without classed parts) differs from its raw text gets a
// second vector per model, variant masked, that restricted viewers
// search. Objects where nothing is withheld keep one vector for both.
func WithMaskedVariant(on bool) EmbeddingOption {
	return func(s *EmbeddingGenerator) { s.masked = on }
}

// NewEmbeddingGenerator returns the embedding step. models supplies the
// populate set (the default plus every model not yet effectively
// deprecated); resolver builds each model's provider from its registry
// entry. A nil dependency makes the step a no-op.
func NewEmbeddingGenerator(models embeddings.ModelSource, resolver embeddings.ProviderResolver, opts ...EmbeddingOption) *EmbeddingGenerator {
	s := &EmbeddingGenerator{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Vectors"},
		}),
		models:   models,
		resolver: resolver,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *EmbeddingGenerator) Name() string { return "embedding_generator" }

// Run embeds projection.EmbeddingText(draft) as chunk 0 under every
// populating model, and, with the masked variant on, the masked
// projection as the masked chunk 0 where it differs. The text takes the
// body from TextContent, else RawContent, as storage does on persist, so
// a draft whose pipeline adds no sections or summaries (text.short)
// still embeds its body.
//
// A model that fails (provider unresolvable or unreachable, wrong
// dimension) is logged with its model_id and skipped: vectors are additive
// and never fail the run, and the missing row is what the migration
// backfill picks up. Whether an object is embedded under a model is read
// from the embeddings table, never from the draft.
func (s *EmbeddingGenerator) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if s.models == nil || s.resolver == nil {
		return draft, nil
	}
	draft.Vectors = nil

	text := projection.EmbeddingText(draft)
	if text == "" {
		return draft, nil
	}
	masked := ""
	if s.masked {
		if m := projection.MaskedEmbeddingText(draft); m != "" && m != text {
			masked = m
		}
	}

	models, err := s.models.Populating(ctx, time.Now())
	if err != nil {
		slog.Warn("embedding: read populating models; no vectors produced", "err", err)
		return draft, nil
	}

	hasDefault := false
	for _, m := range models {
		if m.IsDefault {
			hasDefault = true
		}
		p, err := s.resolver.ForModel(ctx, m)
		if err != nil {
			slog.Warn("embedding: model produced no vector; skipping it", "model_id", m.ModelID, "err", fmt.Errorf("resolve provider: %w", err))
			continue
		}
		vec, err := embedWith(ctx, p, m, text)
		if err != nil {
			slog.Warn("embedding: model produced no vector; skipping it", "model_id", m.ModelID, "err", err)
			continue
		}
		draft.Vectors = append(draft.Vectors, storage.ObjectVector{ModelID: m.ModelID, ChunkIdx: 0, Vector: vec, Text: text})
		if masked == "" {
			continue
		}
		mvec, err := embedWith(ctx, p, m, masked)
		if err != nil {
			slog.Warn("embedding: model produced no masked vector; restricted search falls back to the raw one", "model_id", m.ModelID, "err", err)
			continue
		}
		draft.Vectors = append(draft.Vectors, storage.ObjectVector{
			ModelID: m.ModelID, ChunkIdx: 0, Vector: mvec, Text: masked, Variant: storage.EmbeddingVariantMasked,
		})
	}
	if !hasDefault {
		noDefaultWarning.Do(func() {
			slog.Warn("embedding: no default embedding model; objects are not vector-indexed until one is registered and set as default")
		})
	}
	return draft, nil
}

// embedWith embeds text with p, requiring m's registry dimension.
func embedWith(ctx context.Context, p providers.EmbeddingProvider, m registry.Model, text string) ([]float32, error) {
	vec, err := p.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(vec) != m.Dimension {
		return nil, fmt.Errorf("provider returned %d dimensions, registry has %d: %w",
			len(vec), m.Dimension, storage.ErrEmbeddingDimension)
	}
	return vec, nil
}

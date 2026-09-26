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
}

// NewEmbeddingGenerator returns the embedding step. models supplies the
// populate set (the default plus every model not yet effectively
// deprecated); resolver builds each model's provider from its registry
// entry. A nil dependency makes the step a no-op.
func NewEmbeddingGenerator(models embeddings.ModelSource, resolver embeddings.ProviderResolver) *EmbeddingGenerator {
	return &EmbeddingGenerator{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Vectors"},
		}),
		models:   models,
		resolver: resolver,
	}
}

func (s *EmbeddingGenerator) Name() string { return "embedding_generator" }

// Run embeds projection.EmbeddingText(draft) as chunk 0 under every
// populating model. The text takes the body from TextContent, else
// RawContent, as storage does on persist, so a draft whose pipeline adds
// no sections or summaries (text.short) still embeds its body.
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
		vec, err := s.embed(ctx, m, text)
		if err != nil {
			slog.Warn("embedding: model produced no vector; skipping it", "model_id", m.ModelID, "err", err)
			continue
		}
		draft.Vectors = append(draft.Vectors, storage.ObjectVector{
			ModelID:  m.ModelID,
			ChunkIdx: 0,
			Vector:   vec,
			Text:     text,
		})
	}
	if !hasDefault {
		noDefaultWarning.Do(func() {
			slog.Warn("embedding: no default embedding model; objects are not vector-indexed until one is registered and set as default")
		})
	}
	return draft, nil
}

// embed resolves m's provider and embeds text, requiring the registry
// dimension.
func (s *EmbeddingGenerator) embed(ctx context.Context, m registry.Model, text string) ([]float32, error) {
	p, err := s.resolver.ForModel(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("resolve provider: %w", err)
	}
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

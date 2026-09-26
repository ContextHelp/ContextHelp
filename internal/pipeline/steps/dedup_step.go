package steps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DedupStep is a pipeline step that checks for near-duplicate objects in
// the default embedding model's index after the embedding step has run. It
// applies the configured policy: warn (annotate + continue), drop (annotate
// + suppress), keep (pass through).
//
// builtins.InjectDedupStep inserts it after "embedding" when
// cfg.Duplicates.CheckSimilar == true.
type DedupStep struct {
	pipeline.BaseContract
	models embeddings.ModelSource
	store  storage.EmbeddingStore
	cfg    config.DuplicatesConfig
}

// NewDedupStep creates a DedupStep over the default model's index. A nil
// models or store makes it a passthrough.
func NewDedupStep(models embeddings.ModelSource, store storage.EmbeddingStore, cfg config.DuplicatesConfig) *DedupStep {
	return &DedupStep{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Vectors"},
			Produces: []string{"Metadata"},
		}),
		models: models,
		store:  store,
		cfg:    cfg,
	}
}

func (s *DedupStep) Name() string { return "dedup" }

// dedupTopK leaves room for the draft itself (reanalyze keeps the ID) in
// front of the nearest other object.
const dedupTopK = 2

func (s *DedupStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	if !s.cfg.CheckSimilar || s.models == nil || s.store == nil {
		return draft, nil
	}

	def, err := s.models.Default(ctx)
	if err != nil {
		if !errors.Is(err, registry.ErrNoDefaultModel) {
			slog.Warn("dedup: read default embedding model (non-fatal)", "err", err)
		}
		return draft, nil
	}
	query := defaultModelVector(draft.Vectors, def.ModelID)
	if query == nil {
		return draft, nil
	}

	hits, err := s.store.Search(ctx, storage.VectorQuery{ModelID: def.ModelID, Vector: query, TopK: dedupTopK})
	if err != nil {
		slog.Warn("dedup: vector search (non-fatal)", "model_id", def.ModelID, "err", err)
		return draft, nil
	}

	for _, hit := range hits {
		if hit.ObjectID == draft.ID {
			continue // skip self
		}
		score := 1 - hit.Distance
		if score < s.cfg.SimilarityThreshold {
			break // hits are closest first
		}

		// Found a near-duplicate. Apply policy.
		draft.Metadata["duplicate_of"] = hit.ObjectID
		draft.Metadata["duplicate_similarity"] = score
		draft.Metadata["duplicate_kind"] = "similar"

		switch s.cfg.Policy {
		case "drop":
			// Signal to the caller/pipeline executor that this object should not be persisted.
			draft.Metadata["suppress_output"] = true
		case "warn":
			fmt.Printf("warning: near-duplicate detected (similarity=%.4f): existing object %s\n",
				score, hit.ObjectID)
			// Continue ingestion.
		case "keep":
			// Continue silently.
		}
		break // only check first match
	}

	return draft, nil
}

// defaultModelVector returns the draft's chunk-0 vector under modelID.
func defaultModelVector(vectors []storage.ObjectVector, modelID string) []float32 {
	for _, v := range vectors {
		if v.ModelID == modelID && v.ChunkIdx == 0 {
			return v.Vector
		}
	}
	return nil
}

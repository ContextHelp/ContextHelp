package steps

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DedupStep is a pipeline step that checks for near-duplicate objects in
// the default embedding model's index after the embedding step has run.
// A match at or above the similarity threshold is recorded on the draft's
// Metadata (duplicate_of, duplicate_similarity, duplicate_kind) and the
// configured policy applies: warn (the default) also logs a warning, keep
// records silently, drop also sets suppress_output.
//
// builtins.InjectDedupStep inserts it after "embedding" in every pipeline
// that embeds when cfg.Duplicates.CheckSimilar == true.
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
		if errors.Is(err, registry.ErrNoDefaultModel) {
			slog.Debug("dedup: skipped", "reason", "no default embedding model", "object", draft.ID)
		} else {
			slog.Warn("dedup: read default embedding model (non-fatal)", "err", err)
		}
		return draft, nil
	}
	query := defaultModelVector(draft.Vectors, def.ModelID)
	if query == nil {
		slog.Debug("dedup: skipped", "reason", "no vector for the default model", "object", draft.ID, "model_id", def.ModelID)
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
		case "keep":
			// Recorded above; continue silently.
		default: // "warn"; config validation reads an empty policy as warn
			slog.Warn("dedup: near-duplicate detected",
				"object", draft.ID, "duplicate_of", hit.ObjectID, "similarity", score, "model_id", def.ModelID)
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

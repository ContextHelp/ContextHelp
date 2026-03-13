package steps

import (
	"context"
	"fmt"
	"math"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DedupStep is a pipeline step that checks for near-duplicate objects using
// vector embeddings after the embedding step has run. It applies the configured
// policy: warn (annotate + continue), drop (annotate + suppress), keep (pass through).
//
// Register in builtins.go as "dedup" step, inserted after "embedding" when
// cfg.Duplicates.CheckSimilar == true.
type DedupStep struct {
	pipeline.BaseContract
	store storage.ObjectStore
	cfg   config.DuplicatesConfig
}

// NewDedupStep creates a DedupStep. Pass nil store for passthrough (no-op) mode.
func NewDedupStep(store storage.ObjectStore, cfg config.DuplicatesConfig) *DedupStep {
	return &DedupStep{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Embeddings"},
			Produces: []string{"Metadata"},
		}),
		store: store,
		cfg:   cfg,
	}
}

func (s *DedupStep) Name() string { return "dedup" }

func (s *DedupStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	if !s.cfg.CheckSimilar || len(draft.Embeddings) == 0 || s.store == nil {
		return draft, nil
	}

	candidates, err := s.store.VectorSearch(ctx, draft.Embeddings, storage.ObjectFilter{Limit: 1})
	if err != nil {
		// Non-fatal: log and continue.
		fmt.Printf("dedup: vector search error (non-fatal): %v\n", err)
		return draft, nil
	}

	for _, candidate := range candidates {
		if candidate.ID == draft.ID {
			continue // skip self
		}
		score := 0.0
		if v, ok := candidate.Metadata["score"].(float64); ok {
			score = v
		} else {
			score = dedupCosineSimilarity(draft.Embeddings, candidate.Embeddings)
		}
		if score < s.cfg.SimilarityThreshold {
			continue
		}

		// Found a near-duplicate. Apply policy.
		draft.Metadata["duplicate_of"] = candidate.ID
		draft.Metadata["duplicate_similarity"] = score
		draft.Metadata["duplicate_kind"] = "similar"

		switch s.cfg.Policy {
		case "drop":
			// Signal to the caller/pipeline executor that this object should not be persisted.
			draft.Metadata["suppress_output"] = true
		case "warn":
			fmt.Printf("warning: near-duplicate detected (similarity=%.4f): existing object %s\n",
				score, candidate.ID)
			// Continue ingestion.
		case "keep":
			// Continue silently.
		}
		break // only check first match
	}

	return draft, nil
}

// dedupCosineSimilarity is a local copy to avoid an import cycle with the service package.
func dedupCosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

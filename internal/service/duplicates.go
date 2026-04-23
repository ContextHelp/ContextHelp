package service

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DuplicateKind classifies the type of duplicate match.
type DuplicateKind string

const (
	// DuplicateExact means the content_hash matched an existing object exactly.
	DuplicateExact DuplicateKind = "exact"
	// DuplicateSimilar means cosine similarity exceeded the configured threshold.
	DuplicateSimilar DuplicateKind = "similar"
	// DuplicateSourceKey means the source_key matched an existing object.
	DuplicateSourceKey DuplicateKind = "source_key"
)

// DuplicateResult describes a duplicate match.
type DuplicateResult struct {
	Kind       DuplicateKind
	Existing   *storage.KnowledgeObject
	Similarity float64 // 1.0 for exact matches
}

// checkDuplicates inspects the store for an exact, source-key, or
// near-duplicate of the given content, applying the caller's DuplicatesConfig.
// Returns nil, nil when no duplicate is found.
func (s *Service) checkDuplicates(ctx context.Context, hash, sourceKey string, embeddings []float32, cfg config.DuplicatesConfig) (*DuplicateResult, error) {
	// 1. Exact match via content_hash.
	if cfg.CheckExact && hash != "" {
		existing, err := s.Store.Objects().GetByContentHash(ctx, hash)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return &DuplicateResult{
				Kind:       DuplicateExact,
				Existing:   existing,
				Similarity: 1.0,
			}, nil
		}
	}

	// 2. Source-key match.
	if sourceKey != "" {
		existing, err := s.Store.Objects().GetBySourceKey(ctx, sourceKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return &DuplicateResult{
				Kind:       DuplicateSourceKey,
				Existing:   existing,
				Similarity: 1.0,
			}, nil
		}
	}

	// 3. Near-duplicate via vector similarity.
	if cfg.CheckSimilar && len(embeddings) > 0 {
		filter := storage.ObjectFilter{Limit: 1}
		candidates, err := s.Store.Objects().VectorSearch(ctx, embeddings, filter)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			score := 0.0
			if v, ok := candidate.Metadata["score"].(float64); ok {
				score = v
			} else {
				score = serviceCosineSimilarity(embeddings, candidate.Embeddings)
			}
			if score >= cfg.SimilarityThreshold {
				return &DuplicateResult{
					Kind:       DuplicateSimilar,
					Existing:   candidate,
					Similarity: score,
				}, nil
			}
		}
	}

	return nil, nil
}

// logDedupDecision writes an audit entry recording the dedup outcome.
func (s *Service) logDedupDecision(ctx context.Context, dup *DuplicateResult, policy string) {
	if s.Store.AuditLog() == nil {
		return
	}
	_ = s.Store.AuditLog().Append(ctx, &storage.AuditEntry{
		ID:        uuid.New().String(),
		EventType: "dedup." + string(dup.Kind),
		ObjectID:  dup.Existing.ID,
		Actor:     "system",
		Payload: map[string]any{
			"policy":     policy,
			"similarity": dup.Similarity,
		},
		CreatedAt: time.Now().UTC(),
	})
}

// serviceCosineSimilarity computes cosine similarity between two float32 vectors.
// Returns 0 for empty or length-mismatched vectors.
func serviceCosineSimilarity(a, b []float32) float64 {
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

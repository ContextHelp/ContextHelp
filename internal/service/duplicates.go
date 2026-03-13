package service

import (
	"context"
	"math"

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
)

// DuplicateResult describes a duplicate match.
type DuplicateResult struct {
	Kind       DuplicateKind
	Existing   *storage.KnowledgeObject
	Similarity float64 // 1.0 for exact matches
}

// checkDuplicates inspects the store for an exact or near-duplicate of the
// given content hash / embeddings, applying the caller's DuplicatesConfig.
// Returns nil, nil when no duplicate is found.
func (s *Service) checkDuplicates(ctx context.Context, hash string, embeddings []float32, cfg config.DuplicatesConfig) (*DuplicateResult, error) {
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

	// 2. Near-duplicate via vector similarity.
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

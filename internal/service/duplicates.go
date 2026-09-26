package service

import (
	"context"
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
// The near-duplicate check searches q.ModelID's index with q.Vector; an empty
// q skips it. Returns nil, nil when no duplicate is found.
func (s *Service) checkDuplicates(ctx context.Context, hash, sourceKey string, q storage.VectorQuery, cfg config.DuplicatesConfig) (*DuplicateResult, error) {
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

	// 3. Near-duplicate via vector similarity (score = 1 - cosine distance).
	if cfg.CheckSimilar && len(q.Vector) > 0 {
		candidates, err := s.Store.Objects().VectorSearch(ctx, q, storage.ObjectFilter{Limit: 1})
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			score, _ := candidate.Metadata["score"].(float64)
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

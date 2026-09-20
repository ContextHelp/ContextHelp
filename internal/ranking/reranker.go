// Package ranking provides the Reranker interface and a default implementation
// that scores search candidates using FTS, vector, mention, and graph signals.
package ranking

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Candidate is a pre-merged search result from one or more retrieval legs.
type Candidate struct {
	Object   *storage.KnowledgeObject
	FTSScore float64 // RRF contribution from full-text leg
	VecScore float64 // RRF contribution from vector leg
}

// Result is a reranked candidate with full score breakdown.
type Result struct {
	Object         *storage.KnowledgeObject
	FTS            float64 // RRF FTS contribution
	Vector         float64 // RRF vector contribution
	MentionBoost   float64 // outbound mention count bonus
	GraphRelevance float64 // inbound backlink bonus
	WordOverlap    float64 // post-retrieval word overlap signal
	Total          float64 // sum of all signals
	// DocumentView is pre-computed via ProjectDocument for display surfaces.
	DocumentView pluginapi.DocumentProjection
}

// WeightConfig holds the per-signal weight constants for the default reranker.
// All weights are additive; they are kept small to preserve RRF ordering.
type WeightConfig struct {
	// MentionBoost is the per-mention bonus (capped at MaxMentionBoost total).
	// Default: 0.05.
	MentionBoost float64
	// MaxMentionBoost is the ceiling for the outbound mention bonus. Default: 1.0.
	MaxMentionBoost float64
	// DirectBacklink is the bonus for objects with at least one direct inbound edge.
	// Default: 0.08.
	DirectBacklink float64
	// HopBacklink is the bonus for 2-hop connections via shared entity URIs.
	// Default: 0.03.
	HopBacklink float64
	// WordOverlapWeight scales the word-overlap ratio (0–1) before adding to
	// the total score. Kept intentionally light to act as a tiebreaker signal.
	// Default: 0.15.
	WordOverlapWeight float64
}

// DefaultWeights returns sensible default weight constants.
func DefaultWeights() WeightConfig {
	return WeightConfig{
		MentionBoost:      0.05,
		MaxMentionBoost:   1.0,
		DirectBacklink:    0.08,
		HopBacklink:       0.03,
		WordOverlapWeight: 0.15,
	}
}

// EdgeCounter is the minimal storage interface the reranker needs.
// Satisfied by storage.EdgeStore.
type EdgeCounter interface {
	CountMentionsTo(ctx context.Context, toType, toID string) (int, error)
}

// Reranker merges, scores, and sorts search candidates.
type Reranker interface {
	// Rerank takes a map of pre-scored candidates (keyed by object ID),
	// applies mention + graph + word-overlap signals, and returns results sorted
	// by descending total score.  Results below minScore are excluded.
	// query is the original search string used for word overlap scoring.
	Rerank(ctx context.Context, query string, candidates map[string]Candidate, minScore float64) ([]Result, error)
}

// DefaultReranker is the built-in Reranker implementation.
// It applies outbound-mention boost and inbound-backlink graph scoring on top
// of the per-leg RRF contributions already embedded in each Candidate.
type DefaultReranker struct {
	edges   EdgeCounter
	weights WeightConfig
}

// New creates a DefaultReranker with the given edge store and weights.
// Pass DefaultWeights() when no custom weights are needed.
func New(edges EdgeCounter, weights WeightConfig) *DefaultReranker {
	return &DefaultReranker{edges: edges, weights: weights}
}

// Rerank implements Reranker.
func (r *DefaultReranker) Rerank(ctx context.Context, query string, candidates map[string]Candidate, minScore float64) ([]Result, error) {
	w := r.weights

	mentionScores := make(map[string]float64, len(candidates))
	graphScores := make(map[string]float64, len(candidates))

	// --- Pass 1: outbound mention bonus + direct backlink ---
	// Use ProjectIndex to resolve mentions from graph nodes when available.
	directNeighbours := make(map[string]struct{})

	// Pre-compute index projections once per candidate.
	idxCache := make(map[string][]string, len(candidates))
	for id, c := range candidates {
		idxCache[id] = projection.ProjectIndex(c.Object).Mentions
	}

	for id, c := range candidates {
		mentions := idxCache[id]

		// Outbound mention bonus.
		if n := len(mentions); n > 0 {
			bonus := float64(n) * w.MentionBoost
			if bonus > w.MaxMentionBoost {
				bonus = w.MaxMentionBoost
			}
			mentionScores[id] = bonus
		}

		// Direct inbound backlinks.
		inbound, err := r.edges.CountMentionsTo(ctx, "object", c.Object.ID)
		if err == nil && inbound > 0 {
			graphScores[id] += w.DirectBacklink
			directNeighbours[id] = struct{}{}
		}
	}

	// --- Pass 2: 2-hop boost via shared entity URIs ---
	if len(directNeighbours) > 0 {
		neighbourEntitySet := make(map[string]struct{})
		for id := range directNeighbours {
			for _, m := range idxCache[id] {
				neighbourEntitySet[m] = struct{}{}
			}
		}
		for id := range candidates {
			if _, isDirect := directNeighbours[id]; isDirect {
				continue
			}
			for _, m := range idxCache[id] {
				if _, shared := neighbourEntitySet[m]; shared {
					graphScores[id] += w.HopBacklink
					break
				}
			}
		}
	}

	// --- Pass 3: projection-aware word overlap ---
	overlapScores := make(map[string]float64, len(candidates))
	if w.WordOverlapWeight > 0 {
		scorer := NewWordOverlapScorer()
		for id, c := range candidates {
			ratio := scorer.Score(query, c.Object)
			overlapScores[id] = ratio * w.WordOverlapWeight
		}
	}

	// --- Build, filter, sort results ---
	results := make([]Result, 0, len(candidates))
	for id, c := range candidates {
		total := c.FTSScore + c.VecScore + mentionScores[id] + graphScores[id] + overlapScores[id]
		if total < minScore {
			continue
		}
		results = append(results, Result{
			Object:         c.Object,
			FTS:            c.FTSScore,
			Vector:         c.VecScore,
			MentionBoost:   mentionScores[id],
			GraphRelevance: graphScores[id],
			WordOverlap:    overlapScores[id],
			Total:          total,
			DocumentView:   projection.ProjectDocument(c.Object),
		})
	}

	// Sort descending by total score, stable on ID for determinism.
	sortResults(results)

	return results, nil
}

// sortResults sorts in-place: descending total, then ascending ID for ties.
func sortResults(results []Result) {
	n := len(results)
	for i := 1; i < n; i++ {
		for j := i; j > 0 && less(results[j], results[j-1]); j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}

func less(a, b Result) bool {
	if a.Total != b.Total {
		return a.Total > b.Total
	}
	return a.Object.ID < b.Object.ID
}

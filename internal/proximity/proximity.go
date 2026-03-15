// Package proximity implements precomputed Object Proximity Index scoring.
// Proximity is measured across semantic, temporal, entity, origin, and behavioral dimensions.
// It uses storage.ProximityFactors and storage.ProximityScore so results can be stored
// directly via storage.ProximityStore without any type conversion.
package proximity

import (
	"math"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ProximityWeightsByType defines per-type factor weights. All weights must sum to 1.0.
var ProximityWeightsByType = map[string]storage.ProximityFactors{
	"conversation": {
		Semantic:   0.30,
		Temporal:   0.30,
		Entity:     0.25,
		Origin:     0.15,
		Behavioral: 0.0,
	},
	"document": {
		Semantic:   0.50,
		Temporal:   0.10,
		Entity:     0.25,
		Origin:     0.15,
		Behavioral: 0.0,
	},
	"decision": {
		Semantic:   0.25,
		Temporal:   0.20,
		Entity:     0.35,
		Origin:     0.20,
		Behavioral: 0.0,
	},
	"url": {
		Semantic:   0.40,
		Temporal:   0.10,
		Entity:     0.20,
		Origin:     0.30,
		Behavioral: 0.0,
	},
	"image": {
		Semantic:   0.45,
		Temporal:   0.15,
		Entity:     0.25,
		Origin:     0.15,
		Behavioral: 0.0,
	},
	"email": {
		Semantic:   0.30,
		Temporal:   0.25,
		Entity:     0.25,
		Origin:     0.20,
		Behavioral: 0.0,
	},
	"default": {
		Semantic:   0.40,
		Temporal:   0.20,
		Entity:     0.25,
		Origin:     0.15,
		Behavioral: 0.0,
	},
}

// GetWeights returns the proximity factor weights for a pair of object types.
// If both types are the same and recognised, the type-specific weights are used.
// Otherwise the "default" weights are returned.
func GetWeights(typeA, typeB string) storage.ProximityFactors {
	if typeA == typeB {
		if w, ok := ProximityWeightsByType[typeA]; ok {
			return w
		}
	}
	return ProximityWeightsByType["default"]
}

// CosineSimilarity computes cosine similarity between two float32 vectors.
// Returns 0.0 when either vector is all-zeros or the lengths differ.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// SemanticProximity computes semantic similarity using pre-stored embeddings on the objects.
// Returns 0.0 if either object has no embeddings.
func SemanticProximity(a, b *storage.KnowledgeObject) float64 {
	if len(a.Embeddings) == 0 || len(b.Embeddings) == 0 {
		return 0.0
	}
	return CosineSimilarity(a.Embeddings, b.Embeddings)
}

// TemporalProximity computes proximity based on how close in time two objects were created.
// Uses exponential decay within time windows.
func TemporalProximity(a, b *storage.KnowledgeObject) float64 {
	diff := math.Abs(a.CreatedAt.Sub(b.CreatedAt).Hours())

	switch {
	case diff < 24: // Same day
		return 1.0
	case diff < 24*7: // Same week
		return 0.8 * math.Exp(-diff/(24*7))
	case diff < 24*30: // Same month
		return 0.5 * math.Exp(-diff/(24*30))
	case diff < 24*365: // Same year
		return 0.2 * math.Exp(-diff/(24*365))
	default:
		return 0.05 // Minimal proximity for old items
	}
}

// EntityProximity computes Jaccard similarity between the entity mention sets of two objects.
func EntityProximity(a, b *storage.KnowledgeObject) float64 {
	setA := make(map[string]bool, len(a.Mentions))
	for _, u := range a.Mentions {
		setA[u.String()] = true
	}

	setB := make(map[string]bool, len(b.Mentions))
	for _, u := range b.Mentions {
		setB[u.String()] = true
	}

	intersection := 0
	for e := range setA {
		if setB[e] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection

	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// OriginProximity scores closeness based on shared source, pipeline, and author.
// The result is capped at 1.0.
func OriginProximity(a, b *storage.KnowledgeObject) float64 {
	score := 0.0

	// Same source (exact match)
	if a.Source != "" && a.Source == b.Source {
		score += 0.4
	}

	// Same pipeline
	if a.Pipeline != "" && a.Pipeline == b.Pipeline {
		score += 0.2
	}

	// Same author (from metadata)
	authorA, _ := a.Metadata["author"].(string)
	authorB, _ := b.Metadata["author"].(string)
	if authorA != "" && authorA == authorB {
		score += 0.3
	}

	// Same URL domain (for URL objects)
	if a.Type == "url" && b.Type == "url" {
		domainA := extractDomain(a.RawContent)
		domainB := extractDomain(b.RawContent)
		if domainA != "" && domainA == domainB {
			score += 0.1
		}
	}

	return math.Min(score, 1.0)
}

// BehavioralProximity is a stub for future co-access pattern proximity.
// Always returns 0.0 until access logs are implemented.
func BehavioralProximity(_, _ *storage.KnowledgeObject) float64 {
	return 0.0
}

// ComputeProximity computes the aggregated proximity score between two objects using
// type-appropriate factor weights. The returned *storage.ProximityScore can be stored
// directly via storage.ProximityStore.Put or PutBatch.
func ComputeProximity(a, b *storage.KnowledgeObject) *storage.ProximityScore {
	weights := GetWeights(a.Type, b.Type)

	factors := storage.ProximityFactors{
		Semantic:   SemanticProximity(a, b),
		Temporal:   TemporalProximity(a, b),
		Entity:     EntityProximity(a, b),
		Origin:     OriginProximity(a, b),
		Behavioral: BehavioralProximity(a, b),
	}

	score := weights.Semantic*factors.Semantic +
		weights.Temporal*factors.Temporal +
		weights.Entity*factors.Entity +
		weights.Origin*factors.Origin +
		weights.Behavioral*factors.Behavioral

	return &storage.ProximityScore{
		ObjectA:    a.ID,
		ObjectB:    b.ID,
		Score:      score,
		Factors:    factors,
		Weights:    weights,
		ComputedAt: time.Now(),
	}
}

// extractDomain parses a raw URL string and returns just the host component.
// Returns empty string on failure.
func extractDomain(rawURL string) string {
	// Find "://" separator
	for i := 0; i < len(rawURL)-2; i++ {
		if rawURL[i] == ':' && rawURL[i+1] == '/' && rawURL[i+2] == '/' {
			rawURL = rawURL[i+3:]
			// Trim to end of host
			for j, c := range rawURL {
				if c == '/' || c == '?' || c == '#' {
					return rawURL[:j]
				}
			}
			return rawURL
		}
	}
	return ""
}

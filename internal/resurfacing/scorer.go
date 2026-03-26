// Package resurfacing scores knowledge objects against a profile's focus areas
// and maintains a queue of surfacing candidates.
package resurfacing

import (
	"math"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ScoreInput is the data supplied to the scorer for a single object.
type ScoreInput struct {
	Object          *storage.KnowledgeObject
	Profile         config.FocusProfile
	ProfileEntitySlugs []string // entity slugs derived from recent objects / profile hints
	Now             time.Time
}

// ScoreResult holds the computed score and a human-readable reason.
type ScoreResult struct {
	Score  float64
	Reason string
}

// Score computes a [0, 1] resurfacing relevance score.
//
// Three signals, equal weight (1/3 each):
//  1. Entity overlap — fraction of profile entity slugs present in object mentions.
//  2. Tag overlap    — fraction of profile tags matching object tags.
//  3. Recency decay  — exponential decay over 30 days; brand-new = 1.0.
func Score(in ScoreInput) ScoreResult {
	entity := entityOverlap(in.Object, in.ProfileEntitySlugs)
	tag := tagOverlap(in.Object, in.Profile.Tags)
	recency := recencyDecay(in.Object.CreatedAt, in.Now)

	score := (entity + tag + recency) / 3.0
	reason := buildReason(entity, tag, recency, in.Profile)

	return ScoreResult{Score: score, Reason: reason}
}

// entityOverlap returns the fraction of profileSlugs that appear in the
// object's mention URIs.
//
// A mention URI has Scheme, Space (host), and ID (path). A profile slug like
// "stripe.api" or "api" is matched against the URI's Space field (which holds
// the namespace-qualified slug in ctxt:// mentions).
func entityOverlap(obj *storage.KnowledgeObject, profileSlugs []string) float64 {
	if len(profileSlugs) == 0 || len(obj.Mentions) == 0 {
		return 0
	}
	// Index mention keys: Space, ID, and "Space.ID" composite.
	objKeys := make(map[string]struct{}, len(obj.Mentions)*3)
	for i := range obj.Mentions {
		m := &obj.Mentions[i]
		if m.Space != "" {
			objKeys[m.Space] = struct{}{}
		}
		if m.ID != "" {
			objKeys[m.ID] = struct{}{}
		}
		if m.Space != "" && m.ID != "" {
			objKeys[m.Space+"."+m.ID] = struct{}{}
			objKeys[m.Space+"/"+m.ID] = struct{}{}
		}
	}

	matches := 0
	for _, slug := range profileSlugs {
		if _, ok := objKeys[slug]; ok {
			matches++
			continue
		}
		// Try suffix match: "stripe.api" → match "api".
		if idx := strings.LastIndex(slug, "."); idx >= 0 {
			if _, ok := objKeys[slug[idx+1:]]; ok {
				matches++
				continue
			}
		}
		// Try prefix match: "stripe" matches "stripe.api" Space.
		if idx := strings.Index(slug, "."); idx >= 0 {
			if _, ok := objKeys[slug[:idx]]; ok {
				matches++
			}
		}
	}
	return float64(matches) / float64(len(profileSlugs))
}

// tagOverlap returns the fraction of profileTags that match any object tag label.
func tagOverlap(obj *storage.KnowledgeObject, profileTags []string) float64 {
	if len(profileTags) == 0 || len(obj.Tags) == 0 {
		return 0
	}
	objTags := make(map[string]struct{}, len(obj.Tags))
	for _, t := range obj.Tags {
		objTags[strings.ToLower(t.Label)] = struct{}{}
	}
	matches := 0
	for _, pt := range profileTags {
		if _, ok := objTags[strings.ToLower(pt)]; ok {
			matches++
		}
	}
	return float64(matches) / float64(len(profileTags))
}

// recencyDecay returns a score in [0,1] using exponential decay.
// Half-life: 30 days. An object created now scores 1.0; after 30 days ~0.5.
func recencyDecay(createdAt, now time.Time) float64 {
	age := now.Sub(createdAt)
	if age < 0 {
		return 1.0
	}
	const halfLifeDays = 30.0
	days := age.Hours() / 24.0
	return math.Exp(-math.Log(2) * days / halfLifeDays)
}

func buildReason(entity, tag, recency float64, profile config.FocusProfile) string {
	parts := make([]string, 0, 3)
	if entity > 0 {
		parts = append(parts, "entity_overlap")
	}
	if tag > 0 {
		parts = append(parts, "tag_overlap")
	}
	if recency > 0.9 {
		parts = append(parts, "recent")
	}
	if len(parts) == 0 {
		return "recency_decay"
	}
	return strings.Join(parts, "+")
}

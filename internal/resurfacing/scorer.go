// Package resurfacing scores knowledge objects against a profile's focus areas
// and maintains a queue of surfacing candidates.
package resurfacing

import (
	"math"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
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
//
// Uses ProjectIndex to resolve tags and mentions from graph nodes when available.
func Score(in ScoreInput) ScoreResult {
	idx := projection.ProjectIndex(in.Object)
	entity := entityOverlapFromIndex(idx.Mentions, in.ProfileEntitySlugs)
	tag := tagOverlapFromIndex(idx.Tags, in.Profile.Tags)
	recency := recencyDecay(in.Object.CreatedAt, in.Now)

	score := (entity + tag + recency) / 3.0
	reason := buildReason(entity, tag, recency, in.Profile)

	return ScoreResult{Score: score, Reason: reason}
}

// entityOverlap returns the fraction of profileSlugs that appear in the
// object's mention URIs. Delegates to entityOverlapFromIndex via ProjectIndex.
func entityOverlap(obj *storage.KnowledgeObject, profileSlugs []string) float64 {
	idx := projection.ProjectIndex(obj)
	return entityOverlapFromIndex(idx.Mentions, profileSlugs)
}

// entityOverlapFromIndex matches profileSlugs against pre-projected mention strings.
// Each mention string is a serialised entity URI ("ctxt://entity/space/id").
// Matching is done against the full string and its space/id components.
func entityOverlapFromIndex(mentions []string, profileSlugs []string) float64 {
	if len(profileSlugs) == 0 || len(mentions) == 0 {
		return 0
	}
	// Build a key set from each mention: full string + derived space/id parts.
	objKeys := make(map[string]struct{}, len(mentions)*3)
	for _, m := range mentions {
		objKeys[m] = struct{}{}
		// Extract space and id from "ctxt://entity/space/id".
		space, id := extractSpaceID(m)
		if space != "" {
			objKeys[space] = struct{}{}
		}
		if id != "" {
			objKeys[id] = struct{}{}
		}
		if space != "" && id != "" {
			objKeys[space+"."+id] = struct{}{}
			objKeys[space+"/"+id] = struct{}{}
		}
	}

	matches := 0
	for _, slug := range profileSlugs {
		if _, ok := objKeys[slug]; ok {
			matches++
			continue
		}
		// Suffix match: "stripe.api" → "api".
		if i := strings.LastIndex(slug, "."); i >= 0 {
			if _, ok := objKeys[slug[i+1:]]; ok {
				matches++
				continue
			}
		}
		// Prefix match: "stripe" → matches space "stripe".
		if i := strings.Index(slug, "."); i >= 0 {
			if _, ok := objKeys[slug[:i]]; ok {
				matches++
			}
		}
	}
	return float64(matches) / float64(len(profileSlugs))
}

// extractSpaceID splits a serialised URI string into its space and id components.
// Handles the canonical "ctxt://entity/space/id" form as well as the legacy
// "ctxt://space.id", "ctxt://space/id", and bare "space.id" forms.
func extractSpaceID(m string) (space, id string) {
	// Strip scheme prefix if present.
	s := m
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// Strip the canonical "entity" namespace so space/id land on the slug parts.
	s = strings.TrimPrefix(s, "entity/")
	// Try dot separator first.
	if i := strings.Index(s, "."); i >= 0 {
		return s[:i], s[i+1:]
	}
	// Try slash separator.
	if i := strings.Index(s, "/"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// tagOverlap returns the fraction of profileTags that match any object tag label.
// Delegates to tagOverlapFromIndex via ProjectIndex.
func tagOverlap(obj *storage.KnowledgeObject, profileTags []string) float64 {
	idx := projection.ProjectIndex(obj)
	return tagOverlapFromIndex(idx.Tags, profileTags)
}

// tagOverlapFromIndex matches profileTags against pre-projected pluginapi.Tag values.
func tagOverlapFromIndex(objTags []storage.Tag, profileTags []string) float64 {
	if len(profileTags) == 0 || len(objTags) == 0 {
		return 0
	}
	tagSet := make(map[string]struct{}, len(objTags))
	for _, t := range objTags {
		tagSet[strings.ToLower(t.Label)] = struct{}{}
	}
	matches := 0
	for _, pt := range profileTags {
		if _, ok := tagSet[strings.ToLower(pt)]; ok {
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

package resurfacing

import (
	"math"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

// ─── recencyDecay ─────────────────────────────────────────────────────────────

func TestRecencyDecay_New(t *testing.T) {
	now := time.Now()
	score := recencyDecay(now, now)
	if score != 1.0 {
		t.Errorf("brand-new object: want 1.0, got %f", score)
	}
}

func TestRecencyDecay_HalfLife(t *testing.T) {
	now := time.Now()
	created := now.Add(-30 * 24 * time.Hour) // exactly 30 days ago
	score := recencyDecay(created, now)
	// Should be ~0.5 (half-life = 30 days).
	if math.Abs(score-0.5) > 0.01 {
		t.Errorf("30-day-old object: want ~0.5, got %f", score)
	}
}

func TestRecencyDecay_Old(t *testing.T) {
	now := time.Now()
	created := now.Add(-90 * 24 * time.Hour) // 90 days ago
	score := recencyDecay(created, now)
	// ~0.125
	if score >= 0.2 {
		t.Errorf("90-day-old object: want <0.2, got %f", score)
	}
}

func TestRecencyDecay_Future(t *testing.T) {
	now := time.Now()
	created := now.Add(24 * time.Hour) // future → clamp to 1.0
	score := recencyDecay(created, now)
	if score != 1.0 {
		t.Errorf("future object: want 1.0, got %f", score)
	}
}

// ─── tagOverlap ───────────────────────────────────────────────────────────────

func TestTagOverlap_NoProfileTags(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Tags: []storage.Tag{{Label: "golang"}},
	}
	if got := tagOverlap(obj, nil); got != 0 {
		t.Errorf("no profile tags: want 0, got %f", got)
	}
}

func TestTagOverlap_NoObjectTags(t *testing.T) {
	obj := &storage.KnowledgeObject{}
	if got := tagOverlap(obj, []string{"golang"}); got != 0 {
		t.Errorf("no object tags: want 0, got %f", got)
	}
}

func TestTagOverlap_FullMatch(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Tags: []storage.Tag{{Label: "golang"}, {Label: "api"}},
	}
	got := tagOverlap(obj, []string{"golang", "api"})
	if math.Abs(got-1.0) > 0.001 {
		t.Errorf("full match: want 1.0, got %f", got)
	}
}

func TestTagOverlap_PartialMatch(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Tags: []storage.Tag{{Label: "golang"}},
	}
	got := tagOverlap(obj, []string{"golang", "rust"})
	// 1 of 2 profile tags matched → 0.5
	if math.Abs(got-0.5) > 0.001 {
		t.Errorf("partial match: want 0.5, got %f", got)
	}
}

func TestTagOverlap_CaseInsensitive(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Tags: []storage.Tag{{Label: "Golang"}},
	}
	got := tagOverlap(obj, []string{"golang"})
	if math.Abs(got-1.0) > 0.001 {
		t.Errorf("case-insensitive match: want 1.0, got %f", got)
	}
}

// ─── entityOverlap ────────────────────────────────────────────────────────────

func makeURI(t *testing.T, s string) uri.URI {
	t.Helper()
	u, err := uri.Parse(s)
	if err != nil {
		t.Fatalf("parse uri %q: %v", s, err)
	}
	return *u
}

func TestEntityOverlap_NoProfileSlugs(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Mentions: []uri.URI{makeURI(t, "ctxt://entity/stripe/api")},
	}
	if got := entityOverlap(obj, nil); got != 0 {
		t.Errorf("no profile slugs: want 0, got %f", got)
	}
}

func TestEntityOverlap_NoMentions(t *testing.T) {
	obj := &storage.KnowledgeObject{}
	if got := entityOverlap(obj, []string{"stripe.api"}); got != 0 {
		t.Errorf("no mentions: want 0, got %f", got)
	}
}

func TestEntityOverlap_FullMatch(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Mentions: []uri.URI{makeURI(t, "ctxt://entity/stripe/api")},
	}
	got := entityOverlap(obj, []string{"stripe.api"})
	if math.Abs(got-1.0) > 0.001 {
		t.Errorf("full match: want 1.0, got %f", got)
	}
}

func TestEntityOverlap_PartialMatch(t *testing.T) {
	obj := &storage.KnowledgeObject{
		Mentions: []uri.URI{makeURI(t, "ctxt://entity/stripe/api")},
	}
	got := entityOverlap(obj, []string{"stripe.api", "github.actions"})
	if math.Abs(got-0.5) > 0.001 {
		t.Errorf("partial match: want 0.5, got %f", got)
	}
}

// ─── extractSpaceID ───────────────────────────────────────────────────────────

// TestExtractSpaceID pins the space/id split across every mention form the
// scorer sees. The canonical cases are the regression guard: without the
// "entity/" prefix strip, "ctxt://entity/stripe/api" yields space="entity",
// id="stripe/api", and every real mention silently scores zero overlap.
func TestExtractSpaceID(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantSpace string
		wantID    string
	}{
		// Canonical entity URI — the form ctxt actually emits.
		{"canonical entity uri", "ctxt://entity/stripe/api", "stripe", "api"},
		{"canonical entity uri hyphenated", "ctxt://entity/acme-corp/billing-api", "acme-corp", "billing-api"},
		// Same shape without the scheme; the strip must not depend on it.
		{"schemeless entity prefix", "entity/stripe/api", "stripe", "api"},
		// Legacy forms must keep working.
		{"legacy dot form", "ctxt://stripe.api", "stripe", "api"},
		{"legacy slash form", "ctxt://stripe/api", "stripe", "api"},
		{"bare dot form", "stripe.api", "stripe", "api"},
		// Dot separator wins over slash, even under the entity namespace.
		{"entity prefix with dot slug", "ctxt://entity/stripe.api", "stripe", "api"},
		// Extra slug segments spill into the id; only the first slash splits.
		{"multi segment slug", "ctxt://entity/acme/team/api", "acme", "team/api"},
		// Edge cases.
		{"entity prefix only", "ctxt://entity/", "", ""},
		{"scheme only", "ctxt://", "", ""},
		{"empty input", "", "", ""},
		{"space without id", "ctxt://stripe", "stripe", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			space, id := extractSpaceID(tt.in)
			if space != tt.wantSpace || id != tt.wantID {
				t.Errorf("extractSpaceID(%q) = (%q, %q), want (%q, %q)",
					tt.in, space, id, tt.wantSpace, tt.wantID)
			}
		})
	}
}

// TestEntityOverlapFromIndex_CanonicalMention is the end-to-end half of the
// same guard: a canonical mention must match slugs written as the bare space,
// the bare id, and the dotted "space.id" pair. Reading "entity" as the space
// drops all three to zero.
func TestEntityOverlapFromIndex_CanonicalMention(t *testing.T) {
	mentions := []string{"ctxt://entity/stripe/api"}
	tests := []struct {
		name  string
		slugs []string
		want  float64
	}{
		{"dotted pair", []string{"stripe.api"}, 1.0},
		{"space only", []string{"stripe"}, 1.0},
		{"id only", []string{"api"}, 1.0},
		{"slashed pair", []string{"stripe/api"}, 1.0},
		{"namespace is not a slug", []string{"entity"}, 0.0},
		{"unrelated slug", []string{"github.actions"}, 0.0},
		{"one of two", []string{"stripe.api", "github.actions"}, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entityOverlapFromIndex(mentions, tt.slugs)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("entityOverlapFromIndex(%v, %v) = %f, want %f",
					mentions, tt.slugs, got, tt.want)
			}
		})
	}
}

// TestEntityOverlapFromIndex_LegacyMentionForms asserts the backward-compat
// claim: pre-canonical mention strings still resolve to the same space/id.
func TestEntityOverlapFromIndex_LegacyMentionForms(t *testing.T) {
	for _, mention := range []string{
		"ctxt://stripe.api",
		"ctxt://stripe/api",
		"stripe.api",
	} {
		t.Run(mention, func(t *testing.T) {
			got := entityOverlapFromIndex([]string{mention}, []string{"stripe.api"})
			if math.Abs(got-1.0) > 0.001 {
				t.Errorf("legacy mention %q: want 1.0, got %f", mention, got)
			}
		})
	}
}

// ─── Score (combined) ─────────────────────────────────────────────────────────

func TestScore_Zero(t *testing.T) {
	now := time.Now()
	obj := &storage.KnowledgeObject{
		ID:        "obj-1",
		CreatedAt: now.Add(-120 * 24 * time.Hour), // very old
	}
	res := Score(ScoreInput{
		Object:  obj,
		Profile: config.FocusProfile{},
		Now:     now,
	})
	// No tags/entities, old object → small score.
	if res.Score >= 0.1 {
		t.Errorf("expected near-zero score, got %f", res.Score)
	}
}

func TestScore_ReasonContainsSignals(t *testing.T) {
	now := time.Now()
	obj := &storage.KnowledgeObject{
		ID:        "obj-2",
		Tags:      []storage.Tag{{Label: "golang"}},
		CreatedAt: now,
	}
	res := Score(ScoreInput{
		Object:  obj,
		Profile: config.FocusProfile{Tags: []string{"golang"}},
		Now:     now,
	})
	if res.Score == 0 {
		t.Error("expected non-zero score")
	}
	if res.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestScore_BoundedZeroToOne(t *testing.T) {
	now := time.Now()
	obj := &storage.KnowledgeObject{
		ID:        "obj-3",
		Tags:      []storage.Tag{{Label: "golang"}, {Label: "api"}},
		CreatedAt: now,
	}
	res := Score(ScoreInput{
		Object: obj,
		Profile: config.FocusProfile{
			Tags: []string{"golang", "api"},
		},
		Now: now,
	})
	if res.Score < 0 || res.Score > 1.0 {
		t.Errorf("score out of range [0,1]: %f", res.Score)
	}
}

// ─── deterministicID ─────────────────────────────────────────────────────────

func TestDeterministicID_Stable(t *testing.T) {
	id1 := deterministicID("obj-a", "research")
	id2 := deterministicID("obj-a", "research")
	if id1 != id2 {
		t.Errorf("deterministic ID not stable: %q vs %q", id1, id2)
	}
}

func TestDeterministicID_Unique(t *testing.T) {
	id1 := deterministicID("obj-a", "research")
	id2 := deterministicID("obj-b", "research")
	id3 := deterministicID("obj-a", "founder")
	if id1 == id2 || id1 == id3 || id2 == id3 {
		t.Errorf("expected distinct IDs; got %q %q %q", id1, id2, id3)
	}
}

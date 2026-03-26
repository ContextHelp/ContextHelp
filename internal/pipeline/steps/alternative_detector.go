package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DefaultAlternativeThreshold is the minimum proximity score to emit an "alternative" edge.
const DefaultAlternativeThreshold = 0.35

// AlternativeDetector is a graph enrichment step that detects semantically
// similar knowledge objects (competing tools, alternative products, related
// documents) and writes "alternative" proximity edges into the EdgeStore.
//
// Similarity is determined by comparing:
//   - Tags (Jaccard overlap)
//   - Type/subtype category match
//   - Entity mentions (Jaccard overlap)
//
// When either store is nil the step acts as a no-op passthrough (safe for
// tests or environments without a live database).
type AlternativeDetector struct {
	pipeline.BaseContract
	objects   storage.ObjectStore
	edges     storage.EdgeStore
	threshold float64
	limit     int
}

// NewAlternativeDetector creates an AlternativeDetector in no-op mode.
// Inject stores via NewAlternativeDetectorWithStores for live operation.
func NewAlternativeDetector() *AlternativeDetector {
	return &AlternativeDetector{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Tags"},
			Produces: []string{"Metadata"},
		}),
		threshold: DefaultAlternativeThreshold,
		limit:     50,
	}
}

// NewAlternativeDetectorWithStores creates an AlternativeDetector backed by
// live object and edge stores.
func NewAlternativeDetectorWithStores(objects storage.ObjectStore, edges storage.EdgeStore) *AlternativeDetector {
	d := NewAlternativeDetector()
	d.objects = objects
	d.edges = edges
	return d
}

// WithThreshold returns a copy of the detector with a custom similarity threshold.
func (d *AlternativeDetector) WithThreshold(t float64) *AlternativeDetector {
	d.threshold = t
	return d
}

// WithLimit sets the maximum number of candidate objects to compare against.
func (d *AlternativeDetector) WithLimit(n int) *AlternativeDetector {
	d.limit = n
	return d
}

func (d *AlternativeDetector) Name() string { return "alternative_detector" }

// Run compares the draft object against existing objects in the store, computes
// an alternative proximity score for each, and writes "alternative" edges for
// pairs whose score meets the threshold. Returns the draft unchanged.
func (d *AlternativeDetector) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	// No-op when stores are absent or the object has no identity yet.
	if d.objects == nil || d.edges == nil || draft.ID == "" {
		return draft, nil
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	// Fetch candidates of the same type for focused comparison.
	filter := storage.ObjectFilter{
		Type:  draft.Type,
		Limit: d.limit,
	}
	candidates, _, err := d.objects.List(ctx, filter)
	if err != nil {
		// Non-fatal: log and pass through without creating edges.
		fmt.Printf("alternative_detector: list objects (non-fatal): %v\n", err)
		return draft, nil
	}

	var linked []string
	for _, candidate := range candidates {
		if candidate.ID == draft.ID {
			continue
		}
		score := d.score(draft, candidate)
		if score < d.threshold {
			continue
		}

		// Check for an existing edge before inserting to avoid duplicates.
		existing, _ := d.edges.ListFrom(ctx, "object", draft.ID)
		if alternativeEdgeExists(existing, candidate.ID) {
			continue
		}

		edge := &storage.Edge{
			ID:       uuid.NewString(),
			FromType: "object",
			FromID:   draft.ID,
			ToType:   "object",
			ToID:     candidate.ID,
			EdgeType: "alternative",
			Weight:   score,
			Metadata: map[string]any{
				"score_breakdown": scoreBreakdown(draft, candidate),
			},
			CreatedAt: time.Now().UTC(),
		}
		if err := d.edges.Create(ctx, edge); err != nil {
			fmt.Printf("alternative_detector: create edge %s→%s (non-fatal): %v\n",
				draft.ID, candidate.ID, err)
			continue
		}
		linked = append(linked, candidate.ID)
	}

	if len(linked) > 0 {
		draft.Metadata["alternative_ids"] = linked
	}
	return draft, nil
}

// ─── Scoring helpers ──────────────────────────────────────────────────────────

// tagWeight is the weight given to tag overlap when computing the overall score.
const (
	tagWeight      = 0.45
	categoryWeight = 0.20
	entityWeight   = 0.35
)

// score computes a proximity score in [0, 1] between draft and candidate by
// combining tag Jaccard, category match, and entity mention Jaccard.
func (d *AlternativeDetector) score(a, b *storage.KnowledgeObject) float64 {
	return tagWeight*jaccardTags(a, b) +
		categoryWeight*categoryScore(a, b) +
		entityWeight*jaccardMentions(a, b)
}

// scoreBreakdown returns per-dimension scores for edge metadata.
func scoreBreakdown(a, b *storage.KnowledgeObject) map[string]float64 {
	return map[string]float64{
		"tags":     jaccardTags(a, b),
		"category": categoryScore(a, b),
		"entities": jaccardMentions(a, b),
	}
}

// jaccardTags computes Jaccard similarity over normalised tag labels.
func jaccardTags(a, b *storage.KnowledgeObject) float64 {
	setA := tagSet(a)
	setB := tagSet(b)
	return jaccard(setA, setB)
}

// categoryScore returns 1.0 when both type and subtype match, 0.5 when only
// type matches, and 0.0 otherwise.
func categoryScore(a, b *storage.KnowledgeObject) float64 {
	if a.Type != b.Type || a.Type == "" {
		return 0.0
	}
	if a.Subtype != "" && a.Subtype == b.Subtype {
		return 1.0
	}
	return 0.5
}

// jaccardMentions computes Jaccard similarity over entity mention strings.
func jaccardMentions(a, b *storage.KnowledgeObject) float64 {
	setA := make(map[string]bool, len(a.Mentions))
	for _, u := range a.Mentions {
		setA[u.String()] = true
	}
	setB := make(map[string]bool, len(b.Mentions))
	for _, u := range b.Mentions {
		setB[u.String()] = true
	}
	return jaccard(setA, setB)
}

// tagSet returns a normalised set of tag labels for an object.
func tagSet(obj *storage.KnowledgeObject) map[string]bool {
	s := make(map[string]bool, len(obj.Tags))
	for _, t := range obj.Tags {
		s[strings.ToLower(strings.TrimSpace(t.Label))] = true
	}
	return s
}

// jaccard computes Jaccard index: |A∩B| / |A∪B|.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// alternativeEdgeExists reports whether any of edges already links to targetID
// with EdgeType "alternative".
func alternativeEdgeExists(edges []*storage.Edge, targetID string) bool {
	for _, e := range edges {
		if e.EdgeType == "alternative" && e.ToID == targetID {
			return true
		}
	}
	return false
}

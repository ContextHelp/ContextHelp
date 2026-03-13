package proximity_test

import (
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/proximity"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// --- CosineSimilarity ---

func TestCosineSimilarity_Identical(t *testing.T) {
	v := []float32{1, 0, 0, 1}
	got := proximity.CosineSimilarity(v, v)
	if got < 0.999 || got > 1.001 {
		t.Fatalf("expected ~1.0, got %f", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	got := proximity.CosineSimilarity(a, b)
	if got != 0.0 {
		t.Fatalf("expected 0.0, got %f", got)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{1, 0}
	got := proximity.CosineSimilarity(a, b)
	if got != 0.0 {
		t.Fatalf("expected 0.0, got %f", got)
	}
}

func TestCosineSimilarity_LengthMismatch(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{1, 0, 0}
	got := proximity.CosineSimilarity(a, b)
	if got != 0.0 {
		t.Fatalf("expected 0.0 for mismatched lengths, got %f", got)
	}
}

// --- TemporalProximity ---

func TestTemporalProximity_SameInstant(t *testing.T) {
	now := time.Now()
	a := &storage.KnowledgeObject{CreatedAt: now}
	b := &storage.KnowledgeObject{CreatedAt: now}
	got := proximity.TemporalProximity(a, b)
	if got < 0.99 {
		t.Fatalf("expected ~1.0 for same instant, got %f", got)
	}
}

func TestTemporalProximity_SameDay(t *testing.T) {
	now := time.Now()
	a := &storage.KnowledgeObject{CreatedAt: now}
	b := &storage.KnowledgeObject{CreatedAt: now.Add(12 * time.Hour)}
	got := proximity.TemporalProximity(a, b)
	if got < 0.5 || got > 1.01 {
		t.Fatalf("expected high proximity for same-day objects (0.5-1.0), got %f", got)
	}
}

func TestTemporalProximity_SameWeek(t *testing.T) {
	now := time.Now()
	a := &storage.KnowledgeObject{CreatedAt: now}
	b := &storage.KnowledgeObject{CreatedAt: now.Add(5 * 24 * time.Hour)}
	got := proximity.TemporalProximity(a, b)
	if got < 0.0 || got > 0.81 {
		t.Fatalf("expected medium proximity for same-week objects, got %f", got)
	}
}

func TestTemporalProximity_OldItems(t *testing.T) {
	now := time.Now()
	a := &storage.KnowledgeObject{CreatedAt: now}
	b := &storage.KnowledgeObject{CreatedAt: now.Add(-400 * 24 * time.Hour)}
	got := proximity.TemporalProximity(a, b)
	if got > 0.05+0.001 {
		t.Fatalf("expected minimal proximity (~0.05) for old items, got %f", got)
	}
}

// --- EntityProximity ---

func TestEntityProximity_FullOverlap(t *testing.T) {
	a := &storage.KnowledgeObject{Mentions: []string{"alice", "bob"}}
	b := &storage.KnowledgeObject{Mentions: []string{"alice", "bob"}}
	got := proximity.EntityProximity(a, b)
	if got < 0.999 || got > 1.001 {
		t.Fatalf("expected 1.0 for full overlap, got %f", got)
	}
}

func TestEntityProximity_PartialOverlap(t *testing.T) {
	a := &storage.KnowledgeObject{Mentions: []string{"alice", "bob"}}
	b := &storage.KnowledgeObject{Mentions: []string{"alice", "carol"}}
	got := proximity.EntityProximity(a, b)
	// intersection=1, union=3 => jaccard=0.333...
	if got < 0.33 || got > 0.34 {
		t.Fatalf("expected ~0.333 for partial overlap, got %f", got)
	}
}

func TestEntityProximity_NoOverlap(t *testing.T) {
	a := &storage.KnowledgeObject{Mentions: []string{"alice"}}
	b := &storage.KnowledgeObject{Mentions: []string{"bob"}}
	got := proximity.EntityProximity(a, b)
	if got != 0.0 {
		t.Fatalf("expected 0.0 for no overlap, got %f", got)
	}
}

func TestEntityProximity_BothEmpty(t *testing.T) {
	a := &storage.KnowledgeObject{}
	b := &storage.KnowledgeObject{}
	got := proximity.EntityProximity(a, b)
	if got != 0.0 {
		t.Fatalf("expected 0.0 when both empty, got %f", got)
	}
}

// --- OriginProximity ---

func TestOriginProximity_SameSource(t *testing.T) {
	a := &storage.KnowledgeObject{Source: "github.com/foo/bar"}
	b := &storage.KnowledgeObject{Source: "github.com/foo/bar"}
	got := proximity.OriginProximity(a, b)
	if got < 0.39 {
		t.Fatalf("expected at least 0.4 for same source, got %f", got)
	}
}

func TestOriginProximity_SamePipeline(t *testing.T) {
	a := &storage.KnowledgeObject{Pipeline: "email-ingest"}
	b := &storage.KnowledgeObject{Pipeline: "email-ingest"}
	got := proximity.OriginProximity(a, b)
	if got < 0.19 {
		t.Fatalf("expected at least 0.2 for same pipeline, got %f", got)
	}
}

func TestOriginProximity_SameAuthor(t *testing.T) {
	a := &storage.KnowledgeObject{Metadata: map[string]any{"author": "alice"}}
	b := &storage.KnowledgeObject{Metadata: map[string]any{"author": "alice"}}
	got := proximity.OriginProximity(a, b)
	if got < 0.29 {
		t.Fatalf("expected at least 0.3 for same author, got %f", got)
	}
}

func TestOriginProximity_NothingInCommon(t *testing.T) {
	a := &storage.KnowledgeObject{Source: "a", Pipeline: "p1"}
	b := &storage.KnowledgeObject{Source: "b", Pipeline: "p2"}
	got := proximity.OriginProximity(a, b)
	if got != 0.0 {
		t.Fatalf("expected 0.0 when nothing in common, got %f", got)
	}
}

func TestOriginProximity_CappedAt1(t *testing.T) {
	a := &storage.KnowledgeObject{
		Source:   "same",
		Pipeline: "same",
		Metadata: map[string]any{"author": "alice"},
	}
	b := &storage.KnowledgeObject{
		Source:   "same",
		Pipeline: "same",
		Metadata: map[string]any{"author": "alice"},
	}
	got := proximity.OriginProximity(a, b)
	if got > 1.0 {
		t.Fatalf("expected <= 1.0, got %f", got)
	}
}

// --- GetWeights ---

func TestGetWeights_SameType(t *testing.T) {
	w := proximity.GetWeights("document", "document")
	if w.Semantic != 0.50 {
		t.Fatalf("expected 0.50 semantic for document, got %f", w.Semantic)
	}
}

func TestGetWeights_DifferentTypes(t *testing.T) {
	w := proximity.GetWeights("document", "email")
	// Different types -> use default
	def := proximity.GetWeights("default", "default")
	if w.Semantic != def.Semantic {
		t.Fatalf("expected default weights for cross-type, got %+v", w)
	}
}

func TestGetWeights_UnknownType(t *testing.T) {
	w := proximity.GetWeights("unknown_type", "unknown_type")
	def := proximity.GetWeights("default", "default")
	if w.Semantic != def.Semantic {
		t.Fatalf("expected default weights for unknown type, got %+v", w)
	}
}

func TestGetWeights_WeightsSumToOne(t *testing.T) {
	types := []string{"conversation", "document", "decision", "url", "image", "email", "default"}
	for _, typ := range types {
		w := proximity.GetWeights(typ, typ)
		sum := w.Semantic + w.Temporal + w.Entity + w.Origin + w.Behavioral
		if sum < 0.999 || sum > 1.001 {
			t.Fatalf("weights for %s sum to %f, want ~1.0", typ, sum)
		}
	}
}

// --- ComputeProximity ---

func TestComputeProximity_ReturnsStorageType(t *testing.T) {
	now := time.Now()
	a := &storage.KnowledgeObject{ID: "o-1", Type: "document", CreatedAt: now}
	b := &storage.KnowledgeObject{ID: "o-2", Type: "document", CreatedAt: now}
	// Should compile and return *storage.ProximityScore
	score := proximity.ComputeProximity(a, b)
	var _ *storage.ProximityScore = score // compile-time type check
	if score == nil {
		t.Fatal("expected non-nil score")
	}
}

func TestComputeProximity_SameObject(t *testing.T) {
	now := time.Now()
	obj := &storage.KnowledgeObject{
		ID:        "o-1",
		Type:      "document",
		Source:    "same",
		Mentions:  []string{"alice"},
		CreatedAt: now,
	}
	score := proximity.ComputeProximity(obj, obj)
	if score.Score < 0.0 || score.Score > 1.0 {
		t.Fatalf("score out of range [0,1]: %f", score.Score)
	}
	if score.ObjectA != "o-1" || score.ObjectB != "o-1" {
		t.Fatalf("expected ObjectA and ObjectB = o-1, got %s %s", score.ObjectA, score.ObjectB)
	}
}

func TestComputeProximity_HighlyRelated(t *testing.T) {
	now := time.Now()
	a := &storage.KnowledgeObject{
		ID:        "o-1",
		Type:      "document",
		Source:    "same-source",
		Pipeline:  "same-pipeline",
		Mentions:  []string{"alice", "bob", "checkout"},
		CreatedAt: now,
	}
	b := &storage.KnowledgeObject{
		ID:        "o-2",
		Type:      "document",
		Source:    "same-source",
		Pipeline:  "same-pipeline",
		Mentions:  []string{"alice", "bob", "checkout"},
		CreatedAt: now.Add(1 * time.Hour),
	}
	score := proximity.ComputeProximity(a, b)
	if score.Score < 0.3 {
		t.Fatalf("expected score >= 0.3 for highly related objects, got %f", score.Score)
	}
}

func TestComputeProximity_Unrelated(t *testing.T) {
	now := time.Now()
	old := now.Add(-500 * 24 * time.Hour)
	a := &storage.KnowledgeObject{
		ID:        "o-1",
		Type:      "document",
		Source:    "source-a",
		Pipeline:  "pipeline-a",
		Mentions:  []string{"alice"},
		CreatedAt: now,
	}
	b := &storage.KnowledgeObject{
		ID:        "o-2",
		Type:      "document",
		Source:    "source-b",
		Pipeline:  "pipeline-b",
		Mentions:  []string{"bob"},
		CreatedAt: old,
	}
	score := proximity.ComputeProximity(a, b)
	if score.Score > 0.1 {
		t.Fatalf("expected low score for unrelated objects, got %f", score.Score)
	}
}

func TestComputeProximity_WithEmbeddings(t *testing.T) {
	now := time.Now()
	// Identical embeddings => semantic proximity = 1.0
	emb := []float32{1, 0, 0, 1}
	a := &storage.KnowledgeObject{
		ID:         "o-1",
		Type:       "document",
		Embeddings: emb,
		CreatedAt:  now,
	}
	b := &storage.KnowledgeObject{
		ID:         "o-2",
		Type:       "document",
		Embeddings: emb,
		CreatedAt:  now,
	}
	score := proximity.ComputeProximity(a, b)
	// semantic=1.0, weight for document=0.50; score should be at least 0.5
	if score.Score < 0.5 {
		t.Fatalf("expected score >= 0.5 with identical embeddings, got %f", score.Score)
	}
	if score.Factors.Semantic < 0.999 {
		t.Fatalf("expected semantic factor ~1.0, got %f", score.Factors.Semantic)
	}
}

func TestComputeProximity_ScoreRange(t *testing.T) {
	now := time.Now()
	a := &storage.KnowledgeObject{ID: "o-1", Type: "note", CreatedAt: now}
	b := &storage.KnowledgeObject{ID: "o-2", Type: "email", CreatedAt: now.Add(10 * time.Hour)}
	score := proximity.ComputeProximity(a, b)
	if score.Score < 0.0 || score.Score > 1.0 {
		t.Fatalf("score must be in [0,1], got %f", score.Score)
	}
}

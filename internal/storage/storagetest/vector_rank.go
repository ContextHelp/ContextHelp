// Package storagetest hosts cross-driver conformance fixtures for the
// storage drivers. Fixtures assert behavior against the storage interfaces
// only — no driver imports, no raw SQL — so every driver runs the identical
// assertions and parity gaps surface as failures, not tribal knowledge.
package storagetest

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fixtureTime is a fixed timestamp so fixture rows are deterministic.
func fixtureTime() time.Time {
	return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
}

// VectorRankDimension is the embedding dimension of the golden rank corpus.
// Drivers must be configured (SetVectorDimension) with this value before
// Init so their ANN paths — not just brute-force fallbacks — are exercised.
const VectorRankDimension = 4

// vectorRankQuery is the fixture query vector.
var vectorRankQuery = []float32{1, 0, 0, 0}

// vectorRankCorpus is the golden corpus. The vectors are chosen so the rank
// order under cosine differs from the order under L2: "vr-top" points along
// the query with magnitude 2 (cosine similarity 1.0, but L2 distance 1.0),
// while "vr-second" is unit-norm at cosine 0.9 (L2 distance ~0.45). A
// driver ranking by L2 puts vr-second first and fails — the fixture pins
// the metric, not merely "sorted by something".
var vectorRankCorpus = []struct {
	ID        string
	Embedding []float32
}{
	{"vr-top", []float32{2, 0, 0, 0}},               // cos 1.0
	{"vr-second", []float32{0.9, 0.43588989, 0, 0}}, // cos 0.9
	{"vr-third", []float32{0.5, 0.8660254, 0, 0}},   // cos 0.5
	{"vr-last", []float32{0, 1, 0, 0}},              // cos 0.0
}

// vectorRankOrder is the golden rank order (best first) both drivers must
// reproduce. Rank order — never score values — is the cross-driver
// contract; RRF fusion upstream consumes ranks only.
var vectorRankOrder = []string{"vr-top", "vr-second", "vr-third", "vr-last"}

// RunVectorRankFixture seeds the golden corpus through ObjectStore.Create
// and asserts VectorSearch reproduces the pinned cosine rank order, plus
// the Metadata["score"] contract: present, score = 1 - cosine_distance,
// in range, monotonically non-increasing.
//
// The driver must already be initialized with VectorRankDimension.
func RunVectorRankFixture(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()

	for _, c := range vectorRankCorpus {
		obj := &storage.KnowledgeObject{
			ID:         c.ID,
			Type:       "note",
			Status:     "active",
			RawContent: "vector rank fixture " + c.ID,
			Embeddings: c.Embedding,
			CreatedAt:  fixtureTime(),
			UpdatedAt:  fixtureTime(),
		}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}

	results, err := drv.Objects().VectorSearch(ctx, vectorRankQuery, storage.ObjectFilter{Limit: len(vectorRankCorpus)})
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) != len(vectorRankOrder) {
		got := make([]string, len(results))
		for i, r := range results {
			got[i] = r.ID
		}
		t.Fatalf("VectorSearch returned %d results, want %d: %v", len(results), len(vectorRankOrder), got)
	}

	prev := math.Inf(1)
	for i, want := range vectorRankOrder {
		got := results[i]
		if got.ID != want {
			t.Errorf("rank %d: got %s want %s (cosine is the pinned metric)", i, got.ID, want)
		}
		score, ok := got.Metadata["score"].(float64)
		if !ok {
			t.Fatalf("rank %d (%s): Metadata[score] missing or not float64: %#v", i, got.ID, got.Metadata["score"])
		}
		if score < -1.000001 || score > 1.000001 {
			t.Errorf("rank %d (%s): score %f outside [-1, 1]", i, got.ID, score)
		}
		if score > prev+1e-6 {
			t.Errorf("rank %d (%s): score %f increases over previous %f; scores must be rank-consistent", i, got.ID, score, prev)
		}
		prev = score
	}

	// Pin the score mapping (1 - cosine_distance = cosine similarity), not
	// just the ordering: a 1/(1+L2) mapping would report 0.5 here.
	if s := results[0].Metadata["score"].(float64); math.Abs(s-1.0) > 0.01 {
		t.Errorf("top score: got %f want ~1.0 (score = 1 - cosine_distance)", s)
	}
	if s := results[len(results)-1].Metadata["score"].(float64); math.Abs(s-0.0) > 0.01 {
		t.Errorf("orthogonal score: got %f want ~0.0 (score = 1 - cosine_distance)", s)
	}
}

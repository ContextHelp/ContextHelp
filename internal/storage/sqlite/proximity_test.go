package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func makeScore(a, b string, score float64, computedAt time.Time) *storage.ProximityScore {
	// Enforce canonical order
	if a > b {
		a, b = b, a
	}
	return &storage.ProximityScore{
		ObjectA: a,
		ObjectB: b,
		Score:   score,
		Factors: storage.ProximityFactors{
			Semantic:   0.5,
			Temporal:   0.3,
			Entity:     0.2,
			Origin:     0.0,
			Behavioral: 0.0,
		},
		Weights: storage.ProximityFactors{
			Semantic:   0.4,
			Temporal:   0.2,
			Entity:     0.25,
			Origin:     0.15,
			Behavioral: 0.0,
		},
		ComputedAt: computedAt,
	}
}

func TestProximityStore_PutAndGet(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	s := makeScore("o-aaa", "o-bbb", 0.75, now)

	if err := ps.Put(ctx, s); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := ps.Get(ctx, "o-aaa", "o-bbb")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected score, got nil")
	}
	if got.ObjectA != "o-aaa" || got.ObjectB != "o-bbb" {
		t.Fatalf("got wrong objects: %s %s", got.ObjectA, got.ObjectB)
	}
	if got.Score != 0.75 {
		t.Fatalf("expected score 0.75, got %f", got.Score)
	}
}

func TestProximityStore_Get_NormalisesOrder(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	s := makeScore("o-aaa", "o-bbb", 0.60, now)
	if err := ps.Put(ctx, s); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Query with reversed order — should still find it.
	got, err := ps.Get(ctx, "o-bbb", "o-aaa")
	if err != nil {
		t.Fatalf("get reversed: %v", err)
	}
	if got == nil {
		t.Fatal("expected score after reverse query, got nil")
	}
}

func TestProximityStore_Get_NotFound(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	got, err := ps.Get(ctx, "o-xxx", "o-yyy")
	if err != nil {
		t.Fatalf("expected no error for missing pair, got: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing pair, got %+v", got)
	}
}

func TestProximityStore_Put_Upsert(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	s := makeScore("o-aaa", "o-bbb", 0.5, now)
	if err := ps.Put(ctx, s); err != nil {
		t.Fatalf("put first: %v", err)
	}

	// Update the score
	s.Score = 0.9
	if err := ps.Put(ctx, s); err != nil {
		t.Fatalf("put upsert: %v", err)
	}

	got, err := ps.Get(ctx, "o-aaa", "o-bbb")
	if err != nil || got == nil {
		t.Fatalf("get after upsert: err=%v got=%v", err, got)
	}
	if got.Score != 0.9 {
		t.Fatalf("expected updated score 0.9, got %f", got.Score)
	}
}

func TestProximityStore_PutBatch(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	scores := []*storage.ProximityScore{
		makeScore("o-a", "o-b", 0.8, now),
		makeScore("o-a", "o-c", 0.6, now),
		makeScore("o-b", "o-c", 0.4, now),
	}
	if err := ps.PutBatch(ctx, scores); err != nil {
		t.Fatalf("put batch: %v", err)
	}

	got, err := ps.Get(ctx, "o-a", "o-b")
	if err != nil || got == nil {
		t.Fatalf("get after batch: err=%v got=%v", err, got)
	}
	if got.Score != 0.8 {
		t.Fatalf("expected 0.8, got %f", got.Score)
	}
}

func TestProximityStore_GetNeighbors(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	scores := []*storage.ProximityScore{
		makeScore("o-a", "o-b", 0.9, now),
		makeScore("o-a", "o-c", 0.5, now),
		makeScore("o-a", "o-d", 0.3, now),
	}
	if err := ps.PutBatch(ctx, scores); err != nil {
		t.Fatalf("put batch: %v", err)
	}

	neighbors, err := ps.GetNeighbors(ctx, "o-a", 10)
	if err != nil {
		t.Fatalf("get neighbors: %v", err)
	}
	if len(neighbors) != 3 {
		t.Fatalf("expected 3 neighbors, got %d", len(neighbors))
	}
	// Should be sorted by score DESC
	if neighbors[0].Score < neighbors[1].Score {
		t.Fatalf("neighbors not sorted by score DESC: %f, %f", neighbors[0].Score, neighbors[1].Score)
	}
}

func TestProximityStore_GetNeighbors_Limit(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	scores := []*storage.ProximityScore{
		makeScore("o-a", "o-b", 0.9, now),
		makeScore("o-a", "o-c", 0.5, now),
		makeScore("o-a", "o-d", 0.3, now),
	}
	if err := ps.PutBatch(ctx, scores); err != nil {
		t.Fatalf("put batch: %v", err)
	}

	neighbors, err := ps.GetNeighbors(ctx, "o-a", 2)
	if err != nil {
		t.Fatalf("get neighbors: %v", err)
	}
	if len(neighbors) != 2 {
		t.Fatalf("expected 2 neighbors with limit=2, got %d", len(neighbors))
	}
}

func TestProximityStore_GetNeighbors_BothDirections(t *testing.T) {
	// Neighbors stored as (o-a, o-b) should appear in both o-a's and o-b's neighbor lists.
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	s := makeScore("o-a", "o-b", 0.7, now)
	if err := ps.Put(ctx, s); err != nil {
		t.Fatalf("put: %v", err)
	}

	neighborsA, err := ps.GetNeighbors(ctx, "o-a", 10)
	if err != nil || len(neighborsA) == 0 {
		t.Fatalf("o-a should have neighbors: err=%v len=%d", err, len(neighborsA))
	}
	neighborsB, err := ps.GetNeighbors(ctx, "o-b", 10)
	if err != nil || len(neighborsB) == 0 {
		t.Fatalf("o-b should have neighbors: err=%v len=%d", err, len(neighborsB))
	}
}

func TestProximityStore_GetNeighborsAbove(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	scores := []*storage.ProximityScore{
		makeScore("o-a", "o-b", 0.9, now),
		makeScore("o-a", "o-c", 0.5, now),
		makeScore("o-a", "o-d", 0.2, now),
	}
	if err := ps.PutBatch(ctx, scores); err != nil {
		t.Fatalf("put batch: %v", err)
	}

	above, err := ps.GetNeighborsAbove(ctx, "o-a", 0.5)
	if err != nil {
		t.Fatalf("get neighbors above: %v", err)
	}
	// Should include 0.9 and 0.5, but not 0.2
	if len(above) != 2 {
		t.Fatalf("expected 2 neighbors above 0.5, got %d", len(above))
	}
	for _, n := range above {
		if n.Score < 0.5 {
			t.Fatalf("score %f is below threshold 0.5", n.Score)
		}
	}
}

func TestProximityStore_Delete(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	scores := []*storage.ProximityScore{
		makeScore("o-a", "o-b", 0.8, now),
		makeScore("o-a", "o-c", 0.6, now),
		makeScore("o-b", "o-c", 0.4, now),
	}
	if err := ps.PutBatch(ctx, scores); err != nil {
		t.Fatalf("put batch: %v", err)
	}

	// Delete all records involving o-a
	if err := ps.Delete(ctx, "o-a"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// o-a records should be gone
	got, err := ps.Get(ctx, "o-a", "o-b")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete, got score")
	}

	// o-b <-> o-c should still exist
	got2, err := ps.Get(ctx, "o-b", "o-c")
	if err != nil || got2 == nil {
		t.Fatalf("unrelated pair should still exist: err=%v got=%v", err, got2)
	}
}

func TestProximityStore_FindStale(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	old := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	recent := time.Now().UTC().Truncate(time.Second)

	scores := []*storage.ProximityScore{
		makeScore("o-a", "o-b", 0.8, old),
		makeScore("o-a", "o-c", 0.6, old),
		makeScore("o-b", "o-c", 0.4, recent),
	}
	if err := ps.PutBatch(ctx, scores); err != nil {
		t.Fatalf("put batch: %v", err)
	}

	// Find records older than 24 hours
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	stale, err := ps.FindStale(ctx, cutoff, 100)
	if err != nil {
		t.Fatalf("find stale: %v", err)
	}
	// o-a and o-b should appear as stale (they have old records)
	if len(stale) == 0 {
		t.Fatal("expected stale object IDs, got none")
	}
}

func TestProximityStore_Stats(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	now := time.Now().UTC().Truncate(time.Second)
	scores := []*storage.ProximityScore{
		makeScore("o-a", "o-b", 0.8, now),  // high
		makeScore("o-a", "o-c", 0.5, now),  // medium
		makeScore("o-b", "o-c", 0.25, now), // low
	}
	if err := ps.PutBatch(ctx, scores); err != nil {
		t.Fatalf("put batch: %v", err)
	}

	stats, err := ps.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalPairs != 3 {
		t.Fatalf("expected 3 total pairs, got %d", stats.TotalPairs)
	}
	if stats.HighProximity != 1 {
		t.Fatalf("expected 1 high proximity pair, got %d", stats.HighProximity)
	}
	if stats.MediumProximity != 1 {
		t.Fatalf("expected 1 medium proximity pair, got %d", stats.MediumProximity)
	}
	if stats.LowProximity != 1 {
		t.Fatalf("expected 1 low proximity pair, got %d", stats.LowProximity)
	}
	if stats.MaxScore != 0.8 {
		t.Fatalf("expected max score 0.8, got %f", stats.MaxScore)
	}
}

func TestProximityStore_Stats_Empty(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	ps := d.Proximity()

	stats, err := ps.Stats(ctx)
	if err != nil {
		t.Fatalf("stats on empty store: %v", err)
	}
	if stats.TotalPairs != 0 {
		t.Fatalf("expected 0 pairs for empty store, got %d", stats.TotalPairs)
	}
}

package sqlite

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
)

// vecTestDim is the dimension used for unit tests (small = fast).
const vecTestDim = 4

func TestVecStore_UpsertAndSearch(t *testing.T) {
	d := newTestDriverDim(t, vecTestDim)
	ctx := context.Background()
	vs := d.Vectors()

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	if err := vs.Upsert(ctx, "obj-1", vec); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	hits, err := vs.Search(ctx, vec, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits: got %d, want 1", len(hits))
	}
	if hits[0].ID != "obj-1" {
		t.Errorf("id: got %q, want obj-1", hits[0].ID)
	}
}

func TestVecStore_Delete(t *testing.T) {
	d := newTestDriverDim(t, vecTestDim)
	ctx := context.Background()
	vs := d.Vectors()

	if err := vs.Upsert(ctx, "obj-1", []float32{0.1, 0.2, 0.3, 0.4}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := vs.Delete(ctx, "obj-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	hits, err := vs.Search(ctx, []float32{0.1, 0.2, 0.3, 0.4}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits after delete: got %d, want 0", len(hits))
	}
}

func TestVecStore_UpsertOverwrite(t *testing.T) {
	d := newTestDriverDim(t, vecTestDim)
	ctx := context.Background()
	vs := d.Vectors()

	_ = vs.Upsert(ctx, "obj-1", []float32{0.1, 0.2, 0.3, 0.4})
	_ = vs.Upsert(ctx, "obj-1", []float32{0.9, 0.8, 0.7, 0.6})

	hits, err := vs.Search(ctx, []float32{0.9, 0.8, 0.7, 0.6}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits: got %d, want 1", len(hits))
	}
}

func TestVecStore_SearchEmpty(t *testing.T) {
	d := newTestDriverDim(t, vecTestDim)
	ctx := context.Background()
	vs := d.Vectors()

	hits, err := vs.Search(ctx, []float32{0.1, 0.2, 0.3, 0.4}, 10)
	if err != nil {
		t.Fatalf("search empty: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits: got %d, want 0", len(hits))
	}
}

func TestVecStore_SearchTopK(t *testing.T) {
	d := newTestDriverDim(t, vecTestDim)
	ctx := context.Background()
	vs := d.Vectors()

	for i := 0; i < 5; i++ {
		v := float32(i) * 0.1
		_ = vs.Upsert(ctx, fmt.Sprintf("obj-%d", i), []float32{v, v, v, v})
	}

	hits, err := vs.Search(ctx, []float32{0.4, 0.4, 0.4, 0.4}, 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("hits: got %d, want 2", len(hits))
	}
}

func TestVecStore_Count(t *testing.T) {
	d := newTestDriverDim(t, vecTestDim)
	ctx := context.Background()
	vs := d.Vectors()

	n, err := vs.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("initial count: got %d, want 0", n)
	}

	_ = vs.Upsert(ctx, "obj-1", []float32{0.1, 0.2, 0.3, 0.4})
	_ = vs.Upsert(ctx, "obj-2", []float32{0.5, 0.6, 0.7, 0.8})

	n, err = vs.Count(ctx)
	if err != nil {
		t.Fatalf("count after upsert: %v", err)
	}
	if n != 2 {
		t.Errorf("count after 2 upserts: got %d, want 2", n)
	}
}

// BenchmarkVecStore_Search_10k benchmarks KNN search over 10k indexed vectors.
// Run with: go test -bench=BenchmarkVecStore -benchmem -tags fts5
func BenchmarkVecStore_Search_10k(b *testing.B) {
	benchmarkVecSearch(b, 10_000)
}

// BenchmarkVecStore_Search_100k benchmarks KNN search over 100k indexed vectors.
// Run with: go test -bench=BenchmarkVecStore -benchmem -tags fts5 -benchtime=30s
func BenchmarkVecStore_Search_100k(b *testing.B) {
	benchmarkVecSearch(b, 100_000)
}

func benchmarkVecSearch(b *testing.B, n int) {
	b.Helper()
	const dim = 1536
	d := newTestDriverDim(b, dim)
	ctx := context.Background()
	vs := d.Vectors()

	// Seed a deterministic RNG for reproducible vectors.
	rng := rand.New(rand.NewSource(42)) //nolint:gosec

	b.Logf("indexing %d vectors (dim=%d)...", n, dim)
	for i := 0; i < n; i++ {
		vec := make([]float32, dim)
		for j := range vec {
			vec[j] = rng.Float32()
		}
		if err := vs.Upsert(ctx, fmt.Sprintf("obj-%d", i), vec); err != nil {
			b.Fatalf("upsert %d: %v", i, err)
		}
	}
	b.Logf("indexed %d vectors", n)

	// Query vector.
	query := make([]float32, dim)
	for j := range query {
		query[j] = rng.Float32()
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		hits, err := vs.Search(ctx, query, 10)
		if err != nil {
			b.Fatalf("search: %v", err)
		}
		_ = hits
	}
}


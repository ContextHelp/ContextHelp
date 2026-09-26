package storagetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// embeddingsByIDOtherModel is a second registered model whose rows must
// never leak into a read of VectorFixtureModelID.
const embeddingsByIDOtherModel = "fixture-ebi-other-4"

// RunEmbeddingsByIDConformance asserts the storage.EmbeddingReader
// contract: the driver's EmbeddingStore implements it, returns exactly the
// chunks stored under the requested model (ordered by chunk index) for ids
// that have them, omits ids without a row under that model or without an
// object, never returns another model's rows, tolerates duplicates, and
// serves id lists larger than any per-query parameter chunk.
func RunEmbeddingsByIDConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	SeedVectorModel(t, drv)
	seedOtherModel(t, drv)

	reader, ok := drv.Embeddings().(storage.EmbeddingReader)
	if !ok {
		t.Fatalf("%T does not implement storage.EmbeddingReader", drv.Embeddings())
	}

	fixture := func(chunks ...[]float32) []storage.ObjectVector {
		out := make([]storage.ObjectVector, len(chunks))
		for i, c := range chunks {
			out[i] = storage.ObjectVector{ModelID: VectorFixtureModelID, ChunkIdx: i, Vector: c}
		}
		return out
	}
	want := map[string][]storage.ObjectVector{
		"ebi-a": fixture([]float32{1, 0, 0, 0}),
		"ebi-b": fixture([]float32{0.5, 0.25, -1, 2}, []float32{0, 1, 0, 0}),
	}
	other := storage.ObjectVector{ModelID: embeddingsByIDOtherModel, Vector: []float32{0, 0, 1, 0}}

	for _, id := range []string{"ebi-a", "ebi-b", "ebi-c"} {
		obj := &storage.KnowledgeObject{
			ID:         id,
			Type:       "note",
			Status:     "active",
			RawContent: "embeddings by id fixture " + id,
			CreatedAt:  fixtureTime(),
			UpdatedAt:  fixtureTime(),
		}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	// ebi-b's chunks go in reversed so the read must order them; ebi-c has
	// a row only under the other model.
	b := want["ebi-b"]
	puts := map[string][]storage.ObjectVector{
		"ebi-a": append(want["ebi-a"], storage.ObjectVector{ModelID: embeddingsByIDOtherModel, Vector: []float32{0, 1, 0, 0}}),
		"ebi-b": {b[1], b[0]},
		"ebi-c": {other},
	}
	for id, vs := range puts {
		if err := drv.Embeddings().Put(ctx, id, vs); err != nil {
			t.Fatalf("Put %s: %v", id, err)
		}
	}

	t.Run("empty", func(t *testing.T) {
		got, err := reader.EmbeddingsByID(ctx, VectorFixtureModelID, nil)
		if err != nil {
			t.Fatalf("EmbeddingsByID(nil): %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("EmbeddingsByID(nil) = %v, want empty", got)
		}
	})

	t.Run("subset", func(t *testing.T) {
		got, err := reader.EmbeddingsByID(ctx, VectorFixtureModelID, []string{"ebi-a", "ebi-c", "ebi-missing", "ebi-b", "ebi-a"})
		if err != nil {
			t.Fatalf("EmbeddingsByID: %v", err)
		}
		assertEmbeddings(t, got, want)
	})

	t.Run("other model only", func(t *testing.T) {
		got, err := reader.EmbeddingsByID(ctx, embeddingsByIDOtherModel, []string{"ebi-a", "ebi-b", "ebi-c"})
		if err != nil {
			t.Fatalf("EmbeddingsByID(other): %v", err)
		}
		assertEmbeddings(t, got, map[string][]storage.ObjectVector{
			"ebi-a": {{ModelID: embeddingsByIDOtherModel, Vector: []float32{0, 1, 0, 0}}},
			"ebi-c": {other},
		})
	})

	t.Run("unknown model", func(t *testing.T) {
		got, err := reader.EmbeddingsByID(ctx, "fixture-unregistered", []string{"ebi-a", "ebi-b"})
		if err != nil {
			t.Fatalf("EmbeddingsByID(unknown model): %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("EmbeddingsByID(unknown model) = %v, want empty", got)
		}
	})

	t.Run("large id list", func(t *testing.T) {
		// Hits sit at the start, middle and end so a driver that chunks
		// the list must serve every chunk.
		ids := []string{"ebi-a"}
		for i := range 1200 {
			if i == 750 {
				ids = append(ids, "ebi-b")
			}
			ids = append(ids, fmt.Sprintf("ebi-absent-%04d", i))
		}
		ids = append(ids, "ebi-c", "ebi-a")
		got, err := reader.EmbeddingsByID(ctx, VectorFixtureModelID, ids)
		if err != nil {
			t.Fatalf("EmbeddingsByID(large): %v", err)
		}
		assertEmbeddings(t, got, want)
	})
}

// seedOtherModel registers embeddingsByIDOtherModel beside the fixture
// model, at the same dimension.
func seedOtherModel(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	reg, err := registry.ForDriver(drv)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	m := registry.Model{ModelID: embeddingsByIDOtherModel, Provider: "fixture", Dimension: VectorRankDimension, ConfigJSON: "{}"}
	if err := reg.Register(ctx, m, false); err != nil && !errors.Is(err, registry.ErrModelAlreadyRegistered) {
		t.Fatalf("register %s: %v", m.ModelID, err)
	}
	if err := drv.Embeddings().EnsureIndex(ctx, storage.EmbeddingModelSpec{
		ModelID: m.ModelID, Provider: m.Provider, Dimension: m.Dimension,
	}); err != nil {
		t.Fatalf("EnsureIndex %s: %v", m.ModelID, err)
	}
}

func assertEmbeddings(t *testing.T, got, want map[string][]storage.ObjectVector) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d objects (%v), want %d", len(got), keys(got), len(want))
	}
	for id, w := range want {
		g, ok := got[id]
		if !ok {
			t.Fatalf("missing embeddings for %s; got ids %v", id, keys(got))
		}
		if len(g) != len(w) {
			t.Fatalf("%s: got %d chunks, want %d", id, len(g), len(w))
		}
		for i := range w {
			if g[i].ModelID != w[i].ModelID || g[i].ChunkIdx != w[i].ChunkIdx || !slices.Equal(g[i].Vector, w[i].Vector) {
				t.Fatalf("%s chunk %d = {%s %d %v}, want {%s %d %v}", id, i,
					g[i].ModelID, g[i].ChunkIdx, g[i].Vector, w[i].ModelID, w[i].ChunkIdx, w[i].Vector)
			}
		}
	}
}

func keys(m map[string][]storage.ObjectVector) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Package embeddingtest holds test doubles for the embedding write path:
// an in-memory storage.EmbeddingStore that keeps the binding EmbeddingStore
// semantics (ADR-071, amendment 2026-09-26), a static embeddings.ModelSource,
// and a gate for end-to-end tests that need a driver's real EmbeddingStore.
//
// Import from _test.go files only.
package embeddingtest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MemStore is an in-memory storage.EmbeddingStore. Put validates every
// vector before it writes any (all-or-nothing), replaces the object's rows
// per model present, and leaves other models' rows alone.
type MemStore struct {
	mu      sync.Mutex
	dims    map[string]int                               // model_id -> indexed dimension
	rows    map[string]map[string][]storage.ObjectVector // model_id -> object_id -> chunks
	puts    int
	PutErr  error // when set, Put fails with it and writes nothing
	objects func() []string
}

var _ storage.EmbeddingStore = (*MemStore)(nil)

// NewMemStore returns an empty store with no indexes.
func NewMemStore() *MemStore {
	return &MemStore{
		dims: map[string]int{},
		rows: map[string]map[string][]storage.ObjectVector{},
	}
}

// WithObjects sets the object-ID lister ListMissing pages over.
func (s *MemStore) WithObjects(fn func() []string) *MemStore {
	s.objects = fn
	return s
}

// EnsureIndex records the model's index at spec.Dimension.
func (s *MemStore) EnsureIndex(_ context.Context, spec storage.EmbeddingModelSpec) error {
	if err := storage.ValidateEmbeddingModelID(spec.ModelID); err != nil {
		return err
	}
	if spec.Dimension <= 0 || spec.Dimension > storage.SQLiteVecMaxDimension {
		return fmt.Errorf("model %s dimension %d: %w", spec.ModelID, spec.Dimension, storage.ErrEmbeddingDimension)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.dims[spec.ModelID]; ok && old != spec.Dimension {
		// A changed dimension is a signature mismatch: rebuild from rows
		// that still fit (none of the old ones do).
		delete(s.rows, spec.ModelID)
	}
	s.dims[spec.ModelID] = spec.Dimension
	return nil
}

// Put replaces objectID's rows for every model present in vectors.
func (s *MemStore) Put(_ context.Context, objectID string, vectors []storage.ObjectVector) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts++
	if s.PutErr != nil {
		return s.PutErr
	}
	for _, v := range vectors {
		dim, ok := s.dims[v.ModelID]
		if !ok {
			return fmt.Errorf("model %s: %w", v.ModelID, storage.ErrEmbeddingIndexMissing)
		}
		if len(v.Vector) != dim {
			return fmt.Errorf("model %s: vector has %d dimensions, registry has %d: %w",
				v.ModelID, len(v.Vector), dim, storage.ErrEmbeddingDimension)
		}
	}
	byModel := map[string][]storage.ObjectVector{}
	for _, v := range vectors {
		if _, seen := byModel[v.ModelID]; !seen {
			byModel[v.ModelID] = nil
		}
		if magnitude(v.Vector) == 0 {
			continue
		}
		c := v
		c.Vector = append([]float32(nil), v.Vector...)
		byModel[v.ModelID] = append(byModel[v.ModelID], c)
	}
	for model, chunks := range byModel {
		if s.rows[model] == nil {
			s.rows[model] = map[string][]storage.ObjectVector{}
		}
		sort.Slice(chunks, func(i, j int) bool { return chunks[i].ChunkIdx < chunks[j].ChunkIdx })
		if len(chunks) == 0 {
			delete(s.rows[model], objectID)
			continue
		}
		s.rows[model][objectID] = chunks
	}
	return nil
}

// Get returns objectID's chunks under modelID, ordered by chunk index.
func (s *MemStore) Get(_ context.Context, objectID, modelID string) ([]storage.ObjectVector, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]storage.ObjectVector(nil), s.rows[modelID][objectID]...), nil
}

// Search returns object-level hits by cosine distance, closest first,
// collapsing chunks to the best one per object.
func (s *MemStore) Search(_ context.Context, q storage.VectorQuery) ([]storage.EmbeddingHit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.dims[q.ModelID]; !ok {
		return nil, fmt.Errorf("model %s: %w", q.ModelID, storage.ErrEmbeddingIndexMissing)
	}
	topK := q.TopK
	if topK <= 0 {
		topK = 10
	}
	hits := make([]storage.EmbeddingHit, 0, len(s.rows[q.ModelID]))
	for objectID, chunks := range s.rows[q.ModelID] {
		best := storage.EmbeddingHit{ObjectID: objectID, Distance: math.Inf(1)}
		for _, c := range chunks {
			if d := cosineDistance(q.Vector, c.Vector); d < best.Distance {
				best.Distance, best.ChunkIdx = d, c.ChunkIdx
			}
		}
		hits = append(hits, best)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Distance != hits[j].Distance {
			return hits[i].Distance < hits[j].Distance
		}
		return hits[i].ObjectID < hits[j].ObjectID
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

// ListMissing pages the WithObjects IDs that have no row under modelID.
func (s *MemStore) ListMissing(_ context.Context, modelID, afterObjectID string, limit int) ([]string, error) {
	if s.objects == nil {
		return nil, errors.New("embeddingtest: MemStore.ListMissing needs WithObjects")
	}
	ids := s.objects()
	sort.Strings(ids)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id <= afterObjectID {
			continue
		}
		if _, ok := s.rows[modelID][id]; ok {
			continue
		}
		out = append(out, id)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

// PurgeModel drops the model's rows and index.
func (s *MemStore) PurgeModel(_ context.Context, modelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, modelID)
	delete(s.dims, modelID)
	return nil
}

// Puts reports how many times Put was called.
func (s *MemStore) Puts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

// ObjectIDs returns the IDs with at least one row under modelID, sorted.
func (s *MemStore) ObjectIDs(modelID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.rows[modelID]))
	for id := range s.rows[modelID] {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func magnitude(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return math.Sqrt(sum)
}

func cosineDistance(a, b []float32) float64 {
	if len(a) != len(b) {
		return math.Inf(1)
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	na, nb := magnitude(a), magnitude(b)
	if na == 0 || nb == 0 {
		return math.Inf(1)
	}
	return 1 - dot/(na*nb)
}

// Models is a static embeddings.ModelSource: Populating follows the
// registry rules (default first, then every model whose deprecation is not
// effective at now, skipping models without a dimension).
type Models []registry.Model

var _ embeddings.ModelSource = Models(nil)

// Default returns the IsDefault model or registry.ErrNoDefaultModel.
func (m Models) Default(context.Context) (*registry.Model, error) {
	for i := range m {
		if m[i].IsDefault {
			c := m[i]
			return &c, nil
		}
	}
	return nil, registry.ErrNoDefaultModel
}

// Populating returns the models ingest writes vectors for at now.
func (m Models) Populating(_ context.Context, now time.Time) ([]registry.Model, error) {
	var def, rest []registry.Model
	for _, x := range m {
		if x.Dimension <= 0 {
			continue
		}
		switch {
		case x.IsDefault:
			def = append(def, x)
		case x.DeprecatedAt == nil || x.DeprecatedAt.After(now):
			rest = append(rest, x)
		}
	}
	return append(def, rest...), nil
}

// RequireEmbeddingStore skips t when the driver's EmbeddingStore is still
// the contract stub (errors.ErrUnsupported), and otherwise builds the index
// for every spec. End-to-end tests against a real driver use it so they
// run as soon as the per-model index lands and stay skipped (not red)
// before.
func RequireEmbeddingStore(t testing.TB, store storage.EmbeddingStore, specs ...storage.EmbeddingModelSpec) {
	t.Helper()
	for _, spec := range specs {
		err := store.EnsureIndex(context.Background(), spec)
		if errors.Is(err, errors.ErrUnsupported) {
			t.Skipf("driver EmbeddingStore not implemented yet: %v", err)
		}
		if err != nil {
			t.Fatalf("EnsureIndex(%s): %v", spec.ModelID, err)
		}
	}
}

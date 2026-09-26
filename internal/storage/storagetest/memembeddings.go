package storagetest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// MemEmbeddingStore is an in-memory storage.EmbeddingStore test double that
// follows the binding EmbeddingStore semantics (ADR-071, amendment
// 2026-09-26): one index per model at a fixed dimension, Put replaces an
// object's rows per model and rejects wrong dimensions without a partial
// write, zero-magnitude vectors are skipped, and Search returns cosine
// distance ascending, one hit per object (its best chunk).
//
// It records every Search so tests can assert which model a query read.
type MemEmbeddingStore struct {
	// ObjectIDs lists the object IDs ListMissing pages over. Nil makes
	// ListMissing fail.
	ObjectIDs func(ctx context.Context) ([]string, error)

	mu       sync.Mutex
	dims     map[string]int
	rows     map[string]map[string][]storage.ObjectVector // model -> object -> chunks
	searches []storage.VectorQuery
}

var _ storage.EmbeddingStore = (*MemEmbeddingStore)(nil)

// NewMemEmbeddingStore returns an empty store with no indexes.
func NewMemEmbeddingStore() *MemEmbeddingStore {
	return &MemEmbeddingStore{
		dims: map[string]int{},
		rows: map[string]map[string][]storage.ObjectVector{},
	}
}

// Searches returns the queries Search received, in call order.
func (m *MemEmbeddingStore) Searches() []storage.VectorQuery {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]storage.VectorQuery(nil), m.searches...)
}

// EnsureIndex creates the model's index at spec.Dimension. Re-ensuring at a
// different dimension rebuilds it, which drops rows that no longer fit.
func (m *MemEmbeddingStore) EnsureIndex(_ context.Context, spec storage.EmbeddingModelSpec) error {
	if err := storage.ValidateEmbeddingModelID(spec.ModelID); err != nil {
		return err
	}
	if spec.Dimension <= 0 || spec.Dimension > storage.SQLiteVecMaxDimension {
		return fmt.Errorf("model %s dimension %d: %w", spec.ModelID, spec.Dimension, storage.ErrEmbeddingDimension)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.dims[spec.ModelID]; ok && old != spec.Dimension {
		delete(m.rows, spec.ModelID)
	}
	m.dims[spec.ModelID] = spec.Dimension
	return nil
}

// Put replaces objectID's rows for every model present in vectors.
func (m *MemEmbeddingStore) Put(_ context.Context, objectID string, vectors []storage.ObjectVector) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	byModel := map[string][]storage.ObjectVector{}
	for _, v := range vectors {
		dim, ok := m.dims[v.ModelID]
		if !ok {
			return fmt.Errorf("model %s: %w", v.ModelID, storage.ErrEmbeddingIndexMissing)
		}
		if len(v.Vector) != dim {
			return fmt.Errorf("model %s: vector has %d dimensions, want %d: %w",
				v.ModelID, len(v.Vector), dim, storage.ErrEmbeddingDimension)
		}
		if _, seen := byModel[v.ModelID]; !seen {
			byModel[v.ModelID] = nil
		}
		if magnitude(v.Vector) == 0 {
			continue
		}
		cp := v
		cp.Vector = append([]float32(nil), v.Vector...)
		byModel[v.ModelID] = append(byModel[v.ModelID], cp)
	}
	for model, chunks := range byModel {
		if m.rows[model] == nil {
			m.rows[model] = map[string][]storage.ObjectVector{}
		}
		if len(chunks) == 0 {
			delete(m.rows[model], objectID)
			continue
		}
		sort.Slice(chunks, func(i, j int) bool { return chunks[i].ChunkIdx < chunks[j].ChunkIdx })
		m.rows[model][objectID] = chunks
	}
	return nil
}

// Get returns objectID's chunks under modelID, ordered by chunk index.
func (m *MemEmbeddingStore) Get(_ context.Context, objectID, modelID string) ([]storage.ObjectVector, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]storage.ObjectVector(nil), m.rows[modelID][objectID]...), nil
}

// Search returns object-level hits, closest first.
func (m *MemEmbeddingStore) Search(_ context.Context, q storage.VectorQuery) ([]storage.EmbeddingHit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.searches = append(m.searches, storage.VectorQuery{
		ModelID: q.ModelID, Vector: append([]float32(nil), q.Vector...), TopK: q.TopK,
	})
	dim, ok := m.dims[q.ModelID]
	if !ok {
		return nil, fmt.Errorf("model %s: %w", q.ModelID, storage.ErrEmbeddingIndexMissing)
	}
	if len(q.Vector) != dim {
		return nil, fmt.Errorf("model %s: query has %d dimensions, want %d: %w",
			q.ModelID, len(q.Vector), dim, storage.ErrEmbeddingDimension)
	}
	topK := q.TopK
	if topK <= 0 {
		topK = 10
	}
	var hits []storage.EmbeddingHit
	for objectID, chunks := range m.rows[q.ModelID] {
		best := storage.EmbeddingHit{ObjectID: objectID, Distance: math.Inf(1)}
		for _, c := range chunks {
			if d := cosineDistance(q.Vector, c.Vector); d < best.Distance {
				best.Distance, best.ChunkIdx = d, c.ChunkIdx
			}
		}
		if !math.IsInf(best.Distance, 1) && !math.IsNaN(best.Distance) {
			hits = append(hits, best)
		}
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

// ListMissing pages object IDs (from ObjectIDs) with no row under modelID.
func (m *MemEmbeddingStore) ListMissing(ctx context.Context, modelID, afterObjectID string, limit int) ([]string, error) {
	if m.ObjectIDs == nil {
		return nil, errors.New("MemEmbeddingStore.ListMissing: ObjectIDs not set")
	}
	ids, err := m.ObjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id <= afterObjectID {
			continue
		}
		if _, ok := m.rows[modelID][id]; ok {
			continue
		}
		out = append(out, id)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// PurgeModel drops the model's rows and index.
func (m *MemEmbeddingStore) PurgeModel(_ context.Context, modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, modelID)
	delete(m.dims, modelID)
	return nil
}

func magnitude(v []float32) float64 {
	var s float64
	for _, f := range v {
		s += float64(f) * float64(f)
	}
	return math.Sqrt(s)
}

func cosineDistance(a, b []float32) float64 {
	na, nb := magnitude(a), magnitude(b)
	if na == 0 || nb == 0 {
		return math.NaN()
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return 1 - dot/(na*nb)
}

// WithEmbeddings wraps drv so Embeddings() returns emb and
// Objects().VectorSearch ranks through emb the way the drivers do: search the
// model's index with over-fetch, hydrate each hit with Get, apply the
// type/subtype/pipeline filter, and set Metadata["score"] = 1 - distance.
// Every other store is drv's own. DB() forwards to drv when it has one, so
// registry.ForDriver works on the wrapper.
func WithEmbeddings(drv storage.StorageDriver, emb storage.EmbeddingStore) storage.StorageDriver {
	return &embeddingsDriver{
		StorageDriver: drv,
		emb:           emb,
		objects:       &embeddingsObjects{ObjectStore: drv.Objects(), emb: emb},
	}
}

type embeddingsDriver struct {
	storage.StorageDriver
	emb     storage.EmbeddingStore
	objects *embeddingsObjects
}

func (d *embeddingsDriver) Embeddings() storage.EmbeddingStore { return d.emb }
func (d *embeddingsDriver) Objects() storage.ObjectStore       { return d.objects }

// DB forwards the wrapped driver's database handle.
func (d *embeddingsDriver) DB() *sql.DB {
	if h, ok := d.StorageDriver.(interface{ DB() *sql.DB }); ok {
		return h.DB()
	}
	return nil
}

type embeddingsObjects struct {
	storage.ObjectStore
	emb storage.EmbeddingStore
}

func (o *embeddingsObjects) VectorSearch(ctx context.Context, q storage.VectorQuery, filter storage.ObjectFilter) ([]*storage.KnowledgeObject, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = q.TopK
	}
	if limit <= 0 {
		limit = 50
	}
	hits, err := o.emb.Search(ctx, storage.VectorQuery{ModelID: q.ModelID, Vector: q.Vector, TopK: limit * 10})
	if err != nil {
		return nil, err
	}
	out := make([]*storage.KnowledgeObject, 0, limit)
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		obj, err := o.Get(ctx, h.ObjectID)
		if err != nil || obj == nil {
			continue
		}
		if (filter.Type != "" && obj.Type != filter.Type) ||
			(filter.Subtype != "" && obj.Subtype != filter.Subtype) ||
			(filter.Pipeline != "" && obj.Pipeline != filter.Pipeline) {
			continue
		}
		if obj.Metadata == nil {
			obj.Metadata = map[string]any{}
		}
		obj.Metadata["score"] = 1 - h.Distance
		out = append(out, obj)
	}
	return out, nil
}

func (o *embeddingsObjects) VectorSearchNodeAware(ctx context.Context, q storage.VectorQuery, filter storage.ObjectFilter, naf pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	if len(naf.NodeTypes) > 0 || len(naf.EdgeTypes) > 0 {
		return nil, errors.New("storagetest.WithEmbeddings: node-aware filters are not supported")
	}
	objs, err := o.VectorSearch(ctx, q, filter)
	if err != nil {
		return nil, err
	}
	out := make([]*pluginapi.NodeAwareResult, len(objs))
	for i, obj := range objs {
		out[i] = &pluginapi.NodeAwareResult{Object: obj}
	}
	return out, nil
}

package storage

import (
	"context"
	"io"
	"testing"
	"time"
)

// mockDriver verifies that a concrete type can satisfy StorageDriver.
type mockDriver struct{}

func (m *mockDriver) Init(ctx context.Context) error   { return nil }
func (m *mockDriver) Close(ctx context.Context) error  { return nil }
func (m *mockDriver) Objects() ObjectStore             { return &mockObjectStore{} }
func (m *mockDriver) Entities() EntityStore            { return &mockEntityStore{} }
func (m *mockDriver) Edges() EdgeStore                 { return &mockEdgeStore{} }
func (m *mockDriver) Jobs() JobStore                   { return &mockJobStore{} }
func (m *mockDriver) Pipelines() PipelineStore         { return &mockPipelineStore{} }
func (m *mockDriver) Steps() StepStore                 { return &mockStepStore{} }
func (m *mockDriver) Registries() RegistryStore        { return &mockRegistryStore{} }
func (m *mockDriver) Reminders() ReminderStore         { return &mockReminderStore{} }
func (m *mockDriver) Feeds() FeedStore                 { return &mockFeedStore{} }
func (m *mockDriver) FeedItems() FeedItemStore         { return &mockFeedItemStore{} }
func (m *mockDriver) Batches() BatchStore              { return &mockBatchStore{} }
func (m *mockDriver) Detectors() DetectorStore         { return &mockDetectorStore{} }
func (m *mockDriver) Blobs() BlobStore                    { return &mockBlobStore{} }
func (m *mockDriver) Proximity() ProximityStore            { return &mockProximityStore{} }
func (m *mockDriver) Health(ctx context.Context) error    { return nil }

type mockBlobStore struct{}

func (m *mockBlobStore) Put(_ context.Context, _ string, _ io.Reader, _ BlobMeta) error {
	return nil
}
func (m *mockBlobStore) Get(_ context.Context, key string) (io.ReadCloser, BlobMeta, error) {
	return nil, BlobMeta{}, nil
}
func (m *mockBlobStore) Delete(_ context.Context, _ string) error         { return nil }
func (m *mockBlobStore) Exists(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *mockBlobStore) List(_ context.Context, _ string) ([]BlobInfo, error) {
	return nil, nil
}
func (m *mockBlobStore) URL(_ context.Context, _ string) (string, error) { return "", nil }

func TestBlobStoreInterfaceSatisfaction(t *testing.T) {
	var s BlobStore = &mockBlobStore{}
	if s == nil {
		t.Fatal("mockBlobStore should satisfy BlobStore")
	}
}

type mockObjectStore struct{}

func (m *mockObjectStore) Create(ctx context.Context, obj *KnowledgeObject) error { return nil }
func (m *mockObjectStore) Get(ctx context.Context, id string) (*KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore) GetByContentHash(ctx context.Context, hash string) (*KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore) List(ctx context.Context, filter ObjectFilter) ([]*KnowledgeObject, int, error) {
	return nil, 0, nil
}
func (m *mockObjectStore) Update(ctx context.Context, obj *KnowledgeObject) error { return nil }
func (m *mockObjectStore) Delete(ctx context.Context, id string) error            { return nil }
func (m *mockObjectStore) ListBySQL(ctx context.Context, where string, args []any, limit, offset int) ([]*KnowledgeObject, int, error) {
	return nil, 0, nil
}
func (m *mockObjectStore) Reinforce(ctx context.Context, hash string, mergeData *KnowledgeObject) (string, error) {
	return "", nil
}
func (m *mockObjectStore) ListWithEmbeddings(ctx context.Context) ([]*KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore) VectorSearch(ctx context.Context, vector []float32, filter ObjectFilter) ([]*KnowledgeObject, error) {
	return nil, nil
}

type mockEntityStore struct{}

func (m *mockEntityStore) Upsert(ctx context.Context, entity *Entity) error { return nil }
func (m *mockEntityStore) Get(ctx context.Context, slug string) (*Entity, error) {
	return nil, nil
}
func (m *mockEntityStore) List(ctx context.Context, filter EntityFilter) ([]*Entity, error) {
	return nil, nil
}
func (m *mockEntityStore) Resolve(ctx context.Context, mention string) (*Entity, error) {
	return nil, nil
}

type mockEdgeStore struct{}

func (m *mockEdgeStore) Create(ctx context.Context, edge *Edge) error { return nil }
func (m *mockEdgeStore) ListFrom(ctx context.Context, fromType, fromID string) ([]*Edge, error) {
	return nil, nil
}
func (m *mockEdgeStore) ListTo(ctx context.Context, toType, toID string) ([]*Edge, error) {
	return nil, nil
}
func (m *mockEdgeStore) Delete(ctx context.Context, id string) error               { return nil }
func (m *mockEdgeStore) DeleteByObject(ctx context.Context, objectID string) error { return nil }

type mockJobStore struct{}

func (m *mockJobStore) Create(ctx context.Context, job *Job) error { return nil }
func (m *mockJobStore) Get(ctx context.Context, id string) (*Job, error) {
	return nil, nil
}
func (m *mockJobStore) List(ctx context.Context, filter JobFilter) ([]*Job, int, error) {
	return nil, 0, nil
}
func (m *mockJobStore) AcquireNext(ctx context.Context) (*Job, error) { return nil, nil }
func (m *mockJobStore) Complete(ctx context.Context, id string, resultID string) error {
	return nil
}
func (m *mockJobStore) Fail(ctx context.Context, id string, errMsg string) error { return nil }
func (m *mockJobStore) Retry(ctx context.Context, id string) error               { return nil }
func (m *mockJobStore) Cancel(ctx context.Context, id string) error              { return nil }
func (m *mockJobStore) RecoverStale(ctx context.Context, timeout int64) (int, error) {
	return 0, nil
}

type mockPipelineStore struct{}

func (m *mockPipelineStore) Create(ctx context.Context, pipeline *Pipeline) error    { return nil }
func (m *mockPipelineStore) Get(ctx context.Context, name string) (*Pipeline, error) { return nil, nil }
func (m *mockPipelineStore) List(ctx context.Context, filter PipelineFilter) ([]*Pipeline, int, error) {
	return nil, 0, nil
}
func (m *mockPipelineStore) Update(ctx context.Context, pipeline *Pipeline) error { return nil }
func (m *mockPipelineStore) Delete(ctx context.Context, name string) error        { return nil }
func (m *mockPipelineStore) Archive(ctx context.Context, name string) error       { return nil }
func (m *mockPipelineStore) Unarchive(ctx context.Context, name string) error     { return nil }

type mockStepStore struct{}

func (m *mockStepStore) Create(ctx context.Context, step *RegisteredStep) error { return nil }
func (m *mockStepStore) Get(ctx context.Context, name string) (*RegisteredStep, error) {
	return nil, nil
}
func (m *mockStepStore) List(ctx context.Context, source string) ([]*RegisteredStep, int, error) {
	return nil, 0, nil
}
func (m *mockStepStore) Unregister(ctx context.Context, name string) error      { return nil }
func (m *mockStepStore) Update(ctx context.Context, step *RegisteredStep) error { return nil }

type mockRegistryStore struct{}

func (m *mockRegistryStore) CacheManifest(ctx context.Context, cache *RegistryCache) error {
	return nil
}
func (m *mockRegistryStore) GetCachedManifest(ctx context.Context, url string) (*RegistryCache, error) {
	return nil, nil
}
func (m *mockRegistryStore) UpdateETag(ctx context.Context, url, etag string) error { return nil }
func (m *mockRegistryStore) List(ctx context.Context) ([]*RegistryCache, int, error) {
	return nil, 0, nil
}

type mockReminderStore struct{}

func (m *mockReminderStore) Create(ctx context.Context, reminder *SystemReminder) error { return nil }
func (m *mockReminderStore) Get(ctx context.Context, id string) (*SystemReminder, error) {
	return nil, nil
}
func (m *mockReminderStore) List(ctx context.Context, activeOnly bool) ([]*SystemReminder, int, error) {
	return nil, 0, nil
}
func (m *mockReminderStore) Dismiss(ctx context.Context, id string) error { return nil }

type mockFeedStore struct{}

func (m *mockFeedStore) Create(ctx context.Context, feed *Feed) error            { return nil }
func (m *mockFeedStore) Get(ctx context.Context, id string) (*Feed, error)       { return nil, nil }
func (m *mockFeedStore) GetByURL(ctx context.Context, url string) (*Feed, error) { return nil, nil }
func (m *mockFeedStore) List(ctx context.Context, filter FeedFilter) ([]*Feed, error) {
	return nil, nil
}
func (m *mockFeedStore) Update(ctx context.Context, feed *Feed) error { return nil }
func (m *mockFeedStore) Delete(ctx context.Context, id string) error  { return nil }

type mockFeedItemStore struct{}

func (m *mockFeedItemStore) Create(ctx context.Context, item *FeedItem) error { return nil }
func (m *mockFeedItemStore) ExistsByGUID(ctx context.Context, feedID, guid string) (bool, error) {
	return false, nil
}

type mockBatchStore struct{}

func (m *mockBatchStore) Create(ctx context.Context, batch *Batch) error     { return nil }
func (m *mockBatchStore) Get(ctx context.Context, id string) (*Batch, error) { return nil, nil }
func (m *mockBatchStore) Update(ctx context.Context, batch *Batch) error     { return nil }

type mockDetectorStore struct{}

func (m *mockDetectorStore) Create(_ context.Context, _ *DetectorRecord) error { return nil }
func (m *mockDetectorStore) Get(_ context.Context, _ string) (*DetectorRecord, error) {
	return nil, nil
}
func (m *mockDetectorStore) List(_ context.Context, _ DetectorFilter) ([]*DetectorRecord, int, error) {
	return nil, 0, nil
}
func (m *mockDetectorStore) Update(_ context.Context, _ *DetectorRecord) error { return nil }
func (m *mockDetectorStore) Delete(_ context.Context, _ string) error          { return nil }
func (m *mockDetectorStore) Enable(_ context.Context, _ string) error          { return nil }
func (m *mockDetectorStore) Disable(_ context.Context, _ string) error         { return nil }

type mockProximityStore struct{}

func (m *mockProximityStore) GetNeighbors(_ context.Context, _ string, _ int) ([]*ProximityScore, error) {
	return nil, nil
}
func (m *mockProximityStore) GetNeighborsAbove(_ context.Context, _ string, _ float64) ([]*ProximityScore, error) {
	return nil, nil
}
func (m *mockProximityStore) Get(_ context.Context, _, _ string) (*ProximityScore, error) {
	return nil, nil
}
func (m *mockProximityStore) Put(_ context.Context, _ *ProximityScore) error { return nil }
func (m *mockProximityStore) PutBatch(_ context.Context, _ []*ProximityScore) error { return nil }
func (m *mockProximityStore) Delete(_ context.Context, _ string) error { return nil }
func (m *mockProximityStore) FindStale(_ context.Context, _ time.Time, _ int) ([]string, error) {
	return nil, nil
}
func (m *mockProximityStore) Stats(_ context.Context) (*ProximityStats, error) { return nil, nil }

func TestStorageDriverInterfaceSatisfaction(t *testing.T) {
	var d StorageDriver = &mockDriver{}
	if d == nil {
		t.Fatal("mockDriver should satisfy StorageDriver")
	}
}

func TestObjectStoreInterfaceSatisfaction(t *testing.T) {
	var s ObjectStore = &mockObjectStore{}
	if s == nil {
		t.Fatal("mockObjectStore should satisfy ObjectStore")
	}
}

func TestEntityStoreInterfaceSatisfaction(t *testing.T) {
	var s EntityStore = &mockEntityStore{}
	if s == nil {
		t.Fatal("mockEntityStore should satisfy EntityStore")
	}
}

func TestEdgeStoreInterfaceSatisfaction(t *testing.T) {
	var s EdgeStore = &mockEdgeStore{}
	if s == nil {
		t.Fatal("mockEdgeStore should satisfy EdgeStore")
	}
}

func TestJobStoreInterfaceSatisfaction(t *testing.T) {
	var s JobStore = &mockJobStore{}
	if s == nil {
		t.Fatal("mockJobStore should satisfy JobStore")
	}
}

package storage

import (
	"context"
	"io"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// mockDriver verifies that a concrete type can satisfy StorageDriver.
type mockDriver struct{}

func (m *mockDriver) Init(ctx context.Context) error     { return nil }
func (m *mockDriver) Close(ctx context.Context) error    { return nil }
func (m *mockDriver) Objects() ObjectStore               { return &mockObjectStore{} }
func (m *mockDriver) Entities() EntityStore              { return &mockEntityStore{} }
func (m *mockDriver) Edges() EdgeStore                   { return &mockEdgeStore{} }
func (m *mockDriver) Jobs() JobStore                     { return &mockJobStore{} }
func (m *mockDriver) Pipelines() PipelineStore           { return &mockPipelineStore{} }
func (m *mockDriver) Steps() StepStore                   { return &mockStepStore{} }
func (m *mockDriver) Registries() RegistryStore          { return &mockRegistryStore{} }
func (m *mockDriver) Reminders() ReminderStore           { return &mockReminderStore{} }
func (m *mockDriver) Feeds() FeedStore                   { return &mockFeedStore{} }
func (m *mockDriver) FeedItems() FeedItemStore           { return &mockFeedItemStore{} }
func (m *mockDriver) Batches() BatchStore                { return &mockBatchStore{} }
func (m *mockDriver) Detectors() DetectorStore           { return &mockDetectorStore{} }
func (m *mockDriver) Blobs() BlobStore                   { return &mockBlobStore{} }
func (m *mockDriver) Proximity() ProximityStore          { return &mockProximityStore{} }
func (m *mockDriver) Watches() WatchStore                { return &mockWatchStore{} }
func (m *mockDriver) Aliases() AliasStore                { return &mockAliasStore{} }
func (m *mockDriver) AuditLog() AuditStore               { return &mockAuditLogStore{} }
func (m *mockDriver) Attachments() AttachmentStore       { return &mockAttachmentStore{} }
func (m *mockDriver) Resurfacing() ResurfacingQueueStore { return &mockResurfacingQueueStore{} }
func (m *mockDriver) Entitlements() EntitlementStore     { return &mockEntitlementStore{} }
func (m *mockDriver) Metering() MeteringStore            { return &mockMeteringStore{} }
func (m *mockDriver) Embeddings() EmbeddingStore         { return nil }
func (m *mockDriver) SavedSearches() SavedSearchStore    { return &mockSavedSearchStore{} }
func (m *mockDriver) SearchHistory() SearchHistoryStore  { return &mockSearchHistoryStore{} }
func (m *mockDriver) Watermarks() WatermarkStore         { return &mockWatermarkStore{} }
func (m *mockDriver) Health(ctx context.Context) error   { return nil }

type mockWatermarkStore struct{}

func (m *mockWatermarkStore) GetWatermark(_ context.Context, _ string) (time.Time, error) {
	return time.Unix(0, 0).UTC(), nil
}
func (m *mockWatermarkStore) SetWatermark(_ context.Context, _ string, _ time.Time) error {
	return nil
}

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

// Compile-time assertion: *mockBlobStore satisfies BlobStore.
var _ BlobStore = (*mockBlobStore)(nil)

type mockObjectStore struct{}

func (m *mockObjectStore) Create(ctx context.Context, obj *KnowledgeObject) error { return nil }
func (m *mockObjectStore) Get(ctx context.Context, id string) (*KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore) GetByContentHash(ctx context.Context, hash string) (*KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore) GetBySourceKey(ctx context.Context, key string) (*KnowledgeObject, error) {
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
func (m *mockObjectStore) VectorSearch(ctx context.Context, q VectorQuery, filter ObjectFilter) ([]*KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore) FTSSearch(ctx context.Context, query string, filter ObjectFilter) ([]*KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore) FTSSearchNodeAware(_ context.Context, _ string, _ ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}
func (m *mockObjectStore) VectorSearchNodeAware(_ context.Context, _ VectorQuery, _ ObjectFilter, _ pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error) {
	return nil, nil
}
func (m *mockObjectStore) SetReminder(ctx context.Context, id string, at time.Time) error {
	return nil
}
func (m *mockObjectStore) ClearReminder(ctx context.Context, id string) error { return nil }
func (m *mockObjectStore) ListDueReminders(ctx context.Context, now time.Time) ([]*KnowledgeObject, error) {
	return nil, nil
}
func (m *mockObjectStore) MarkReminded(ctx context.Context, id string, now time.Time) error {
	return nil
}
func (m *mockObjectStore) ListPendingReminders(ctx context.Context) ([]*KnowledgeObject, error) {
	return nil, nil
}

type mockEntityStore struct{}

func (m *mockEntityStore) Upsert(ctx context.Context, entity *Entity) error     { return nil }
func (m *mockEntityStore) UpsertThin(ctx context.Context, entity *Entity) error { return nil }
func (m *mockEntityStore) SetContentStatus(ctx context.Context, slug string, status ContentStatus) error {
	return nil
}
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
func (m *mockEdgeStore) CountMentionsTo(ctx context.Context, toType, toID string) (int, error) {
	return 0, nil
}
func (m *mockEdgeStore) RelatedObjectIDs(ctx context.Context, objectID string, depth, limit int) ([]string, error) {
	return nil, nil
}

type mockJobStore struct{}

func (m *mockJobStore) Create(ctx context.Context, job *Job) error { return nil }
func (m *mockJobStore) Get(ctx context.Context, id string) (*Job, error) {
	return nil, nil
}
func (m *mockJobStore) GetByIdempotencyKey(ctx context.Context, key string) (*Job, error) {
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

func (m *mockJobStore) ExtendLease(ctx context.Context, id, claim string, ttl time.Duration) (bool, error) {
	return false, nil
}

func (m *mockJobStore) ReleaseLease(ctx context.Context, id, claim string) error { return nil }

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
func (m *mockRegistryStore) Delete(ctx context.Context, url string) error { return nil }

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
func (m *mockProximityStore) Put(_ context.Context, _ *ProximityScore) error        { return nil }
func (m *mockProximityStore) PutBatch(_ context.Context, _ []*ProximityScore) error { return nil }
func (m *mockProximityStore) Delete(_ context.Context, _ string) error              { return nil }
func (m *mockProximityStore) FindStale(_ context.Context, _ time.Time, _ int) ([]string, error) {
	return nil, nil
}
func (m *mockProximityStore) Stats(_ context.Context) (*ProximityStats, error) { return nil, nil }

// Compile-time assertion: *mockDriver satisfies StorageDriver.
var _ StorageDriver = (*mockDriver)(nil)

// Compile-time assertion: *mockObjectStore satisfies ObjectStore.
var _ ObjectStore = (*mockObjectStore)(nil)

// Compile-time assertion: *mockEntityStore satisfies EntityStore.
var _ EntityStore = (*mockEntityStore)(nil)

// Compile-time assertion: *mockEdgeStore satisfies EdgeStore.
var _ EdgeStore = (*mockEdgeStore)(nil)

// Compile-time assertion: *mockJobStore satisfies JobStore.
var _ JobStore = (*mockJobStore)(nil)

type mockWatchStore struct{}

func (m *mockWatchStore) CreateWatch(_ context.Context, _ *WatchConfig) error        { return nil }
func (m *mockWatchStore) GetWatch(_ context.Context, _ string) (*WatchConfig, error) { return nil, nil }
func (m *mockWatchStore) ListWatches(_ context.Context, _ string) ([]*WatchConfig, error) {
	return nil, nil
}
func (m *mockWatchStore) UpdateWatch(_ context.Context, _ *WatchConfig) error          { return nil }
func (m *mockWatchStore) DeleteWatch(_ context.Context, _ string) error                { return nil }
func (m *mockWatchStore) UpsertFileRecord(_ context.Context, _ *WatchFileRecord) error { return nil }
func (m *mockWatchStore) GetFileRecord(_ context.Context, _, _ string) (*WatchFileRecord, error) {
	return nil, nil
}
func (m *mockWatchStore) DeleteFileRecord(_ context.Context, _, _ string) error { return nil }
func (m *mockWatchStore) ListFileRecords(_ context.Context, _ string) ([]*WatchFileRecord, error) {
	return nil, nil
}

type mockAliasStore struct{}

func (m *mockAliasStore) Create(_ context.Context, _ *Alias) error               { return nil }
func (m *mockAliasStore) Resolve(_ context.Context, _, _ string) (string, error) { return "", nil }
func (m *mockAliasStore) List(_ context.Context, _ AliasFilter) ([]*Alias, error) {
	return nil, nil
}
func (m *mockAliasStore) Delete(_ context.Context, _, _, _ string) error { return nil }

type mockAuditLogStore struct{}

func (m *mockAuditLogStore) Append(_ context.Context, _ *AuditEntry) error { return nil }
func (m *mockAuditLogStore) List(_ context.Context, _ AuditFilter) ([]*AuditEntry, int, error) {
	return nil, 0, nil
}
func (m *mockAuditLogStore) GetObjectHistory(_ context.Context, _ string) ([]*AuditEntry, error) {
	return nil, nil
}

type mockAttachmentStore struct{}

func (m *mockAttachmentStore) SaveAttachment(_ context.Context, _, _, _ string, _ []byte) (string, error) {
	return "", nil
}
func (m *mockAttachmentStore) GetAttachment(_ context.Context, _ string) (*Attachment, error) {
	return nil, nil
}
func (m *mockAttachmentStore) ListAttachments(_ context.Context, _ string) ([]Attachment, error) {
	return nil, nil
}
func (m *mockAttachmentStore) DeleteAttachment(_ context.Context, _ string) error { return nil }

type mockResurfacingQueueStore struct{}

func (m *mockResurfacingQueueStore) Upsert(_ context.Context, _ *ResurfacingEntry) error {
	return nil
}
func (m *mockResurfacingQueueStore) List(
	_ context.Context, _ ResurfacingFilter,
) ([]*ResurfacingEntry, error) {
	return nil, nil
}
func (m *mockResurfacingQueueStore) MarkSurfaced(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (m *mockResurfacingQueueStore) Dismiss(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (m *mockResurfacingQueueStore) DeleteByProfile(_ context.Context, _ string) error { return nil }
func (m *mockResurfacingQueueStore) DeleteByObject(_ context.Context, _ string) error  { return nil }

type mockEntitlementStore struct{}

func (m *mockEntitlementStore) Upsert(_ context.Context, _ *RegistryEntitlement) error { return nil }
func (m *mockEntitlementStore) Get(_ context.Context, _ string) (*RegistryEntitlement, error) {
	return nil, nil
}
func (m *mockEntitlementStore) List(_ context.Context) ([]*RegistryEntitlement, error) {
	return nil, nil
}

type mockMeteringStore struct{}

func (m *mockMeteringStore) Record(_ context.Context, _ *MeteringEvent) error { return nil }
func (m *mockMeteringStore) RecordCapped(
	_ context.Context, _ *MeteringEvent, _ time.Time, _ int,
) (bool, error) {
	return true, nil
}
func (m *mockMeteringStore) Aggregate(
	_ context.Context, _ MeteringFilter,
) ([]*MeteringAggregate, error) {
	return nil, nil
}
func (m *mockMeteringStore) List(
	_ context.Context, _ MeteringFilter,
) ([]*MeteringEvent, error) {
	return nil, nil
}

type mockSavedSearchStore struct{}

func (m *mockSavedSearchStore) Create(_ context.Context, _ *SavedSearch) error { return nil }
func (m *mockSavedSearchStore) GetByName(_ context.Context, _ string) (*SavedSearch, error) {
	return nil, nil
}
func (m *mockSavedSearchStore) List(_ context.Context, _ SavedSearchFilter) ([]*SavedSearch, error) {
	return nil, nil
}
func (m *mockSavedSearchStore) Update(_ context.Context, _ *SavedSearch) error { return nil }
func (m *mockSavedSearchStore) Delete(_ context.Context, _ string) error       { return nil }

type mockSearchHistoryStore struct{}

func (m *mockSearchHistoryStore) Append(_ context.Context, _ *SearchHistoryEntry) error { return nil }
func (m *mockSearchHistoryStore) List(
	_ context.Context, _ SearchHistoryFilter,
) ([]*SearchHistoryEntry, error) {
	return nil, nil
}
func (m *mockSearchHistoryStore) ClearByProfile(_ context.Context, _ string) error { return nil }

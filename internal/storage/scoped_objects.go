package storage

import (
	"context"
	"io"
	"time"
)

// restrictedObjects is the ObjectStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedObjects struct{ inner ObjectStore }

var _ ObjectStore = restrictedObjects{}

func (v restrictedObjects) Create(ctx context.Context, obj *KnowledgeObject) error {
	return v.inner.Create(ctx, obj)
}

func (v restrictedObjects) Get(_ context.Context, _ string) (*KnowledgeObject, error) {
	return nil, restrictedRead("Objects", "Get")
}

func (v restrictedObjects) GetByContentHash(_ context.Context, _ string) (*KnowledgeObject, error) {
	return nil, restrictedRead("Objects", "GetByContentHash")
}

func (v restrictedObjects) GetBySourceKey(_ context.Context, _ string) (*KnowledgeObject, error) {
	return nil, restrictedRead("Objects", "GetBySourceKey")
}

func (v restrictedObjects) List(_ context.Context, _ ObjectFilter) ([]*KnowledgeObject, int, error) {
	return nil, 0, restrictedRead("Objects", "List")
}

func (v restrictedObjects) CountByType(_ context.Context) (map[string]int, error) {
	return nil, restrictedRead("Objects", "CountByType")
}

func (v restrictedObjects) Update(ctx context.Context, obj *KnowledgeObject) error {
	return v.inner.Update(ctx, obj)
}

func (v restrictedObjects) TransitionStatus(ctx context.Context, id string, from string, to string, at time.Time) error {
	return v.inner.TransitionStatus(ctx, id, from, to, at)
}

func (v restrictedObjects) Delete(ctx context.Context, id string) error {
	return v.inner.Delete(ctx, id)
}

func (v restrictedObjects) CountBlobRefs(_ context.Context, _ string) (int, error) {
	return 0, restrictedRead("Objects", "CountBlobRefs")
}

func (v restrictedObjects) ListBySQL(_ context.Context, _ string, _ []any, _ ObjectFilter) ([]*KnowledgeObject, int, error) {
	return nil, 0, restrictedRead("Objects", "ListBySQL")
}

func (v restrictedObjects) Reinforce(ctx context.Context, hash string, mergeData *KnowledgeObject) (string, error) {
	return v.inner.Reinforce(ctx, hash, mergeData)
}

func (v restrictedObjects) SetReminder(ctx context.Context, id string, at time.Time) error {
	return v.inner.SetReminder(ctx, id, at)
}

func (v restrictedObjects) ClearReminder(ctx context.Context, id string) error {
	return v.inner.ClearReminder(ctx, id)
}

func (v restrictedObjects) ListDueReminders(_ context.Context, _ time.Time) ([]*KnowledgeObject, error) {
	return nil, restrictedRead("Objects", "ListDueReminders")
}

func (v restrictedObjects) MarkReminded(ctx context.Context, id string, now time.Time) error {
	return v.inner.MarkReminded(ctx, id, now)
}

func (v restrictedObjects) ListPendingReminders(_ context.Context) ([]*KnowledgeObject, error) {
	return nil, restrictedRead("Objects", "ListPendingReminders")
}

func (v restrictedObjects) VectorSearch(_ context.Context, _ VectorQuery, _ ObjectFilter) ([]*KnowledgeObject, error) {
	return nil, restrictedRead("Objects", "VectorSearch")
}

func (v restrictedObjects) FTSSearch(_ context.Context, _ string, _ ObjectFilter) ([]*KnowledgeObject, error) {
	return nil, restrictedRead("Objects", "FTSSearch")
}

func (v restrictedObjects) FTSSearchNodeAware(_ context.Context, _ string, _ ObjectFilter, _ NodeAwareFilter) ([]*NodeAwareResult, error) {
	return nil, restrictedRead("Objects", "FTSSearchNodeAware")
}

func (v restrictedObjects) VectorSearchNodeAware(_ context.Context, _ VectorQuery, _ ObjectFilter, _ NodeAwareFilter) ([]*NodeAwareResult, error) {
	return nil, restrictedRead("Objects", "VectorSearchNodeAware")
}

// restrictedEntities is the EntityStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedEntities struct{ inner EntityStore }

var _ EntityStore = restrictedEntities{}

func (v restrictedEntities) Upsert(ctx context.Context, entity *Entity) error {
	return v.inner.Upsert(ctx, entity)
}

func (v restrictedEntities) UpsertThin(ctx context.Context, entity *Entity) error {
	return v.inner.UpsertThin(ctx, entity)
}

func (v restrictedEntities) SetContentStatus(ctx context.Context, slug string, status ContentStatus) error {
	return v.inner.SetContentStatus(ctx, slug, status)
}

func (v restrictedEntities) Get(_ context.Context, _ string) (*Entity, error) {
	return nil, restrictedRead("Entities", "Get")
}

func (v restrictedEntities) List(_ context.Context, _ EntityFilter) ([]*Entity, error) {
	return nil, restrictedRead("Entities", "List")
}

func (v restrictedEntities) Namespaces(_ context.Context) ([]string, error) {
	return nil, restrictedRead("Entities", "Namespaces")
}

func (v restrictedEntities) Resolve(_ context.Context, _ string) (*Entity, error) {
	return nil, restrictedRead("Entities", "Resolve")
}

// restrictedEdges is the EdgeStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedEdges struct{ inner EdgeStore }

var _ EdgeStore = restrictedEdges{}

func (v restrictedEdges) Create(ctx context.Context, edge *Edge) error {
	return v.inner.Create(ctx, edge)
}

func (v restrictedEdges) ListFrom(_ context.Context, _ string, _ string) ([]*Edge, error) {
	return nil, restrictedRead("Edges", "ListFrom")
}

func (v restrictedEdges) ListTo(_ context.Context, _ string, _ string) ([]*Edge, error) {
	return nil, restrictedRead("Edges", "ListTo")
}

func (v restrictedEdges) Delete(ctx context.Context, id string) error {
	return v.inner.Delete(ctx, id)
}

func (v restrictedEdges) DeleteByObject(ctx context.Context, objectID string) error {
	return v.inner.DeleteByObject(ctx, objectID)
}

func (v restrictedEdges) CountMentionsTo(_ context.Context, _ string, _ string) (int, error) {
	return 0, restrictedRead("Edges", "CountMentionsTo")
}

func (v restrictedEdges) RelatedObjectIDs(_ context.Context, _ string, _ int, _ int) ([]string, error) {
	return nil, restrictedRead("Edges", "RelatedObjectIDs")
}

func (v restrictedEdges) CreateAll(ctx context.Context, edges []*Edge) error {
	return v.inner.CreateAll(ctx, edges)
}

func (v restrictedEdges) CreateMissing(ctx context.Context, edges []*Edge) (int, error) {
	return v.inner.CreateMissing(ctx, edges)
}

func (v restrictedEdges) DeleteMatching(ctx context.Context, matches []EdgeMatch) (int, error) {
	return v.inner.DeleteMatching(ctx, matches)
}

// restrictedEmbeddings is the EmbeddingStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedEmbeddings struct{ inner EmbeddingStore }

var _ EmbeddingStore = restrictedEmbeddings{}

func (v restrictedEmbeddings) EnsureIndex(ctx context.Context, spec EmbeddingModelSpec) error {
	return v.inner.EnsureIndex(ctx, spec)
}

func (v restrictedEmbeddings) Put(ctx context.Context, objectID string, vectors []ObjectVector) error {
	return v.inner.Put(ctx, objectID, vectors)
}

func (v restrictedEmbeddings) Get(_ context.Context, _ string, _ string) ([]ObjectVector, error) {
	return nil, restrictedRead("Embeddings", "Get")
}

func (v restrictedEmbeddings) Search(_ context.Context, _ VectorQuery) ([]EmbeddingHit, error) {
	return nil, restrictedRead("Embeddings", "Search")
}

func (v restrictedEmbeddings) ListMissing(_ context.Context, _ string, _ string, _ int) ([]string, error) {
	return nil, restrictedRead("Embeddings", "ListMissing")
}

func (v restrictedEmbeddings) PurgeModel(ctx context.Context, modelID string) error {
	return v.inner.PurgeModel(ctx, modelID)
}

// restrictedProximity is the ProximityStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedProximity struct{ inner ProximityStore }

var _ ProximityStore = restrictedProximity{}

func (v restrictedProximity) GetNeighbors(_ context.Context, _ string, _ int) ([]*ProximityScore, error) {
	return nil, restrictedRead("Proximity", "GetNeighbors")
}

func (v restrictedProximity) GetNeighborsAbove(_ context.Context, _ string, _ float64) ([]*ProximityScore, error) {
	return nil, restrictedRead("Proximity", "GetNeighborsAbove")
}

func (v restrictedProximity) Get(_ context.Context, _ string, _ string) (*ProximityScore, error) {
	return nil, restrictedRead("Proximity", "Get")
}

func (v restrictedProximity) Put(ctx context.Context, score *ProximityScore) error {
	return v.inner.Put(ctx, score)
}

func (v restrictedProximity) PutBatch(ctx context.Context, scores []*ProximityScore) error {
	return v.inner.PutBatch(ctx, scores)
}

func (v restrictedProximity) Delete(ctx context.Context, objectID string) error {
	return v.inner.Delete(ctx, objectID)
}

func (v restrictedProximity) FindStale(_ context.Context, _ time.Time, _ int) ([]string, error) {
	return nil, restrictedRead("Proximity", "FindStale")
}

func (v restrictedProximity) Stats(_ context.Context) (*ProximityStats, error) {
	return nil, restrictedRead("Proximity", "Stats")
}

// restrictedAliases is the AliasStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedAliases struct{ inner AliasStore }

var _ AliasStore = restrictedAliases{}

func (v restrictedAliases) Create(ctx context.Context, a *Alias) error {
	return v.inner.Create(ctx, a)
}

func (v restrictedAliases) Resolve(_ context.Context, _ string, _ string) (string, error) {
	return "", restrictedRead("Aliases", "Resolve")
}

func (v restrictedAliases) List(_ context.Context, _ AliasFilter) ([]*Alias, error) {
	return nil, restrictedRead("Aliases", "List")
}

func (v restrictedAliases) Delete(ctx context.Context, alias string, scope string, profile string) error {
	return v.inner.Delete(ctx, alias, scope, profile)
}

// restrictedAttachments is the AttachmentStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedAttachments struct{ inner AttachmentStore }

var _ AttachmentStore = restrictedAttachments{}

func (v restrictedAttachments) SaveAttachment(ctx context.Context, objectID string, filename string, mimeType string, data []byte) (string, error) {
	return v.inner.SaveAttachment(ctx, objectID, filename, mimeType, data)
}

func (v restrictedAttachments) GetAttachment(_ context.Context, _ string) (*Attachment, error) {
	return nil, restrictedRead("Attachments", "GetAttachment")
}

func (v restrictedAttachments) ListAttachments(_ context.Context, _ string) ([]Attachment, error) {
	return nil, restrictedRead("Attachments", "ListAttachments")
}

func (v restrictedAttachments) DeleteAttachment(ctx context.Context, attachmentID string) error {
	return v.inner.DeleteAttachment(ctx, attachmentID)
}

// restrictedBlobs is the BlobStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedBlobs struct{ inner BlobStore }

var _ BlobStore = restrictedBlobs{}

func (v restrictedBlobs) Put(ctx context.Context, key string, data io.Reader, meta BlobMeta) error {
	return v.inner.Put(ctx, key, data, meta)
}

func (v restrictedBlobs) Get(_ context.Context, _ string) (io.ReadCloser, BlobMeta, error) {
	return nil, BlobMeta{}, restrictedRead("Blobs", "Get")
}

func (v restrictedBlobs) Delete(ctx context.Context, key string) error {
	return v.inner.Delete(ctx, key)
}

func (v restrictedBlobs) Exists(_ context.Context, _ string) (bool, error) {
	return false, restrictedRead("Blobs", "Exists")
}

func (v restrictedBlobs) List(_ context.Context, _ string) ([]BlobInfo, error) {
	return nil, restrictedRead("Blobs", "List")
}

func (v restrictedBlobs) URL(_ context.Context, _ string) (string, error) {
	return "", restrictedRead("Blobs", "URL")
}

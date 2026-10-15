package storage

import (
	"context"
	"fmt"
	"io"
	"time"
)

// restrictedObjects is the ObjectStore of a restricted viewer: the
// object reads answer through the driver's scoped store (visible rows
// only, a hidden row is ErrNotFound), the system reads refuse, every
// write reaches the driver. scoped is nil for a nil viewer or a driver
// whose object store cannot bind to a scope; its reads then refuse.
type restrictedObjects struct {
	inner  ObjectStore
	scoped ObjectStore
	// refuse is why the scoped reads refuse when scoped is nil.
	refuse error
}

var _ ObjectStore = restrictedObjects{}

func (v restrictedObjects) Create(ctx context.Context, obj *KnowledgeObject) error {
	return v.inner.Create(ctx, obj)
}

func (v restrictedObjects) Get(ctx context.Context, id string) (*KnowledgeObject, error) {
	if v.scoped == nil {
		return nil, refuseRead("Objects", "Get", v.refuse)
	}
	return v.scoped.Get(ctx, id)
}

func (v restrictedObjects) GetByContentHash(ctx context.Context, hash, profileID string) (*KnowledgeObject, error) {
	if v.scoped == nil {
		return nil, refuseRead("Objects", "GetByContentHash", v.refuse)
	}
	return v.scoped.GetByContentHash(ctx, hash, profileID)
}

func (v restrictedObjects) GetBySourceKey(ctx context.Context, key string) (*KnowledgeObject, error) {
	if v.scoped == nil {
		return nil, refuseRead("Objects", "GetBySourceKey", v.refuse)
	}
	return v.scoped.GetBySourceKey(ctx, key)
}

func (v restrictedObjects) List(ctx context.Context, filter ObjectFilter) ([]*KnowledgeObject, int, error) {
	if v.scoped == nil {
		return nil, 0, refuseRead("Objects", "List", v.refuse)
	}
	return v.scoped.List(ctx, filter)
}

func (v restrictedObjects) CountByType(ctx context.Context) (map[string]int, error) {
	if v.scoped == nil {
		return nil, refuseRead("Objects", "CountByType", v.refuse)
	}
	return v.scoped.CountByType(ctx)
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

func (v restrictedObjects) ListBySQL(ctx context.Context, where string, args []any, filter ObjectFilter) ([]*KnowledgeObject, int, error) {
	if v.scoped == nil {
		return nil, 0, refuseRead("Objects", "ListBySQL", v.refuse)
	}
	return v.scoped.ListBySQL(ctx, where, args, filter)
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

func (v restrictedObjects) VectorSearch(ctx context.Context, q VectorQuery, filter ObjectFilter) ([]*KnowledgeObject, error) {
	if v.scoped == nil {
		return nil, refuseRead("Objects", "VectorSearch", v.refuse)
	}
	return v.scoped.VectorSearch(ctx, q, filter)
}

func (v restrictedObjects) FTSSearch(ctx context.Context, query string, filter ObjectFilter) ([]*KnowledgeObject, error) {
	if v.scoped == nil {
		return nil, refuseRead("Objects", "FTSSearch", v.refuse)
	}
	return v.scoped.FTSSearch(ctx, query, filter)
}

func (v restrictedObjects) FTSSearchNodeAware(ctx context.Context, query string, filter ObjectFilter, naf NodeAwareFilter) ([]*NodeAwareResult, error) {
	if v.scoped == nil {
		return nil, refuseRead("Objects", "FTSSearchNodeAware", v.refuse)
	}
	return v.scoped.FTSSearchNodeAware(ctx, query, filter, naf)
}

func (v restrictedObjects) VectorSearchNodeAware(ctx context.Context, q VectorQuery, filter ObjectFilter, naf NodeAwareFilter) ([]*NodeAwareResult, error) {
	if v.scoped == nil {
		return nil, refuseRead("Objects", "VectorSearchNodeAware", v.refuse)
	}
	return v.scoped.VectorSearchNodeAware(ctx, q, filter, naf)
}

// restrictedEntities is the EntityStore of a restricted viewer: reads
// answer through the driver's scoped store (ADR-080 §6: a registry
// entity when the viewer may see entities, a placeholder while a
// visible object mentions it), every write reaches the driver.
type restrictedEntities struct {
	inner  EntityStore
	scoped EntityStore
	// refuse is why the scoped reads refuse when scoped is nil.
	refuse error
}

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

func (v restrictedEntities) Get(ctx context.Context, slug string) (*Entity, error) {
	if v.scoped == nil {
		return nil, refuseRead("Entities", "Get", v.refuse)
	}
	return v.scoped.Get(ctx, slug)
}

func (v restrictedEntities) List(ctx context.Context, filter EntityFilter) ([]*Entity, error) {
	if v.scoped == nil {
		return nil, refuseRead("Entities", "List", v.refuse)
	}
	return v.scoped.List(ctx, filter)
}

func (v restrictedEntities) Namespaces(ctx context.Context) ([]string, error) {
	if v.scoped == nil {
		return nil, refuseRead("Entities", "Namespaces", v.refuse)
	}
	return v.scoped.Namespaces(ctx)
}

func (v restrictedEntities) Resolve(ctx context.Context, mention string) (*Entity, error) {
	if v.scoped == nil {
		return nil, refuseRead("Entities", "Resolve", v.refuse)
	}
	return v.scoped.Resolve(ctx, mention)
}

// restrictedEdges is the EdgeStore of a restricted viewer: reads answer
// through the driver's scoped store (an edge is visible when every
// object it touches is), every write reaches the driver.
type restrictedEdges struct {
	inner  EdgeStore
	scoped EdgeStore
	// refuse is why the scoped reads refuse when scoped is nil.
	refuse error
}

var _ EdgeStore = restrictedEdges{}

func (v restrictedEdges) Create(ctx context.Context, edge *Edge) error {
	return v.inner.Create(ctx, edge)
}

func (v restrictedEdges) ListFrom(ctx context.Context, fromType, fromID string) ([]*Edge, error) {
	if v.scoped == nil {
		return nil, refuseRead("Edges", "ListFrom", v.refuse)
	}
	return v.scoped.ListFrom(ctx, fromType, fromID)
}

func (v restrictedEdges) ListTo(ctx context.Context, toType, toID string) ([]*Edge, error) {
	if v.scoped == nil {
		return nil, refuseRead("Edges", "ListTo", v.refuse)
	}
	return v.scoped.ListTo(ctx, toType, toID)
}

func (v restrictedEdges) Delete(ctx context.Context, id string) error {
	return v.inner.Delete(ctx, id)
}

func (v restrictedEdges) DeleteByObject(ctx context.Context, objectID string) error {
	return v.inner.DeleteByObject(ctx, objectID)
}

func (v restrictedEdges) CountMentionsTo(ctx context.Context, toType, toID string) (int, error) {
	if v.scoped == nil {
		return 0, refuseRead("Edges", "CountMentionsTo", v.refuse)
	}
	return v.scoped.CountMentionsTo(ctx, toType, toID)
}

func (v restrictedEdges) RelatedObjectIDs(ctx context.Context, objectID string, depth, limit int) ([]string, error) {
	if v.scoped == nil {
		return nil, refuseRead("Edges", "RelatedObjectIDs", v.refuse)
	}
	return v.scoped.RelatedObjectIDs(ctx, objectID, depth, limit)
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

// restrictedEmbeddings is the EmbeddingStore of a restricted viewer:
// Search answers through the driver's scoped store (the masked variant
// over visible objects, ADR-080 §7), Get and ListMissing are system
// reads that refuse, every write reaches the driver. It deliberately
// does not expose EmbeddingReader: stored vectors and their text never
// leave the server for a restricted viewer.
type restrictedEmbeddings struct {
	inner  EmbeddingStore
	scoped EmbeddingStore
	// refuse is why the scoped reads refuse when scoped is nil.
	refuse error
}

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

func (v restrictedEmbeddings) Search(ctx context.Context, q VectorQuery) ([]EmbeddingHit, error) {
	if v.scoped == nil {
		return nil, refuseRead("Embeddings", "Search", v.refuse)
	}
	return v.scoped.Search(ctx, q)
}

func (v restrictedEmbeddings) ListMissing(_ context.Context, _ string, _ string, _ int) ([]string, error) {
	return nil, restrictedRead("Embeddings", "ListMissing")
}

func (v restrictedEmbeddings) PurgeModel(ctx context.Context, modelID string) error {
	return v.inner.PurgeModel(ctx, modelID)
}

// restrictedProximity is the ProximityStore of a restricted viewer:
// neighbour reads answer through the driver's scoped store (both
// objects of a pair visible), the maintenance reads refuse, every write
// reaches the driver.
type restrictedProximity struct {
	inner  ProximityStore
	scoped ProximityStore
	// refuse is why the scoped reads refuse when scoped is nil.
	refuse error
}

var _ ProximityStore = restrictedProximity{}

func (v restrictedProximity) GetNeighbors(ctx context.Context, objectID string, limit int) ([]*ProximityScore, error) {
	if v.scoped == nil {
		return nil, refuseRead("Proximity", "GetNeighbors", v.refuse)
	}
	return v.scoped.GetNeighbors(ctx, objectID, limit)
}

func (v restrictedProximity) GetNeighborsAbove(ctx context.Context, objectID string, threshold float64) ([]*ProximityScore, error) {
	if v.scoped == nil {
		return nil, refuseRead("Proximity", "GetNeighborsAbove", v.refuse)
	}
	return v.scoped.GetNeighborsAbove(ctx, objectID, threshold)
}

func (v restrictedProximity) Get(ctx context.Context, objectA, objectB string) (*ProximityScore, error) {
	if v.scoped == nil {
		return nil, refuseRead("Proximity", "Get", v.refuse)
	}
	return v.scoped.Get(ctx, objectA, objectB)
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

// restrictedAliases is the AliasStore of a restricted viewer: reads
// answer through the driver's scoped store (an alias of a visible
// object only), every write reaches the driver.
type restrictedAliases struct {
	inner  AliasStore
	scoped AliasStore
	// refuse is why the scoped reads refuse when scoped is nil.
	refuse error
}

var _ AliasStore = restrictedAliases{}

func (v restrictedAliases) Create(ctx context.Context, a *Alias) error {
	return v.inner.Create(ctx, a)
}

func (v restrictedAliases) Resolve(ctx context.Context, alias, profile string) (string, error) {
	if v.scoped == nil {
		return "", refuseRead("Aliases", "Resolve", v.refuse)
	}
	return v.scoped.Resolve(ctx, alias, profile)
}

func (v restrictedAliases) List(ctx context.Context, filter AliasFilter) ([]*Alias, error) {
	if v.scoped == nil {
		return nil, refuseRead("Aliases", "List", v.refuse)
	}
	return v.scoped.List(ctx, filter)
}

func (v restrictedAliases) Delete(ctx context.Context, alias string, scope string, profile string) error {
	return v.inner.Delete(ctx, alias, scope, profile)
}

// restrictedAttachments is the AttachmentStore of a restricted viewer:
// every read fails closed with ErrRestrictedViewer (no route reads an
// attachment on its own; an object's attachment IDs come with its
// scoped Get), every write reaches the driver.
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

// restrictedBlobs is the BlobStore of a restricted viewer. A blob is
// visible only through an object that refers to it (ADR-080 §6:
// derived data inherits its source's visibility), so the point reads
// (Get, Exists, URL) and the prefix listing answer only keys a visible
// object refers to, counted through the driver's scoped object store
// (CountBlobRefs under the viewer's scope); any other key is answered
// as missing, ErrBlobNotFound and ErrNotFound alike, so a guessed key
// confirms nothing. Writes reach the driver. With no scope (a nil
// viewer, or a driver whose object store cannot bind) every read
// refuses.
type restrictedBlobs struct {
	inner BlobStore
	// refs is the driver's object store bound to the viewer's scope;
	// nil when there is none, and refuse says why.
	refs   ObjectStore
	refuse error
}

var _ BlobStore = restrictedBlobs{}

// visible reports whether a visible object refers to key.
func (v restrictedBlobs) visible(ctx context.Context, key string) (bool, error) {
	n, err := v.refs.CountBlobRefs(ctx, key)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// hiddenBlob is the error of a blob no visible object refers to: the
// same answer a missing key gets.
func hiddenBlob(key string) error {
	return fmt.Errorf("blob %s: %w: %w", key, ErrBlobNotFound, ErrNotFound)
}

func (v restrictedBlobs) Put(ctx context.Context, key string, data io.Reader, meta BlobMeta) error {
	return v.inner.Put(ctx, key, data, meta)
}

func (v restrictedBlobs) Get(ctx context.Context, key string) (io.ReadCloser, BlobMeta, error) {
	if v.refs == nil {
		return nil, BlobMeta{}, refuseRead("Blobs", "Get", v.refuse)
	}
	ok, err := v.visible(ctx, key)
	if err != nil {
		return nil, BlobMeta{}, err
	}
	if !ok {
		return nil, BlobMeta{}, hiddenBlob(key)
	}
	return v.inner.Get(ctx, key)
}

func (v restrictedBlobs) Delete(ctx context.Context, key string) error {
	return v.inner.Delete(ctx, key)
}

func (v restrictedBlobs) Exists(ctx context.Context, key string) (bool, error) {
	if v.refs == nil {
		return false, refuseRead("Blobs", "Exists", v.refuse)
	}
	ok, err := v.visible(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	return v.inner.Exists(ctx, key)
}

func (v restrictedBlobs) List(ctx context.Context, prefix string) ([]BlobInfo, error) {
	if v.refs == nil {
		return nil, refuseRead("Blobs", "List", v.refuse)
	}
	all, err := v.inner.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	out := make([]BlobInfo, 0, len(all))
	for _, b := range all {
		ok, err := v.visible(ctx, b.Key)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, b)
		}
	}
	return out, nil
}

func (v restrictedBlobs) URL(ctx context.Context, key string) (string, error) {
	if v.refs == nil {
		return "", refuseRead("Blobs", "URL", v.refuse)
	}
	ok, err := v.visible(ctx, key)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", hiddenBlob(key)
	}
	return v.inner.URL(ctx, key)
}

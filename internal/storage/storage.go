package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// ErrEntityDefinitionUnavailable is returned when a thin entity's full definition
// has not yet been pulled from the source registry.
var ErrEntityDefinitionUnavailable = errors.New("entity definition unavailable: pull required")

// Alias represents a human-readable name that resolves to a knowledge object ID.
// The canonical definition lives in pkg/pluginapi.
type Alias = pluginapi.Alias

// AliasFilter restricts alias listing results.
type AliasFilter = pluginapi.AliasFilter

// AliasStore persists and retrieves object aliases.
// The canonical definition lives in pkg/pluginapi.
type AliasStore = pluginapi.AliasStore

// StorageDriver is the top-level interface for all persistence operations.
type StorageDriver interface {
	Init(ctx context.Context) error
	Close(ctx context.Context) error
	Objects() ObjectStore
	Entities() EntityStore
	Edges() EdgeStore
	Jobs() JobStore
	Pipelines() PipelineStore
	Steps() StepStore
	Registries() RegistryStore
	Reminders() ReminderStore
	Feeds() FeedStore
	FeedItems() FeedItemStore
	Batches() BatchStore
	Detectors() DetectorStore
	Blobs() BlobStore
	Proximity() ProximityStore
	Watches() WatchStore
	Aliases() AliasStore
	AuditLog() AuditStore
	Attachments() AttachmentStore
	Resurfacing() ResurfacingQueueStore
	Entitlements() EntitlementStore
	Metering() MeteringStore
	Vectors() VectorStore
	SavedSearches() SavedSearchStore
	SearchHistory() SearchHistoryStore
	Watermarks() WatermarkStore
	Health(ctx context.Context) error
}

// WatermarkStore tracks per-federation last-synced timestamps for the
// federation_watermarks table (US-0319). One row per federation name.
// Missing rows are treated as the Unix epoch (never synced).
type WatermarkStore interface {
	// GetWatermark returns the last_synced_at for federationName.
	// Missing rows return time.Unix(0, 0).UTC() with nil error.
	GetWatermark(ctx context.Context, federationName string) (time.Time, error)
	// SetWatermark upserts last_synced_at for federationName. Idempotent.
	SetWatermark(ctx context.Context, federationName string, ts time.Time) error
}

// VectorStore persists and queries ANN (approximate nearest neighbour) embeddings.
type VectorStore interface {
	// Upsert inserts or replaces a vector for the given object ID.
	Upsert(ctx context.Context, id string, vector []float32) error
	// Search returns the top-K nearest neighbours by L2/cosine distance.
	Search(ctx context.Context, vector []float32, topK int) ([]VectorHit, error)
	// Delete removes the vector for the given object ID.
	Delete(ctx context.Context, id string) error
	// Count returns the total number of indexed vectors.
	Count(ctx context.Context) (int, error)
}

// MeteringStore persists and queries metering events for paid registry access.
type MeteringStore interface {
	// Record appends one metering event.
	Record(ctx context.Context, event *MeteringEvent) error
	// RecordCapped atomically appends event only when the summed count
	// for (event.RegistryName, event.EventType) since periodStart stays
	// below limit. Check and insert MUST happen as one storage-level
	// operation so concurrent recorders can never overshoot the cap.
	// Returns whether the event was recorded. A zero periodStart means
	// all-time.
	RecordCapped(ctx context.Context, event *MeteringEvent, periodStart time.Time, limit int) (bool, error)
	// Aggregate returns summed counts grouped by registry_name+event_type,
	// optionally filtered by the provided MeteringFilter.
	Aggregate(ctx context.Context, filter MeteringFilter) ([]*MeteringAggregate, error)
	// List returns raw events matching the filter.
	List(ctx context.Context, filter MeteringFilter) ([]*MeteringEvent, error)
}

// AuditEntry is one immutable record in the audit log.
type AuditEntry struct {
	ID        string         `json:"id"`
	EventType string         `json:"event_type"`
	ObjectID  string         `json:"object_id"`
	Actor     string         `json:"actor"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

// AuditFilter restricts audit log queries.
type AuditFilter struct {
	ObjectID   string
	EventType  string   // single event type (legacy)
	EventTypes []string // multiple event types (OR)
	Actor      string
	After      time.Time
	Before     time.Time
	Limit      int
	Offset     int
}

// AuditStore is an append-only store for audit log entries.
// There are intentionally no Update or Delete methods.
type AuditStore interface {
	// Append inserts a new entry. Returns error on failure; never modifies existing entries.
	Append(ctx context.Context, entry *AuditEntry) error
	// List returns entries matching the filter, ordered by created_at ascending.
	List(ctx context.Context, filter AuditFilter) ([]*AuditEntry, int, error)
	// GetObjectHistory returns all entries for a specific object, ordered by created_at ascending.
	GetObjectHistory(ctx context.Context, objectID string) ([]*AuditEntry, error)
}

// BlobStore manages external binary content.
type BlobStore interface {
	Put(ctx context.Context, key string, data io.Reader, meta BlobMeta) error
	Get(ctx context.Context, key string) (io.ReadCloser, BlobMeta, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	List(ctx context.Context, prefix string) ([]BlobInfo, error)
	URL(ctx context.Context, key string) (string, error)
}

// ObjectStore persists and retrieves knowledge objects.
type ObjectStore interface {
	Create(ctx context.Context, obj *KnowledgeObject) error
	Get(ctx context.Context, id string) (*KnowledgeObject, error)
	GetByContentHash(ctx context.Context, hash string) (*KnowledgeObject, error)
	GetBySourceKey(ctx context.Context, key string) (*KnowledgeObject, error)
	List(ctx context.Context, filter ObjectFilter) ([]*KnowledgeObject, int, error)
	Update(ctx context.Context, obj *KnowledgeObject) error
	Delete(ctx context.Context, id string) error
	ListBySQL(ctx context.Context, where string, args []any, limit, offset int) ([]*KnowledgeObject, int, error)
	Reinforce(ctx context.Context, hash string, mergeData *KnowledgeObject) (string, error)
	ListWithEmbeddings(ctx context.Context) ([]*KnowledgeObject, error)
	// ListWithoutEmbeddings returns objects that have no stored embedding blob.
	ListWithoutEmbeddings(ctx context.Context) ([]*KnowledgeObject, error)
	// SetReminder sets remind_at on the object identified by id.
	SetReminder(ctx context.Context, id string, at time.Time) error
	// ClearReminder removes remind_at (and reminded_at) from the object.
	ClearReminder(ctx context.Context, id string) error
	// ListDueReminders returns objects where remind_at <= now and reminded_at IS NULL.
	ListDueReminders(ctx context.Context, now time.Time) ([]*KnowledgeObject, error)
	// MarkReminded sets reminded_at to now for the given object.
	MarkReminded(ctx context.Context, id string, now time.Time) error
	// ListPendingReminders returns all objects with a non-null remind_at.
	ListPendingReminders(ctx context.Context) ([]*KnowledgeObject, error)
	// VectorSearch returns the top-K objects ranked by cosine similarity to
	// the given vector, optionally filtered by ObjectFilter fields.
	VectorSearch(ctx context.Context, vector []float32, filter ObjectFilter) ([]*KnowledgeObject, error)
	// FTSSearch queries the objects_fts FTS5 virtual table using SQLite FTS5 MATCH syntax.
	// Returns results ranked by FTS5 bm25 score, filtered by ObjectFilter.
	FTSSearch(ctx context.Context, query string, filter ObjectFilter) ([]*KnowledgeObject, error)
	// FTSSearchNodeAware runs FTS search and applies NodeAwareFilter post-query.
	// When naf.NodeTypes is non-empty, only objects with ALL listed node types are returned.
	FTSSearchNodeAware(ctx context.Context, query string, filter ObjectFilter, naf pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error)
	// VectorSearchNodeAware runs vector search and applies NodeAwareFilter post-query.
	// When naf.NodeTypes is non-empty, only objects with ALL listed node types are returned.
	VectorSearchNodeAware(ctx context.Context, vector []float32, filter ObjectFilter, naf pluginapi.NodeAwareFilter) ([]*pluginapi.NodeAwareResult, error)
}

// EntityStore persists and retrieves named entities.
type EntityStore interface {
	Upsert(ctx context.Context, entity *Entity) error
	// UpsertThin stores an index-only stub (slug, title, namespace, version_hash, registry_url).
	// Sets content_status = 'thin'. Does NOT overwrite a 'full' record.
	UpsertThin(ctx context.Context, entity *Entity) error
	// SetContentStatus updates the content_status column for a single entity.
	SetContentStatus(ctx context.Context, slug string, status ContentStatus) error
	Get(ctx context.Context, slug string) (*Entity, error)
	List(ctx context.Context, filter EntityFilter) ([]*Entity, error)
	Resolve(ctx context.Context, mention string) (*Entity, error)
}

// EdgeStore persists and retrieves edges between nodes (ADR-049).
type EdgeStore interface {
	Create(ctx context.Context, edge *Edge) error
	ListFrom(ctx context.Context, fromType, fromID string) ([]*Edge, error)
	ListTo(ctx context.Context, toType, toID string) ([]*Edge, error)
	Delete(ctx context.Context, id string) error
	DeleteByObject(ctx context.Context, objectID string) error
	// CountMentionsTo returns the number of object→entity edges pointing at toID.
	CountMentionsTo(ctx context.Context, toType, toID string) (int, error)
	// RelatedObjectIDs returns object IDs reachable from objectID by traversing
	// shared mention targets up to depth hops. depth=1 means objects that share
	// at least one mention target with objectID; depth=2 extends one more hop.
	// The seed objectID is never included in the result.
	RelatedObjectIDs(ctx context.Context, objectID string, depth, limit int) ([]string, error)
}

// JobStore persists and retrieves jobs.
type JobStore interface {
	Create(ctx context.Context, job *Job) error
	Get(ctx context.Context, id string) (*Job, error)
	List(ctx context.Context, filter JobFilter) ([]*Job, int, error)
	AcquireNext(ctx context.Context) (*Job, error)
	Complete(ctx context.Context, id string, resultID string) error
	Fail(ctx context.Context, id string, errMsg string) error
	Retry(ctx context.Context, id string) error
	Cancel(ctx context.Context, id string) error
	RecoverStale(ctx context.Context, timeout int64) (int, error)
}

// PipelineStore persists and retrieves pipelines.
type PipelineStore interface {
	Create(ctx context.Context, pipeline *Pipeline) error
	Get(ctx context.Context, name string) (*Pipeline, error)
	List(ctx context.Context, filter PipelineFilter) ([]*Pipeline, int, error)
	Update(ctx context.Context, pipeline *Pipeline) error
	Delete(ctx context.Context, name string) error
	Archive(ctx context.Context, name string) error
	Unarchive(ctx context.Context, name string) error
}

// StepStore persists and retrieves registered steps.
type StepStore interface {
	Create(ctx context.Context, step *RegisteredStep) error
	Get(ctx context.Context, name string) (*RegisteredStep, error)
	List(ctx context.Context, source string) ([]*RegisteredStep, int, error)
	Unregister(ctx context.Context, name string) error
	Update(ctx context.Context, step *RegisteredStep) error
}

// RegistryStore caches registry manifests.
type RegistryStore interface {
	CacheManifest(ctx context.Context, cache *RegistryCache) error
	GetCachedManifest(ctx context.Context, url string) (*RegistryCache, error)
	UpdateETag(ctx context.Context, url, etag string) error
	List(ctx context.Context) ([]*RegistryCache, int, error)
	Delete(ctx context.Context, url string) error
}

// EntitlementStore persists and retrieves registry entitlement records.
type EntitlementStore interface {
	Upsert(ctx context.Context, e *RegistryEntitlement) error
	Get(ctx context.Context, registryName string) (*RegistryEntitlement, error)
	List(ctx context.Context) ([]*RegistryEntitlement, error)
}

// ReminderStore persists and retrieves system reminders.
type ReminderStore interface {
	Create(ctx context.Context, reminder *SystemReminder) error
	Get(ctx context.Context, id string) (*SystemReminder, error)
	List(ctx context.Context, activeOnly bool) ([]*SystemReminder, int, error)
	Dismiss(ctx context.Context, id string) error
}

// FeedStore persists and retrieves feed subscriptions.
type FeedStore interface {
	Create(ctx context.Context, feed *Feed) error
	Get(ctx context.Context, id string) (*Feed, error)
	GetByURL(ctx context.Context, url string) (*Feed, error)
	List(ctx context.Context, filter FeedFilter) ([]*Feed, error)
	Update(ctx context.Context, feed *Feed) error
	Delete(ctx context.Context, id string) error
}

// FeedItemStore persists and retrieves feed items.
type FeedItemStore interface {
	Create(ctx context.Context, item *FeedItem) error
	ExistsByGUID(ctx context.Context, feedID, guid string) (bool, error)
}

// BatchStore persists and retrieves batch import operations.
type BatchStore interface {
	Create(ctx context.Context, batch *Batch) error
	Get(ctx context.Context, id string) (*Batch, error)
	Update(ctx context.Context, batch *Batch) error
}

// DetectorStore persists and retrieves detector configurations.
type DetectorStore interface {
	Create(ctx context.Context, d *DetectorRecord) error
	Get(ctx context.Context, id string) (*DetectorRecord, error)
	List(ctx context.Context, filter DetectorFilter) ([]*DetectorRecord, int, error)
	Update(ctx context.Context, d *DetectorRecord) error
	Delete(ctx context.Context, id string) error
	Enable(ctx context.Context, id string) error
	Disable(ctx context.Context, id string) error
}

// ProximityStore persists and retrieves precomputed object proximity scores.
type ProximityStore interface {
	// GetNeighbors returns up to limit neighbors for objectID, sorted by score descending.
	GetNeighbors(ctx context.Context, objectID string, limit int) ([]*ProximityScore, error)

	// GetNeighborsAbove returns all neighbors whose score is >= threshold.
	GetNeighborsAbove(ctx context.Context, objectID string, threshold float64) ([]*ProximityScore, error)

	// Get returns the proximity score for a specific pair. objectA and objectB
	// are normalised to canonical (a < b) order internally.
	Get(ctx context.Context, objectA, objectB string) (*ProximityScore, error)

	// Put stores or updates a single proximity score.
	Put(ctx context.Context, score *ProximityScore) error

	// PutBatch stores or updates multiple proximity scores in a single transaction.
	PutBatch(ctx context.Context, scores []*ProximityScore) error

	// Delete removes all proximity records that involve objectID.
	Delete(ctx context.Context, objectID string) error

	// FindStale returns up to limit object IDs whose proximity was last computed
	// before cutoff, ordered by computed_at ascending.
	FindStale(ctx context.Context, cutoff time.Time, limit int) ([]string, error)

	// Stats returns aggregate statistics about the proximity index.
	Stats(ctx context.Context) (*ProximityStats, error)
}

// WatchStore persists and retrieves filesystem watch configurations and file records.
type WatchStore interface {
	CreateWatch(ctx context.Context, w *WatchConfig) error
	GetWatch(ctx context.Context, id string) (*WatchConfig, error)
	ListWatches(ctx context.Context, status string) ([]*WatchConfig, error)
	UpdateWatch(ctx context.Context, w *WatchConfig) error
	DeleteWatch(ctx context.Context, id string) error

	UpsertFileRecord(ctx context.Context, r *WatchFileRecord) error
	GetFileRecord(ctx context.Context, watchID, filePath string) (*WatchFileRecord, error)
	DeleteFileRecord(ctx context.Context, watchID, filePath string) error
	ListFileRecords(ctx context.Context, watchID string) ([]*WatchFileRecord, error)
}

// Attachment holds binary content (e.g. images, PDFs) linked to a KnowledgeObject.
type Attachment struct {
	ID        string    `json:"id"`
	ObjectID  string    `json:"object_id"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	Data      []byte    `json:"data,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// AttachmentStore persists and retrieves binary attachments linked to knowledge objects.
type AttachmentStore interface {
	// SaveAttachment stores binary data and returns the new attachment ID.
	SaveAttachment(ctx context.Context, objectID, filename, mimeType string, data []byte) (string, error)
	// GetAttachment retrieves an attachment by its ID, including binary data.
	GetAttachment(ctx context.Context, attachmentID string) (*Attachment, error)
	// ListAttachments returns all attachments for a knowledge object (data omitted).
	ListAttachments(ctx context.Context, objectID string) ([]Attachment, error)
	// DeleteAttachment removes an attachment by ID.
	DeleteAttachment(ctx context.Context, attachmentID string) error
}

// ResurfacingEntry is one candidate in the resurfacing queue.
type ResurfacingEntry struct {
	ID          string     `json:"id"`
	ObjectID    string     `json:"object_id"`
	ProfileID   string     `json:"profile_id"`
	Score       float64    `json:"score"`
	Reason      string     `json:"reason"`
	SurfacedAt  *time.Time `json:"surfaced_at,omitempty"`
	DismissedAt *time.Time `json:"dismissed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ResurfacingFilter restricts resurfacing queue queries.
type ResurfacingFilter struct {
	ProfileID  string
	UnseenOnly bool // surfaced_at IS NULL AND dismissed_at IS NULL
	MinScore   float64
	Limit      int
}

// ResurfacingQueueStore manages the resurfacing candidate queue.
type ResurfacingQueueStore interface {
	// Upsert inserts or replaces an entry (keyed on object_id + profile_id).
	Upsert(ctx context.Context, e *ResurfacingEntry) error
	// List returns entries matching the filter, ordered by score desc.
	List(ctx context.Context, f ResurfacingFilter) ([]*ResurfacingEntry, error)
	// MarkSurfaced sets surfaced_at = now for the given entry id.
	MarkSurfaced(ctx context.Context, id string, now time.Time) error
	// Dismiss sets dismissed_at = now for the given entry id.
	Dismiss(ctx context.Context, id string, now time.Time) error
	// DeleteByProfile removes all entries for a profile.
	DeleteByProfile(ctx context.Context, profileID string) error
	// DeleteByObject removes all entries for an object.
	DeleteByObject(ctx context.Context, objectID string) error
}

// SavedSearchStore persists and retrieves named saved searches (US-0054).
type SavedSearchStore interface {
	// Create inserts a new saved search. Returns error if name already exists.
	Create(ctx context.Context, s *SavedSearch) error
	// GetByName returns a saved search by its unique name, or nil if not found.
	GetByName(ctx context.Context, name string) (*SavedSearch, error)
	// List returns saved searches matching the filter, ordered by created_at desc.
	List(ctx context.Context, f SavedSearchFilter) ([]*SavedSearch, error)
	// Update persists field changes to an existing saved search.
	Update(ctx context.Context, s *SavedSearch) error
	// Delete removes a saved search by name.
	Delete(ctx context.Context, name string) error
}

// SearchHistoryStore persists and retrieves the search query log (US-0055).
type SearchHistoryStore interface {
	// Append records one search query execution.
	Append(ctx context.Context, e *SearchHistoryEntry) error
	// List returns history entries matching the filter, ordered by searched_at desc.
	List(ctx context.Context, f SearchHistoryFilter) ([]*SearchHistoryEntry, error)
	// ClearByProfile removes all history entries for a profile.
	ClearByProfile(ctx context.Context, profileID string) error
}

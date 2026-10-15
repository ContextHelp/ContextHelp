// Package storage defines the backend-neutral persistence contracts that
// the sqlite and postgres drivers implement.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrEntityDefinitionUnavailable is returned when a thin entity's full definition
// has not yet been pulled from the source registry.
var ErrEntityDefinitionUnavailable = errors.New("entity definition unavailable: pull required")

// ErrCapabilityMissing is returned when a driver opens a store whose underlying
// engine lacks a capability the schema requires (for example an SQLite build
// without FTS5 or the sqlite-vec extension). Errors wrapping this sentinel name
// the missing capability and the remedy, so callers such as config validation
// and doctor can report a clear diagnosis instead of a mid-migration SQL error.
var ErrCapabilityMissing = errors.New("storage capability missing")

// ErrNotFound is the cross-backend sentinel for "the row you asked for
// does not exist". Backends return errors wrapping it so callers can
// tell a missing record from a failed query without matching on message
// text — the CLI needs that distinction to answer a missing ref with a
// NOT_FOUND envelope and a "try ctxt find" hint, rather than the
// uncharacterized GENERIC it owes a real storage failure.
//
// Lives here, beside the other cross-backend sentinels, rather than in
// each driver: sqlite and postgres each had their own unexported
// not-found error, so the same condition was unmatchable from outside
// the driver and unequal between drivers.
var ErrNotFound = errors.New("not found")

// ErrBlobNotFound is the cross-backend sentinel for "the blob store
// holds no blob under this key". BlobStore.Get returns an error wrapping
// it for a missing key, and only then: a store it cannot reach (network,
// timeout, 5xx, permissions) is some other error, so callers can tell a
// blob that is gone from one they could not read this time.
var ErrBlobNotFound = errors.New("blob not found")

// ErrNotImplemented is returned by a store a backend does not implement
// yet, so callers can tell a missing capability from a failed query.
var ErrNotImplemented = errors.New("not implemented by this storage backend")

// Alias represents a human-readable name that resolves to a knowledge object ID.
type Alias struct {
	Alias     string    `json:"alias"`
	ObjectID  string    `json:"object_id"`
	Scope     string    `json:"scope"`   // "global" | "profile"
	Profile   string    `json:"profile"` // empty for global
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AliasFilter restricts alias listing results.
type AliasFilter struct {
	ObjectID string
	Scope    string
	Profile  string
}

// AliasStore is the persistence interface for aliases.
type AliasStore interface {
	Create(ctx context.Context, a *Alias) error
	Resolve(ctx context.Context, alias, profile string) (string, error)
	List(ctx context.Context, filter AliasFilter) ([]*Alias, error)
	Delete(ctx context.Context, alias, scope, profile string) error
}

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
	PendingEntities() PendingEntityStore
	Metering() MeteringStore
	Embeddings() EmbeddingStore
	SavedSearches() SavedSearchStore
	SearchHistory() SearchHistoryStore
	Watermarks() WatermarkStore
	UISessions() UISessionStore
	// Auth is the principal registry (ADR-079): principals, their
	// identities, sign-in tickets and the API key records.
	Auth() AuthStore
	// Sharing is the sharing registry (ADR-080): audiences, share
	// policies, grants, ceilings, key bindings and the read audit.
	Sharing() SharingStore
	Health(ctx context.Context) error
}

// WatermarkStore keeps, per federation target, the ChangeSeq of the
// last object the target acknowledged (federation_watermarks, one row
// per federation name). Federation pushes only objects with a greater
// ChangeSeq.
type WatermarkStore interface {
	// GetWatermark returns the ChangeSeq stored for federationName; 0,
	// which precedes every object, when none is.
	GetWatermark(ctx context.Context, federationName string) (int64, error)
	// SetWatermark stores seq for federationName, replacing any
	// previous one.
	SetWatermark(ctx context.Context, federationName string, seq int64) error
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
	// Get opens the blob under key. A missing key is an error wrapping
	// ErrBlobNotFound.
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
	// GetByContentHash returns the object of profileID whose content
	// hash is hash, or (nil, nil) when there is none. Content-hash
	// dedup stays inside a profile (ADR-080 §4): the same content under
	// another profile is another object.
	GetByContentHash(ctx context.Context, hash, profileID string) (*KnowledgeObject, error)
	GetBySourceKey(ctx context.Context, key string) (*KnowledgeObject, error)
	List(ctx context.Context, filter ObjectFilter) ([]*KnowledgeObject, int, error)
	// CountByType counts every stored object, whatever its status, by
	// type. Types with no object are absent.
	CountByType(ctx context.Context) (map[string]int, error)
	Update(ctx context.Context, obj *KnowledgeObject) error
	// TransitionStatus moves object id from status from to status to and
	// sets updated_at to at, in one conditional write: nothing changes
	// unless the object is in from when the write lands, so two callers
	// racing on the same object cannot both move it. A missing object,
	// or one in any other status, wraps ErrNotFound.
	TransitionStatus(ctx context.Context, id, from, to string, at time.Time) error
	// Delete removes the object's row and the rows the database ties to
	// it (its attachments, its index entries). A hard delete goes
	// through blob.DeleteObject, which also deletes its edges and the
	// blob its content is externalized to.
	Delete(ctx context.Context, id string) error
	// CountBlobRefs counts the objects that refer to blob key: their
	// raw_content or text_content is its blob:// ref, or metadata.blob_key
	// names it. Keys are content hashes, so objects can share one.
	CountBlobRefs(ctx context.Context, key string) (int, error)
	// ListBySQL lists the objects List(filter) would return that also
	// satisfy where, a raw predicate over the objects table in the
	// driver's dialect whose values are bound in args (placeholders
	// numbered from 1; the driver's own predicates come after). Status,
	// profile, paging and order come from filter exactly as for List, so
	// a query-language search never reaches an object a List would not.
	ListBySQL(ctx context.Context, where string, args []any, filter ObjectFilter) ([]*KnowledgeObject, int, error)
	// Reinforce merges mergeData's tags, mentions and content into the
	// object of mergeData.ProfileID whose content hash is hash, bumps
	// its reinforcement count and returns its ID. No such object in
	// that profile is an error wrapping sql.ErrNoRows: the same content
	// under another profile is never merged into (ADR-080 §4).
	Reinforce(ctx context.Context, hash string, mergeData *KnowledgeObject) (string, error)
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
	// VectorSearch returns objects ranked by q.ModelID's index (cosine
	// distance, closest first), filtered by filter (its StatusScope
	// included) and capped at filter.Limit. Each result carries
	// Metadata["score"] = 1 - distance. Fails with
	// ErrEmbeddingIndexMissing when the model has no index.
	VectorSearch(ctx context.Context, q VectorQuery, filter ObjectFilter) ([]*KnowledgeObject, error)
	// FTSSearch runs full-text search over the driver's index for raw
	// query text and returns matches best first, filtered by filter (its
	// StatusScope included). Each result carries Metadata["fts_score"].
	FTSSearch(ctx context.Context, query string, filter ObjectFilter) ([]*KnowledgeObject, error)
	// FTSSearchNodeAware runs FTS search and applies NodeAwareFilter post-query.
	// When naf.NodeTypes is non-empty, only objects with ALL listed node types are returned.
	FTSSearchNodeAware(ctx context.Context, query string, filter ObjectFilter, naf NodeAwareFilter) ([]*NodeAwareResult, error)
	// VectorSearchNodeAware runs vector search and applies NodeAwareFilter post-query.
	// When naf.NodeTypes is non-empty, only objects with ALL listed node types are returned.
	VectorSearchNodeAware(ctx context.Context, q VectorQuery, filter ObjectFilter, naf NodeAwareFilter) ([]*NodeAwareResult, error)
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
	// Namespaces returns each distinct namespace of the stored entities
	// once, ascending, the empty namespace included.
	Namespaces(ctx context.Context) ([]string, error)
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
	// CreateAll stores every edge in one transaction: all of them or,
	// on any error, none. A typed link and its inverse go in this way.
	CreateAll(ctx context.Context, edges []*Edge) error
	// CreateMissing stores, in one transaction, every edge in edges that
	// no stored edge matches on (from, to, edge type), and returns how
	// many it stored. An edge the call has just stored counts as stored,
	// so a repeat within edges goes in once. The stored edge of a matched
	// pair keeps its ID and metadata. The mention-edge writers share it.
	CreateMissing(ctx context.Context, edges []*Edge) (int, error)
	// DeleteMatching deletes, in one transaction, every edge a match
	// selects and returns how many it deleted. A match with no edge
	// types is an error, and nothing is deleted.
	DeleteMatching(ctx context.Context, matches []EdgeMatch) (int, error)
}

// EdgeMatch selects the edges running from one node to another whose
// type is one of EdgeTypes.
type EdgeMatch struct {
	FromType, FromID string
	ToType, ToID     string
	EdgeTypes        []string
}

// JobStore persists and retrieves jobs.
type JobStore interface {
	Create(ctx context.Context, job *Job) error
	Get(ctx context.Context, id string) (*Job, error)
	// GetByIdempotencyKey returns the job carrying the given non-empty
	// idempotency key, or (nil, nil) when no such job exists. An empty
	// key never matches.
	GetByIdempotencyKey(ctx context.Context, key string) (*Job, error)
	List(ctx context.Context, filter JobFilter) ([]*Job, int, error)
	AcquireNext(ctx context.Context) (*Job, error)
	Complete(ctx context.Context, id string, resultID string) error
	Fail(ctx context.Context, id string, errMsg string) error
	Retry(ctx context.Context, id string) error
	Cancel(ctx context.Context, id string) error
	// RecoverStale requeues running jobs that are no longer being run:
	// a leased job once its lease has expired, an unleased job once its
	// started_at is timeout seconds old (0 = every unleased running job,
	// the startup crash recovery). Requeueing clears claim and lease.
	RecoverStale(ctx context.Context, timeout int64) (int, error)
	// ExtendLease sets the lease of running job id, claimed with claim,
	// to expire ttl from now. It reports false, without error, when the
	// job is no longer running under that claim (settled, cancelled, or
	// requeued and claimed again). A live lease keeps RecoverStale off
	// the job, so only its holder runs it.
	ExtendLease(ctx context.Context, id, claim string, ttl time.Duration) (bool, error)
	// ReleaseLease drops the lease of running job id held under claim,
	// leaving the job running and unleased for RecoverStale.
	ReleaseLease(ctx context.Context, id, claim string) error
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

// EntitlementStore persists and retrieves entitlement records, keyed by
// (kind, name): an outbound row for a registry and an inbound grant for
// a principal of the same name are separate rows.
type EntitlementStore interface {
	// Upsert inserts or replaces the row keyed by (e.Kind,
	// e.RegistryName). An invalid Kind is refused.
	Upsert(ctx context.Context, e *RegistryEntitlement) error
	// Get returns the row keyed by (kind, name).
	Get(ctx context.Context, kind EntitlementKind, name string) (*RegistryEntitlement, error)
	// List returns every row of kind, ordered by name.
	List(ctx context.Context, kind EntitlementKind) ([]*RegistryEntitlement, error)
	// Delete removes the row keyed by (kind, name). Returns an error
	// wrapping ErrNotFound when there is no such row.
	Delete(ctx context.Context, kind EntitlementKind, name string) error
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
	ID        string  `json:"id"`
	ObjectID  string  `json:"object_id"`
	ProfileID string  `json:"profile_id"`
	Score     float64 `json:"score"`
	Reason    string  `json:"reason"`
	// OwnerPrincipal is the principal the candidate is queued for; empty
	// on write means OwnerPrincipal (the instance owner).
	OwnerPrincipal string     `json:"owner_principal"`
	SurfacedAt     *time.Time `json:"surfaced_at,omitempty"`
	DismissedAt    *time.Time `json:"dismissed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ResurfacingFilter restricts resurfacing queue queries.
type ResurfacingFilter struct {
	ProfileID string
	// OwnerPrincipal, when set, keeps that principal's candidates only.
	OwnerPrincipal string
	UnseenOnly     bool // surfaced_at IS NULL AND dismissed_at IS NULL
	MinScore       float64
	Limit          int
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

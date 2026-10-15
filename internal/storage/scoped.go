package storage

import (
	"errors"
	"fmt"
)

// Viewer is what ScopedDriver needs to know about the caller: whether
// it bypasses visibility entirely. sharing.Viewer satisfies it; the
// interface lives here so storage never imports sharing, which will
// import the search compiler (and so storage) once viewers carry a
// compiled predicate (ADR-080 §4). A nil Viewer is the most restrictive
// caller, like the zero sharing.Viewer.
type Viewer interface {
	IsUnrestricted() bool
}

// ErrRestrictedViewer is returned by every read of a ScopedDriver whose
// viewer is not unrestricted. Restricted viewers have no storage path
// yet: until the per-viewer predicate lands, a read on their behalf
// fails closed instead of answering with unscoped rows.
var ErrRestrictedViewer = errors.New("restricted viewer: scoped reads not supported")

// restrictedRead is the error a read method returns for a restricted
// viewer, naming the store and method so a fail-closed response can be
// traced to the read that refused.
func restrictedRead(store, method string) error {
	return fmt.Errorf("%s.%s: %w", store, method, ErrRestrictedViewer)
}

// ScopedDriver returns d as seen by v. An unrestricted viewer gets d
// itself: the owner's reads cost nothing and keep every optional
// capability the driver exposes. Any other viewer, nil included, gets
// a driver whose every read fails closed with ErrRestrictedViewer and
// whose writes reach d unchanged.
//
// Request handlers reach storage only through this function, via the
// service's Store(ctx); the unscoped driver is named with System by the
// pipeline, jobs and housekeeping. Every store accessor of StorageDriver
// is wrapped (scopedAccessors); an accessor added to the interface is
// promoted from the embedded driver and caught by the package's tests
// until it is classified there.
func ScopedDriver(v Viewer, d StorageDriver) StorageDriver {
	if d == nil {
		return nil
	}
	if v != nil && v.IsUnrestricted() {
		return d
	}
	return scopedDriver{StorageDriver: d}
}

// scopedDriver is a restricted viewer's StorageDriver. The embedded
// driver answers the methods that are not data reads (Init, Close,
// Health) and any accessor this package has not classified yet; every
// store accessor below returns the restricted wrapper of the driver's
// store.
type scopedDriver struct {
	StorageDriver
}

func (s scopedDriver) Objects() ObjectStore {
	return restrictedObjects{inner: s.StorageDriver.Objects()}
}
func (s scopedDriver) Entities() EntityStore {
	return restrictedEntities{inner: s.StorageDriver.Entities()}
}
func (s scopedDriver) Edges() EdgeStore { return restrictedEdges{inner: s.StorageDriver.Edges()} }
func (s scopedDriver) Jobs() JobStore   { return restrictedJobs{inner: s.StorageDriver.Jobs()} }
func (s scopedDriver) Pipelines() PipelineStore {
	return restrictedPipelines{inner: s.StorageDriver.Pipelines()}
}
func (s scopedDriver) Steps() StepStore { return restrictedSteps{inner: s.StorageDriver.Steps()} }
func (s scopedDriver) Registries() RegistryStore {
	return restrictedRegistries{inner: s.StorageDriver.Registries()}
}
func (s scopedDriver) Reminders() ReminderStore {
	return restrictedReminders{inner: s.StorageDriver.Reminders()}
}
func (s scopedDriver) Feeds() FeedStore { return restrictedFeeds{inner: s.StorageDriver.Feeds()} }
func (s scopedDriver) FeedItems() FeedItemStore {
	return restrictedFeedItems{inner: s.StorageDriver.FeedItems()}
}
func (s scopedDriver) Batches() BatchStore {
	return restrictedBatches{inner: s.StorageDriver.Batches()}
}
func (s scopedDriver) Detectors() DetectorStore {
	return restrictedDetectors{inner: s.StorageDriver.Detectors()}
}
func (s scopedDriver) Blobs() BlobStore { return restrictedBlobs{inner: s.StorageDriver.Blobs()} }
func (s scopedDriver) Proximity() ProximityStore {
	return restrictedProximity{inner: s.StorageDriver.Proximity()}
}
func (s scopedDriver) Watches() WatchStore {
	return restrictedWatches{inner: s.StorageDriver.Watches()}
}
func (s scopedDriver) Aliases() AliasStore {
	return restrictedAliases{inner: s.StorageDriver.Aliases()}
}
func (s scopedDriver) AuditLog() AuditStore {
	return restrictedAuditLog{inner: s.StorageDriver.AuditLog()}
}
func (s scopedDriver) Attachments() AttachmentStore {
	return restrictedAttachments{inner: s.StorageDriver.Attachments()}
}
func (s scopedDriver) Resurfacing() ResurfacingQueueStore {
	return restrictedResurfacing{inner: s.StorageDriver.Resurfacing()}
}
func (s scopedDriver) Entitlements() EntitlementStore {
	return restrictedEntitlements{inner: s.StorageDriver.Entitlements()}
}
func (s scopedDriver) PendingEntities() PendingEntityStore {
	return restrictedPendingEntities{inner: s.StorageDriver.PendingEntities()}
}
func (s scopedDriver) Metering() MeteringStore {
	return restrictedMetering{inner: s.StorageDriver.Metering()}
}
func (s scopedDriver) Embeddings() EmbeddingStore {
	return restrictedEmbeddings{inner: s.StorageDriver.Embeddings()}
}
func (s scopedDriver) SavedSearches() SavedSearchStore {
	return restrictedSavedSearches{inner: s.StorageDriver.SavedSearches()}
}
func (s scopedDriver) SearchHistory() SearchHistoryStore {
	return restrictedSearchHistory{inner: s.StorageDriver.SearchHistory()}
}
func (s scopedDriver) Watermarks() WatermarkStore {
	return restrictedWatermarks{inner: s.StorageDriver.Watermarks()}
}
func (s scopedDriver) UISessions() UISessionStore {
	return restrictedUISessions{inner: s.StorageDriver.UISessions()}
}

// accessorKind says what ScopedDriver does with one StorageDriver
// method.
type accessorKind int

const (
	// accessorScoped: the method returns a store and scopedDriver wraps
	// it for a restricted viewer.
	accessorScoped accessorKind = iota
	// accessorPassthrough: the method is not a data read (lifecycle,
	// health) and the embedded driver answers it for every viewer.
	accessorPassthrough
)

// scopedAccessors classifies every method of StorageDriver. The
// package's tests compare it with the interface's method set, so a
// method added to StorageDriver fails them until it is listed here and,
// if it returns a store, wrapped above.
var scopedAccessors = map[string]accessorKind{
	"Init":   accessorPassthrough,
	"Close":  accessorPassthrough,
	"Health": accessorPassthrough,
	// Auth is the principal registry: scopes govern it, not share
	// policies (ADR-080 §1), and the chain consults it before a viewer
	// exists, so it passes through for every viewer.
	"Auth": accessorPassthrough,

	"Objects":         accessorScoped,
	"Entities":        accessorScoped,
	"Edges":           accessorScoped,
	"Jobs":            accessorScoped,
	"Pipelines":       accessorScoped,
	"Steps":           accessorScoped,
	"Registries":      accessorScoped,
	"Reminders":       accessorScoped,
	"Feeds":           accessorScoped,
	"FeedItems":       accessorScoped,
	"Batches":         accessorScoped,
	"Detectors":       accessorScoped,
	"Blobs":           accessorScoped,
	"Proximity":       accessorScoped,
	"Watches":         accessorScoped,
	"Aliases":         accessorScoped,
	"AuditLog":        accessorScoped,
	"Attachments":     accessorScoped,
	"Resurfacing":     accessorScoped,
	"Entitlements":    accessorScoped,
	"PendingEntities": accessorScoped,
	"Metering":        accessorScoped,
	"Embeddings":      accessorScoped,
	"SavedSearches":   accessorScoped,
	"SearchHistory":   accessorScoped,
	"Watermarks":      accessorScoped,
	"UISessions":      accessorScoped,
}

// restrictedReads names, per scoped accessor, the store methods that
// fail closed for a restricted viewer: every method that returns rows,
// counts or derived data. The store's other methods are writes and
// reach the driver. The restricted wrappers are written by hand against
// this table; the conformance suite (storagetest) walks it to check
// each listed read refuses and each other method reaches the driver.
var restrictedReads = map[string][]string{
	"Aliases":         {"Resolve", "List"},
	"Attachments":     {"GetAttachment", "ListAttachments"},
	"AuditLog":        {"List", "GetObjectHistory"},
	"Batches":         {"Get"},
	"Blobs":           {"Get", "Exists", "List", "URL"},
	"Detectors":       {"Get", "List"},
	"Edges":           {"ListFrom", "ListTo", "CountMentionsTo", "RelatedObjectIDs"},
	"Embeddings":      {"Get", "Search", "ListMissing"},
	"Entities":        {"Get", "List", "Resolve"},
	"Entitlements":    {"Get", "List"},
	"FeedItems":       {"ExistsByGUID"},
	"Feeds":           {"Get", "GetByURL", "List"},
	"Jobs":            {"Get", "GetByIdempotencyKey", "List"},
	"Metering":        {"Aggregate", "List"},
	"Objects":         {"Get", "GetByContentHash", "GetBySourceKey", "List", "CountByType", "CountBlobRefs", "ListBySQL", "ListDueReminders", "ListPendingReminders", "VectorSearch", "FTSSearch", "FTSSearchNodeAware", "VectorSearchNodeAware"},
	"PendingEntities": {"List", "Get"},
	"Pipelines":       {"Get", "List"},
	"Proximity":       {"GetNeighbors", "GetNeighborsAbove", "Get", "FindStale", "Stats"},
	"Registries":      {"GetCachedManifest", "List"},
	"Reminders":       {"Get", "List"},
	"Resurfacing":     {"List"},
	"SavedSearches":   {"GetByName", "List"},
	"SearchHistory":   {"List"},
	"Steps":           {"Get", "List"},
	"UISessions":      {"Lookup", "Get", "List"},
	"Watches":         {"GetWatch", "ListWatches", "GetFileRecord", "ListFileRecords"},
	"Watermarks":      {"GetWatermark"},
}

// RestrictedReads returns, per StorageDriver store accessor, the store
// methods a restricted viewer may not run. It is the contract the
// conformance suite checks every driver's scoped reads against.
func RestrictedReads() map[string][]string {
	out := make(map[string][]string, len(restrictedReads))
	for accessor, methods := range restrictedReads {
		out[accessor] = append([]string(nil), methods...)
	}
	return out
}

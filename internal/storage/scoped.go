package storage

import (
	"errors"
	"fmt"
)

// ErrRestrictedViewer is returned by a read of a ScopedDriver that a
// restricted viewer may not run at all: the reads of the owner-only
// stores (jobs, pipelines, watches, ...) and the system reads of the
// scoped stores (reminders due, stale proximity, blob listings). A
// write to an owner-only store wraps it too (restrictedWrite). Such a
// read fails closed instead of answering unscoped rows; the transports
// answer it as a capability refusal, never as a row. A read a restricted
// viewer may run (RestrictedReads lists what it may not) answers through
// the viewer's scope instead, and a row outside it is ErrNotFound.
var ErrRestrictedViewer = errors.New("restricted viewer: scoped reads not supported")

// restrictedRead is the error a read method returns for a restricted
// viewer, naming the store and method so a fail-closed response can be
// traced to the read that refused.
func restrictedRead(store, method string) error {
	return fmt.Errorf("%s.%s: %w", store, method, ErrRestrictedViewer)
}

// restrictedWrite is the error a write method of an owner-only store
// returns for a restricted or nil viewer, naming the store and method
// like restrictedRead. It wraps ErrRestrictedViewer, so the transports
// answer it as the same capability refusal: the route that reaches the
// write should have been closed to the viewer, and this is the
// backstop behind it.
func restrictedWrite(store, method string) error {
	return fmt.Errorf("%s.%s: owner-only write: %w", store, method, ErrRestrictedViewer)
}

// ErrNoViewer is the cause of every refusal of a ScopedDriver built
// for a nil viewer: no transport attached a viewer to the request,
// which is a transport bug, not a visibility answer. It wraps nothing;
// refuseRead pairs it with ErrRestrictedViewer so existing checks hold,
// and the transports keep it a server error instead of a capability
// refusal.
var ErrNoViewer = errors.New("no viewer attached")

// refuseRead is the error a scoped read returns when it cannot answer
// through a scope: cause is ErrNoViewer for a nil viewer or
// ErrNoScopedReads for a driver store that cannot bind to one.
func refuseRead(store, method string, cause error) error {
	return fmt.Errorf("%s.%s: %w: %w", store, method, cause, ErrRestrictedViewer)
}

// ScopedDriver returns d as seen by v. An unrestricted viewer gets d
// itself: the owner's reads cost nothing and keep every optional
// capability the driver exposes. Any other viewer gets a driver whose
// reads answer through the viewer's scope (ViewerScope): object reads
// AND the viewer's predicate into their SQL, derived data follows the
// visibility of its source objects (ADR-080 §6), per-principal rows
// are kept to the viewer's principal, the owner-only stores refuse
// every read and write with ErrRestrictedViewer (restrictedWrites), and
// the other writes reach d unchanged. A nil viewer gets a driver whose
// every read, and every owner-only write, refuses.
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
	sd := scopedDriver{StorageDriver: d}
	if sc, ok := ViewerScope(v, driverDialect(d)); ok {
		sd.scope = &sc
	}
	return sd
}

// driverDialect is the SQL dialect d reports, SQLite when it reports
// none (the same rule as search.DialectFor).
func driverDialect(d StorageDriver) string {
	if dd, ok := d.(interface{ SQLDialect() string }); ok && dd.SQLDialect() == SQLDialectPostgres {
		return SQLDialectPostgres
	}
	return SQLDialectSQLite
}

// scopedDriver is a restricted viewer's StorageDriver. The embedded
// driver answers the methods that are not data reads (Init, Close,
// Health) and any accessor this package has not classified yet; every
// store accessor below returns the restricted wrapper of the driver's
// store, bound to scope when the viewer has one. A nil scope is the nil
// viewer: every read refuses.
type scopedDriver struct {
	StorageDriver
	scope *Scope
}

// scopedObjects returns the driver's object store bound to the scope,
// or nil when there is no scope or the store cannot bind.
func scopedObjects(st ObjectStore, sc *Scope) ObjectStore {
	if s, ok := st.(ObjectScoper); ok && sc != nil {
		return s.Scoped(*sc)
	}
	return nil
}

func scopedEntities(st EntityStore, sc *Scope) EntityStore {
	if s, ok := st.(EntityScoper); ok && sc != nil {
		return s.Scoped(*sc)
	}
	return nil
}

func scopedEdges(st EdgeStore, sc *Scope) EdgeStore {
	if s, ok := st.(EdgeScoper); ok && sc != nil {
		return s.Scoped(*sc)
	}
	return nil
}

func scopedEmbeddings(st EmbeddingStore, sc *Scope) EmbeddingStore {
	if s, ok := st.(EmbeddingScoper); ok && sc != nil {
		return s.Scoped(*sc)
	}
	return nil
}

func scopedProximity(st ProximityStore, sc *Scope) ProximityStore {
	if s, ok := st.(ProximityScoper); ok && sc != nil {
		return s.Scoped(*sc)
	}
	return nil
}

func scopedAliases(st AliasStore, sc *Scope) AliasStore {
	if s, ok := st.(AliasScoper); ok && sc != nil {
		return s.Scoped(*sc)
	}
	return nil
}

// principalOf is the scope's principal, "" with no scope.
func (s scopedDriver) principalOf() string {
	if s.scope == nil {
		return ""
	}
	return s.scope.Principal
}

// refusal is the cause a wrapper refuses scoped reads with: ErrNoViewer
// without a scope, ErrNoScopedReads when the driver's store could not
// bind to it (bound false), nil when the reads can answer.
func (s scopedDriver) refusal(bound bool) error {
	switch {
	case s.scope == nil:
		return ErrNoViewer
	case !bound:
		return ErrNoScopedReads
	}
	return nil
}

func (s scopedDriver) Objects() ObjectStore {
	st := s.StorageDriver.Objects()
	bound := scopedObjects(st, s.scope)
	return restrictedObjects{inner: st, scoped: bound, refuse: s.refusal(bound != nil)}
}

func (s scopedDriver) Entities() EntityStore {
	st := s.StorageDriver.Entities()
	bound := scopedEntities(st, s.scope)
	return restrictedEntities{inner: st, scoped: bound, refuse: s.refusal(bound != nil)}
}

func (s scopedDriver) Edges() EdgeStore {
	st := s.StorageDriver.Edges()
	bound := scopedEdges(st, s.scope)
	return restrictedEdges{inner: st, scoped: bound, refuse: s.refusal(bound != nil)}
}
func (s scopedDriver) Jobs() JobStore { return restrictedJobs{inner: s.StorageDriver.Jobs()} }
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
func (s scopedDriver) Blobs() BlobStore {
	refs := scopedObjects(s.StorageDriver.Objects(), s.scope)
	return restrictedBlobs{inner: s.StorageDriver.Blobs(), refs: refs, refuse: s.refusal(refs != nil)}
}
func (s scopedDriver) Proximity() ProximityStore {
	st := s.StorageDriver.Proximity()
	bound := scopedProximity(st, s.scope)
	return restrictedProximity{inner: st, scoped: bound, refuse: s.refusal(bound != nil)}
}
func (s scopedDriver) Watches() WatchStore {
	return restrictedWatches{inner: s.StorageDriver.Watches()}
}
func (s scopedDriver) Aliases() AliasStore {
	st := s.StorageDriver.Aliases()
	bound := scopedAliases(st, s.scope)
	return restrictedAliases{inner: st, scoped: bound, refuse: s.refusal(bound != nil)}
}
func (s scopedDriver) AuditLog() AuditStore {
	return restrictedAuditLog{inner: s.StorageDriver.AuditLog()}
}
func (s scopedDriver) Attachments() AttachmentStore {
	return restrictedAttachments{inner: s.StorageDriver.Attachments()}
}
func (s scopedDriver) Resurfacing() ResurfacingQueueStore {
	return restrictedResurfacing{inner: s.StorageDriver.Resurfacing(), principal: s.principalOf(), refuse: s.refusal(true)}
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
	st := s.StorageDriver.Embeddings()
	bound := scopedEmbeddings(st, s.scope)
	return restrictedEmbeddings{inner: st, scoped: bound, refuse: s.refusal(bound != nil)}
}
func (s scopedDriver) SavedSearches() SavedSearchStore {
	return restrictedSavedSearches{inner: s.StorageDriver.SavedSearches(), principal: s.principalOf(), refuse: s.refusal(true)}
}
func (s scopedDriver) SearchHistory() SearchHistoryStore {
	return restrictedSearchHistory{inner: s.StorageDriver.SearchHistory(), principal: s.principalOf(), refuse: s.refusal(true)}
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
	// Sharing is the sharing registry: scopes govern it like Auth, the
	// chain reads a principal's grants to compile its viewer before one
	// exists, and a restricted viewer lists its own audiences and
	// policies (ADR-080 §8), so it passes through for every viewer.
	"Sharing": accessorPassthrough,

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
// fail closed with ErrRestrictedViewer for every restricted viewer: the
// reads of the owner-only stores (ADR-080 §6: jobs, inbox and capture
// payloads, pipelines, watches, registries, feeds, the audit log, ...)
// and the system reads of the scoped stores (reminders due, blob
// listings, stale proximity, missing embeddings). The store's other
// methods are owner-only writes (restrictedWrites), which refuse too,
// other writes, which reach the driver, or scoped reads (scopedReads),
// which answer through the viewer's scope. The restricted wrappers are
// written by hand against these tables; the conformance suite
// (storagetest) walks them to check each listed read and owner-only
// write refuses, each scoped read answers visible rows only and each
// other method reaches the driver.
var restrictedReads = map[string][]string{
	"Attachments":     {"GetAttachment", "ListAttachments"},
	"AuditLog":        {"List", "GetObjectHistory"},
	"Batches":         {"Get"},
	"Detectors":       {"Get", "List"},
	"Embeddings":      {"Get", "ListMissing"},
	"Entitlements":    {"Get", "List"},
	"FeedItems":       {"ExistsByGUID"},
	"Feeds":           {"Get", "GetByURL", "List"},
	"Jobs":            {"Get", "GetByIdempotencyKey", "List"},
	"Metering":        {"Aggregate", "List"},
	"Objects":         {"CountBlobRefs", "ListDueReminders", "ListPendingReminders"},
	"PendingEntities": {"List", "Get"},
	"Pipelines":       {"Get", "List"},
	"Proximity":       {"FindStale", "Stats"},
	"Registries":      {"GetCachedManifest", "List"},
	"Reminders":       {"Get", "List"},
	"SearchHistory":   {"ClearByProfile"},
	"Steps":           {"Get", "List"},
	"UISessions":      {"Lookup", "Get", "List"},
	"Watches":         {"GetWatch", "ListWatches", "GetFileRecord", "ListFileRecords"},
	"Watermarks":      {"GetWatermark"},
}

// restrictedWrites names, per owner-only store, the store methods that
// write and fail closed with ErrRestrictedViewer for every restricted
// or nil viewer, without reaching the driver. An owner-only store
// (ADR-080 §6) holds rows no share policy covers: jobs, pipelines,
// steps, registries, entitlements (a restricted principal writing one
// would grant itself namespaces), metering, feeds, batches, detectors
// and watches. No route open to a restricted viewer writes them; the
// system (worker, pipeline, inbound gate, CLI) writes them through the
// unscoped driver or an Unrestricted viewer. Every method of these
// stores is either here or in restrictedReads; the package's tests
// fail on one that is neither. Writes to the scoped stores (objects,
// entities, edges, aliases, ...) are not listed: they reach the driver,
// and the scoped reads decide what the viewer sees of them.
var restrictedWrites = map[string][]string{
	"Batches":      {"Create", "Update"},
	"Detectors":    {"Create", "Update", "Delete", "Enable", "Disable"},
	"Entitlements": {"Upsert", "Delete"},
	"FeedItems":    {"Create"},
	"Feeds":        {"Create", "Update", "Delete"},
	"Jobs":         {"Create", "AcquireNext", "Complete", "Fail", "Retry", "Cancel", "RecoverStale", "ExtendLease", "ReleaseLease"},
	"Metering":     {"Record", "RecordCapped"},
	"Pipelines":    {"Create", "Update", "Delete", "Archive", "Unarchive"},
	"Registries":   {"CacheManifest", "UpdateETag", "Delete"},
	"Steps":        {"Create", "Unregister", "Update"},
	"Watches":      {"CreateWatch", "UpdateWatch", "DeleteWatch", "UpsertFileRecord", "DeleteFileRecord"},
}

// scopedReads names, per scoped accessor, the store methods that answer
// a restricted viewer through the driver's scoped store (the scoper
// interfaces): visible rows only, exact counts, ErrNotFound for a
// hidden row. When the driver's store cannot bind to a scope, and for
// a nil viewer, they refuse with ErrNoScopedReads before touching the
// store.
var scopedReads = map[string][]string{
	"Aliases":    {"Resolve", "List"},
	"Blobs":      {"Get", "Exists", "List", "URL"},
	"Edges":      {"ListFrom", "ListTo", "CountMentionsTo", "RelatedObjectIDs"},
	"Embeddings": {"Search"},
	"Entities":   {"Get", "List", "Namespaces", "Resolve"},
	"Objects":    {"Get", "GetByContentHash", "GetBySourceKey", "List", "CountByType", "ListBySQL", "VectorSearch", "FTSSearch", "FTSSearchNodeAware", "VectorSearchNodeAware"},
	"Proximity":  {"GetNeighbors", "GetNeighborsAbove", "Get"},
}

// principalReads names, per scoped accessor, the store methods the
// wrapper answers by pinning the viewer's principal on the driver's
// store (the per-principal tables, ADR-080 §4): the viewer's own rows
// only. A nil viewer, which has no principal, refuses them.
var principalReads = map[string][]string{
	"Resurfacing":   {"List"},
	"SavedSearches": {"GetByName", "List"},
	"SearchHistory": {"List"},
}

// RestrictedReads returns, per StorageDriver store accessor, the store
// methods a restricted viewer may not run at all. It is the contract
// the conformance suite checks every driver's scoped reads against.
func RestrictedReads() map[string][]string {
	return cloneReads(restrictedReads)
}

// RestrictedWrites returns, per owner-only store accessor, the write
// methods a restricted viewer may not run at all. The conformance
// suite checks every driver's owner-only writes refuse against it.
func RestrictedWrites() map[string][]string {
	return cloneReads(restrictedWrites)
}

// ScopedReads returns, per StorageDriver store accessor, the store
// methods a restricted viewer runs through the driver's scoped store.
func ScopedReads() map[string][]string {
	return cloneReads(scopedReads)
}

// PrincipalReads returns, per StorageDriver store accessor, the store
// methods a restricted viewer runs as its own principal.
func PrincipalReads() map[string][]string {
	return cloneReads(principalReads)
}

func cloneReads(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for accessor, methods := range in {
		out[accessor] = append([]string(nil), methods...)
	}
	return out
}

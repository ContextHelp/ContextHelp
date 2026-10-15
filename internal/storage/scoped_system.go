package storage

import (
	"context"
	"time"
)

// The stores below are owner-only (ADR-080 §6): no share policy covers
// their rows, so a restricted or nil viewer may neither read nor write
// them, and every method refuses with restrictedRead or restrictedWrite
// (the restrictedReads and restrictedWrites tables in scoped.go) before
// touching the driver. The system writes them through the unscoped
// driver (job worker, pipeline, inbound entitlement gate, step
// installer) or under an Unrestricted viewer (the CLI at the owner's
// terminal), which ScopedDriver answers with the driver itself. A
// method added to one of these store interfaces must refuse the same
// way; the package's tests fail until it is listed in one of the two
// tables.

// restrictedJobs is the JobStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedJobs struct{ inner JobStore }

var _ JobStore = restrictedJobs{}

func (v restrictedJobs) Create(_ context.Context, _ *Job) error {
	return restrictedWrite("Jobs", "Create")
}

func (v restrictedJobs) Get(_ context.Context, _ string) (*Job, error) {
	return nil, restrictedRead("Jobs", "Get")
}

func (v restrictedJobs) GetByIdempotencyKey(_ context.Context, _ string) (*Job, error) {
	return nil, restrictedRead("Jobs", "GetByIdempotencyKey")
}

func (v restrictedJobs) List(_ context.Context, _ JobFilter) ([]*Job, int, error) {
	return nil, 0, restrictedRead("Jobs", "List")
}

func (v restrictedJobs) AcquireNext(_ context.Context) (*Job, error) {
	return nil, restrictedWrite("Jobs", "AcquireNext")
}

func (v restrictedJobs) Complete(_ context.Context, _ string, _ string) error {
	return restrictedWrite("Jobs", "Complete")
}

func (v restrictedJobs) Fail(_ context.Context, _ string, _ string) error {
	return restrictedWrite("Jobs", "Fail")
}

func (v restrictedJobs) Retry(_ context.Context, _ string) error {
	return restrictedWrite("Jobs", "Retry")
}

func (v restrictedJobs) Cancel(_ context.Context, _ string) error {
	return restrictedWrite("Jobs", "Cancel")
}

func (v restrictedJobs) RecoverStale(_ context.Context, _ int64) (int, error) {
	return 0, restrictedWrite("Jobs", "RecoverStale")
}

func (v restrictedJobs) ExtendLease(_ context.Context, _ string, _ string, _ time.Duration) (bool, error) {
	return false, restrictedWrite("Jobs", "ExtendLease")
}

func (v restrictedJobs) ReleaseLease(_ context.Context, _ string, _ string) error {
	return restrictedWrite("Jobs", "ReleaseLease")
}

// restrictedPipelines is the PipelineStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedPipelines struct{ inner PipelineStore }

var _ PipelineStore = restrictedPipelines{}

func (v restrictedPipelines) Create(_ context.Context, _ *Pipeline) error {
	return restrictedWrite("Pipelines", "Create")
}

func (v restrictedPipelines) Get(_ context.Context, _ string) (*Pipeline, error) {
	return nil, restrictedRead("Pipelines", "Get")
}

func (v restrictedPipelines) List(_ context.Context, _ PipelineFilter) ([]*Pipeline, int, error) {
	return nil, 0, restrictedRead("Pipelines", "List")
}

func (v restrictedPipelines) Update(_ context.Context, _ *Pipeline) error {
	return restrictedWrite("Pipelines", "Update")
}

func (v restrictedPipelines) Delete(_ context.Context, _ string) error {
	return restrictedWrite("Pipelines", "Delete")
}

func (v restrictedPipelines) Archive(_ context.Context, _ string) error {
	return restrictedWrite("Pipelines", "Archive")
}

func (v restrictedPipelines) Unarchive(_ context.Context, _ string) error {
	return restrictedWrite("Pipelines", "Unarchive")
}

// restrictedSteps is the StepStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedSteps struct{ inner StepStore }

var _ StepStore = restrictedSteps{}

func (v restrictedSteps) Create(_ context.Context, _ *RegisteredStep) error {
	return restrictedWrite("Steps", "Create")
}

func (v restrictedSteps) Get(_ context.Context, _ string) (*RegisteredStep, error) {
	return nil, restrictedRead("Steps", "Get")
}

func (v restrictedSteps) List(_ context.Context, _ string) ([]*RegisteredStep, int, error) {
	return nil, 0, restrictedRead("Steps", "List")
}

func (v restrictedSteps) Unregister(_ context.Context, _ string) error {
	return restrictedWrite("Steps", "Unregister")
}

func (v restrictedSteps) Update(_ context.Context, _ *RegisteredStep) error {
	return restrictedWrite("Steps", "Update")
}

// restrictedRegistries is the RegistryStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedRegistries struct{ inner RegistryStore }

var _ RegistryStore = restrictedRegistries{}

func (v restrictedRegistries) CacheManifest(_ context.Context, _ *RegistryCache) error {
	return restrictedWrite("Registries", "CacheManifest")
}

func (v restrictedRegistries) GetCachedManifest(_ context.Context, _ string) (*RegistryCache, error) {
	return nil, restrictedRead("Registries", "GetCachedManifest")
}

func (v restrictedRegistries) UpdateETag(_ context.Context, _ string, _ string) error {
	return restrictedWrite("Registries", "UpdateETag")
}

func (v restrictedRegistries) List(_ context.Context) ([]*RegistryCache, int, error) {
	return nil, 0, restrictedRead("Registries", "List")
}

func (v restrictedRegistries) Delete(_ context.Context, _ string) error {
	return restrictedWrite("Registries", "Delete")
}

// restrictedEntitlements is the EntitlementStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedEntitlements struct{ inner EntitlementStore }

var _ EntitlementStore = restrictedEntitlements{}

func (v restrictedEntitlements) Upsert(_ context.Context, _ *RegistryEntitlement) error {
	return restrictedWrite("Entitlements", "Upsert")
}

func (v restrictedEntitlements) Get(_ context.Context, _ string) (*RegistryEntitlement, error) {
	return nil, restrictedRead("Entitlements", "Get")
}

func (v restrictedEntitlements) List(_ context.Context) ([]*RegistryEntitlement, error) {
	return nil, restrictedRead("Entitlements", "List")
}

// restrictedMetering is the MeteringStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedMetering struct{ inner MeteringStore }

var _ MeteringStore = restrictedMetering{}

func (v restrictedMetering) Record(_ context.Context, _ *MeteringEvent) error {
	return restrictedWrite("Metering", "Record")
}

func (v restrictedMetering) RecordCapped(_ context.Context, _ *MeteringEvent, _ time.Time, _ int) (bool, error) {
	return false, restrictedWrite("Metering", "RecordCapped")
}

func (v restrictedMetering) Aggregate(_ context.Context, _ MeteringFilter) ([]*MeteringAggregate, error) {
	return nil, restrictedRead("Metering", "Aggregate")
}

func (v restrictedMetering) List(_ context.Context, _ MeteringFilter) ([]*MeteringEvent, error) {
	return nil, restrictedRead("Metering", "List")
}

// restrictedFeeds is the FeedStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedFeeds struct{ inner FeedStore }

var _ FeedStore = restrictedFeeds{}

func (v restrictedFeeds) Create(_ context.Context, _ *Feed) error {
	return restrictedWrite("Feeds", "Create")
}

func (v restrictedFeeds) Get(_ context.Context, _ string) (*Feed, error) {
	return nil, restrictedRead("Feeds", "Get")
}

func (v restrictedFeeds) GetByURL(_ context.Context, _ string) (*Feed, error) {
	return nil, restrictedRead("Feeds", "GetByURL")
}

func (v restrictedFeeds) List(_ context.Context, _ FeedFilter) ([]*Feed, error) {
	return nil, restrictedRead("Feeds", "List")
}

func (v restrictedFeeds) Update(_ context.Context, _ *Feed) error {
	return restrictedWrite("Feeds", "Update")
}

func (v restrictedFeeds) Delete(_ context.Context, _ string) error {
	return restrictedWrite("Feeds", "Delete")
}

// restrictedFeedItems is the FeedItemStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedFeedItems struct{ inner FeedItemStore }

var _ FeedItemStore = restrictedFeedItems{}

func (v restrictedFeedItems) Create(_ context.Context, _ *FeedItem) error {
	return restrictedWrite("FeedItems", "Create")
}

func (v restrictedFeedItems) ExistsByGUID(_ context.Context, _ string, _ string) (bool, error) {
	return false, restrictedRead("FeedItems", "ExistsByGUID")
}

// restrictedBatches is the BatchStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedBatches struct{ inner BatchStore }

var _ BatchStore = restrictedBatches{}

func (v restrictedBatches) Create(_ context.Context, _ *Batch) error {
	return restrictedWrite("Batches", "Create")
}

func (v restrictedBatches) Get(_ context.Context, _ string) (*Batch, error) {
	return nil, restrictedRead("Batches", "Get")
}

func (v restrictedBatches) Update(_ context.Context, _ *Batch) error {
	return restrictedWrite("Batches", "Update")
}

// restrictedDetectors is the DetectorStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedDetectors struct{ inner DetectorStore }

var _ DetectorStore = restrictedDetectors{}

func (v restrictedDetectors) Create(_ context.Context, _ *DetectorRecord) error {
	return restrictedWrite("Detectors", "Create")
}

func (v restrictedDetectors) Get(_ context.Context, _ string) (*DetectorRecord, error) {
	return nil, restrictedRead("Detectors", "Get")
}

func (v restrictedDetectors) List(_ context.Context, _ DetectorFilter) ([]*DetectorRecord, int, error) {
	return nil, 0, restrictedRead("Detectors", "List")
}

func (v restrictedDetectors) Update(_ context.Context, _ *DetectorRecord) error {
	return restrictedWrite("Detectors", "Update")
}

func (v restrictedDetectors) Delete(_ context.Context, _ string) error {
	return restrictedWrite("Detectors", "Delete")
}

func (v restrictedDetectors) Enable(_ context.Context, _ string) error {
	return restrictedWrite("Detectors", "Enable")
}

func (v restrictedDetectors) Disable(_ context.Context, _ string) error {
	return restrictedWrite("Detectors", "Disable")
}

// restrictedWatches is the WatchStore of a restricted viewer: every read and every write
// fails closed with ErrRestrictedViewer without reaching the driver.
type restrictedWatches struct{ inner WatchStore }

var _ WatchStore = restrictedWatches{}

func (v restrictedWatches) CreateWatch(_ context.Context, _ *WatchConfig) error {
	return restrictedWrite("Watches", "CreateWatch")
}

func (v restrictedWatches) GetWatch(_ context.Context, _ string) (*WatchConfig, error) {
	return nil, restrictedRead("Watches", "GetWatch")
}

func (v restrictedWatches) ListWatches(_ context.Context, _ string) ([]*WatchConfig, error) {
	return nil, restrictedRead("Watches", "ListWatches")
}

func (v restrictedWatches) UpdateWatch(_ context.Context, _ *WatchConfig) error {
	return restrictedWrite("Watches", "UpdateWatch")
}

func (v restrictedWatches) DeleteWatch(_ context.Context, _ string) error {
	return restrictedWrite("Watches", "DeleteWatch")
}

func (v restrictedWatches) UpsertFileRecord(_ context.Context, _ *WatchFileRecord) error {
	return restrictedWrite("Watches", "UpsertFileRecord")
}

func (v restrictedWatches) GetFileRecord(_ context.Context, _ string, _ string) (*WatchFileRecord, error) {
	return nil, restrictedRead("Watches", "GetFileRecord")
}

func (v restrictedWatches) DeleteFileRecord(_ context.Context, _ string, _ string) error {
	return restrictedWrite("Watches", "DeleteFileRecord")
}

func (v restrictedWatches) ListFileRecords(_ context.Context, _ string) ([]*WatchFileRecord, error) {
	return nil, restrictedRead("Watches", "ListFileRecords")
}

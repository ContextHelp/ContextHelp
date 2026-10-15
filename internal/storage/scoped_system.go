package storage

import (
	"context"
	"time"
)

// restrictedJobs is the JobStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedJobs struct{ inner JobStore }

var _ JobStore = restrictedJobs{}

func (v restrictedJobs) Create(ctx context.Context, job *Job) error {
	return v.inner.Create(ctx, job)
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

func (v restrictedJobs) AcquireNext(ctx context.Context) (*Job, error) {
	return v.inner.AcquireNext(ctx)
}

func (v restrictedJobs) Complete(ctx context.Context, id string, resultID string) error {
	return v.inner.Complete(ctx, id, resultID)
}

func (v restrictedJobs) Fail(ctx context.Context, id string, errMsg string) error {
	return v.inner.Fail(ctx, id, errMsg)
}

func (v restrictedJobs) Retry(ctx context.Context, id string) error {
	return v.inner.Retry(ctx, id)
}

func (v restrictedJobs) Cancel(ctx context.Context, id string) error {
	return v.inner.Cancel(ctx, id)
}

func (v restrictedJobs) RecoverStale(ctx context.Context, timeout int64) (int, error) {
	return v.inner.RecoverStale(ctx, timeout)
}

func (v restrictedJobs) ExtendLease(ctx context.Context, id string, claim string, ttl time.Duration) (bool, error) {
	return v.inner.ExtendLease(ctx, id, claim, ttl)
}

func (v restrictedJobs) ReleaseLease(ctx context.Context, id string, claim string) error {
	return v.inner.ReleaseLease(ctx, id, claim)
}

// restrictedPipelines is the PipelineStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedPipelines struct{ inner PipelineStore }

var _ PipelineStore = restrictedPipelines{}

func (v restrictedPipelines) Create(ctx context.Context, pipeline *Pipeline) error {
	return v.inner.Create(ctx, pipeline)
}

func (v restrictedPipelines) Get(_ context.Context, _ string) (*Pipeline, error) {
	return nil, restrictedRead("Pipelines", "Get")
}

func (v restrictedPipelines) List(_ context.Context, _ PipelineFilter) ([]*Pipeline, int, error) {
	return nil, 0, restrictedRead("Pipelines", "List")
}

func (v restrictedPipelines) Update(ctx context.Context, pipeline *Pipeline) error {
	return v.inner.Update(ctx, pipeline)
}

func (v restrictedPipelines) Delete(ctx context.Context, name string) error {
	return v.inner.Delete(ctx, name)
}

func (v restrictedPipelines) Archive(ctx context.Context, name string) error {
	return v.inner.Archive(ctx, name)
}

func (v restrictedPipelines) Unarchive(ctx context.Context, name string) error {
	return v.inner.Unarchive(ctx, name)
}

// restrictedSteps is the StepStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedSteps struct{ inner StepStore }

var _ StepStore = restrictedSteps{}

func (v restrictedSteps) Create(ctx context.Context, step *RegisteredStep) error {
	return v.inner.Create(ctx, step)
}

func (v restrictedSteps) Get(_ context.Context, _ string) (*RegisteredStep, error) {
	return nil, restrictedRead("Steps", "Get")
}

func (v restrictedSteps) List(_ context.Context, _ string) ([]*RegisteredStep, int, error) {
	return nil, 0, restrictedRead("Steps", "List")
}

func (v restrictedSteps) Unregister(ctx context.Context, name string) error {
	return v.inner.Unregister(ctx, name)
}

func (v restrictedSteps) Update(ctx context.Context, step *RegisteredStep) error {
	return v.inner.Update(ctx, step)
}

// restrictedRegistries is the RegistryStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedRegistries struct{ inner RegistryStore }

var _ RegistryStore = restrictedRegistries{}

func (v restrictedRegistries) CacheManifest(ctx context.Context, cache *RegistryCache) error {
	return v.inner.CacheManifest(ctx, cache)
}

func (v restrictedRegistries) GetCachedManifest(_ context.Context, _ string) (*RegistryCache, error) {
	return nil, restrictedRead("Registries", "GetCachedManifest")
}

func (v restrictedRegistries) UpdateETag(ctx context.Context, url string, etag string) error {
	return v.inner.UpdateETag(ctx, url, etag)
}

func (v restrictedRegistries) List(_ context.Context) ([]*RegistryCache, int, error) {
	return nil, 0, restrictedRead("Registries", "List")
}

func (v restrictedRegistries) Delete(ctx context.Context, url string) error {
	return v.inner.Delete(ctx, url)
}

// restrictedEntitlements is the EntitlementStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedEntitlements struct{ inner EntitlementStore }

var _ EntitlementStore = restrictedEntitlements{}

func (v restrictedEntitlements) Upsert(ctx context.Context, e *RegistryEntitlement) error {
	return v.inner.Upsert(ctx, e)
}

func (v restrictedEntitlements) Get(_ context.Context, _ string) (*RegistryEntitlement, error) {
	return nil, restrictedRead("Entitlements", "Get")
}

func (v restrictedEntitlements) List(_ context.Context) ([]*RegistryEntitlement, error) {
	return nil, restrictedRead("Entitlements", "List")
}

// restrictedMetering is the MeteringStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedMetering struct{ inner MeteringStore }

var _ MeteringStore = restrictedMetering{}

func (v restrictedMetering) Record(ctx context.Context, event *MeteringEvent) error {
	return v.inner.Record(ctx, event)
}

func (v restrictedMetering) RecordCapped(ctx context.Context, event *MeteringEvent, periodStart time.Time, limit int) (bool, error) {
	return v.inner.RecordCapped(ctx, event, periodStart, limit)
}

func (v restrictedMetering) Aggregate(_ context.Context, _ MeteringFilter) ([]*MeteringAggregate, error) {
	return nil, restrictedRead("Metering", "Aggregate")
}

func (v restrictedMetering) List(_ context.Context, _ MeteringFilter) ([]*MeteringEvent, error) {
	return nil, restrictedRead("Metering", "List")
}

// restrictedFeeds is the FeedStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedFeeds struct{ inner FeedStore }

var _ FeedStore = restrictedFeeds{}

func (v restrictedFeeds) Create(ctx context.Context, feed *Feed) error {
	return v.inner.Create(ctx, feed)
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

func (v restrictedFeeds) Update(ctx context.Context, feed *Feed) error {
	return v.inner.Update(ctx, feed)
}

func (v restrictedFeeds) Delete(ctx context.Context, id string) error {
	return v.inner.Delete(ctx, id)
}

// restrictedFeedItems is the FeedItemStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedFeedItems struct{ inner FeedItemStore }

var _ FeedItemStore = restrictedFeedItems{}

func (v restrictedFeedItems) Create(ctx context.Context, item *FeedItem) error {
	return v.inner.Create(ctx, item)
}

func (v restrictedFeedItems) ExistsByGUID(_ context.Context, _ string, _ string) (bool, error) {
	return false, restrictedRead("FeedItems", "ExistsByGUID")
}

// restrictedBatches is the BatchStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedBatches struct{ inner BatchStore }

var _ BatchStore = restrictedBatches{}

func (v restrictedBatches) Create(ctx context.Context, batch *Batch) error {
	return v.inner.Create(ctx, batch)
}

func (v restrictedBatches) Get(_ context.Context, _ string) (*Batch, error) {
	return nil, restrictedRead("Batches", "Get")
}

func (v restrictedBatches) Update(ctx context.Context, batch *Batch) error {
	return v.inner.Update(ctx, batch)
}

// restrictedDetectors is the DetectorStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedDetectors struct{ inner DetectorStore }

var _ DetectorStore = restrictedDetectors{}

func (v restrictedDetectors) Create(ctx context.Context, d *DetectorRecord) error {
	return v.inner.Create(ctx, d)
}

func (v restrictedDetectors) Get(_ context.Context, _ string) (*DetectorRecord, error) {
	return nil, restrictedRead("Detectors", "Get")
}

func (v restrictedDetectors) List(_ context.Context, _ DetectorFilter) ([]*DetectorRecord, int, error) {
	return nil, 0, restrictedRead("Detectors", "List")
}

func (v restrictedDetectors) Update(ctx context.Context, d *DetectorRecord) error {
	return v.inner.Update(ctx, d)
}

func (v restrictedDetectors) Delete(ctx context.Context, id string) error {
	return v.inner.Delete(ctx, id)
}

func (v restrictedDetectors) Enable(ctx context.Context, id string) error {
	return v.inner.Enable(ctx, id)
}

func (v restrictedDetectors) Disable(ctx context.Context, id string) error {
	return v.inner.Disable(ctx, id)
}

// restrictedWatches is the WatchStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedWatches struct{ inner WatchStore }

var _ WatchStore = restrictedWatches{}

func (v restrictedWatches) CreateWatch(ctx context.Context, w *WatchConfig) error {
	return v.inner.CreateWatch(ctx, w)
}

func (v restrictedWatches) GetWatch(_ context.Context, _ string) (*WatchConfig, error) {
	return nil, restrictedRead("Watches", "GetWatch")
}

func (v restrictedWatches) ListWatches(_ context.Context, _ string) ([]*WatchConfig, error) {
	return nil, restrictedRead("Watches", "ListWatches")
}

func (v restrictedWatches) UpdateWatch(ctx context.Context, w *WatchConfig) error {
	return v.inner.UpdateWatch(ctx, w)
}

func (v restrictedWatches) DeleteWatch(ctx context.Context, id string) error {
	return v.inner.DeleteWatch(ctx, id)
}

func (v restrictedWatches) UpsertFileRecord(ctx context.Context, r *WatchFileRecord) error {
	return v.inner.UpsertFileRecord(ctx, r)
}

func (v restrictedWatches) GetFileRecord(_ context.Context, _ string, _ string) (*WatchFileRecord, error) {
	return nil, restrictedRead("Watches", "GetFileRecord")
}

func (v restrictedWatches) DeleteFileRecord(ctx context.Context, watchID string, filePath string) error {
	return v.inner.DeleteFileRecord(ctx, watchID, filePath)
}

func (v restrictedWatches) ListFileRecords(_ context.Context, _ string) ([]*WatchFileRecord, error) {
	return nil, restrictedRead("Watches", "ListFileRecords")
}

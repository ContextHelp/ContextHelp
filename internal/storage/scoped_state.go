package storage

import (
	"context"
	"time"
)

// restrictedAuditLog is the AuditStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedAuditLog struct{ inner AuditStore }

var _ AuditStore = restrictedAuditLog{}

func (v restrictedAuditLog) Append(ctx context.Context, entry *AuditEntry) error {
	return v.inner.Append(ctx, entry)
}

func (v restrictedAuditLog) List(_ context.Context, _ AuditFilter) ([]*AuditEntry, int, error) {
	return nil, 0, restrictedRead("AuditLog", "List")
}

func (v restrictedAuditLog) GetObjectHistory(_ context.Context, _ string) ([]*AuditEntry, error) {
	return nil, restrictedRead("AuditLog", "GetObjectHistory")
}

// restrictedSavedSearches is the SavedSearchStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedSavedSearches struct{ inner SavedSearchStore }

var _ SavedSearchStore = restrictedSavedSearches{}

func (v restrictedSavedSearches) Create(ctx context.Context, s *SavedSearch) error {
	return v.inner.Create(ctx, s)
}

func (v restrictedSavedSearches) GetByName(_ context.Context, _ string) (*SavedSearch, error) {
	return nil, restrictedRead("SavedSearches", "GetByName")
}

func (v restrictedSavedSearches) List(_ context.Context, _ SavedSearchFilter) ([]*SavedSearch, error) {
	return nil, restrictedRead("SavedSearches", "List")
}

func (v restrictedSavedSearches) Update(ctx context.Context, s *SavedSearch) error {
	return v.inner.Update(ctx, s)
}

func (v restrictedSavedSearches) Delete(ctx context.Context, name string) error {
	return v.inner.Delete(ctx, name)
}

// restrictedSearchHistory is the SearchHistoryStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedSearchHistory struct{ inner SearchHistoryStore }

var _ SearchHistoryStore = restrictedSearchHistory{}

func (v restrictedSearchHistory) Append(ctx context.Context, e *SearchHistoryEntry) error {
	return v.inner.Append(ctx, e)
}

func (v restrictedSearchHistory) List(_ context.Context, _ SearchHistoryFilter) ([]*SearchHistoryEntry, error) {
	return nil, restrictedRead("SearchHistory", "List")
}

func (v restrictedSearchHistory) ClearByProfile(ctx context.Context, profileID string) error {
	return v.inner.ClearByProfile(ctx, profileID)
}

// restrictedResurfacing is the ResurfacingQueueStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedResurfacing struct{ inner ResurfacingQueueStore }

var _ ResurfacingQueueStore = restrictedResurfacing{}

func (v restrictedResurfacing) Upsert(ctx context.Context, e *ResurfacingEntry) error {
	return v.inner.Upsert(ctx, e)
}

func (v restrictedResurfacing) List(_ context.Context, _ ResurfacingFilter) ([]*ResurfacingEntry, error) {
	return nil, restrictedRead("Resurfacing", "List")
}

func (v restrictedResurfacing) MarkSurfaced(ctx context.Context, id string, now time.Time) error {
	return v.inner.MarkSurfaced(ctx, id, now)
}

func (v restrictedResurfacing) Dismiss(ctx context.Context, id string, now time.Time) error {
	return v.inner.Dismiss(ctx, id, now)
}

func (v restrictedResurfacing) DeleteByProfile(ctx context.Context, profileID string) error {
	return v.inner.DeleteByProfile(ctx, profileID)
}

func (v restrictedResurfacing) DeleteByObject(ctx context.Context, objectID string) error {
	return v.inner.DeleteByObject(ctx, objectID)
}

// restrictedReminders is the ReminderStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedReminders struct{ inner ReminderStore }

var _ ReminderStore = restrictedReminders{}

func (v restrictedReminders) Create(ctx context.Context, reminder *SystemReminder) error {
	return v.inner.Create(ctx, reminder)
}

func (v restrictedReminders) Get(_ context.Context, _ string) (*SystemReminder, error) {
	return nil, restrictedRead("Reminders", "Get")
}

func (v restrictedReminders) List(_ context.Context, _ bool) ([]*SystemReminder, int, error) {
	return nil, 0, restrictedRead("Reminders", "List")
}

func (v restrictedReminders) Dismiss(ctx context.Context, id string) error {
	return v.inner.Dismiss(ctx, id)
}

// restrictedWatermarks is the WatermarkStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedWatermarks struct{ inner WatermarkStore }

var _ WatermarkStore = restrictedWatermarks{}

func (v restrictedWatermarks) GetWatermark(_ context.Context, _ string) (int64, error) {
	return 0, restrictedRead("Watermarks", "GetWatermark")
}

func (v restrictedWatermarks) SetWatermark(ctx context.Context, federationName string, seq int64) error {
	return v.inner.SetWatermark(ctx, federationName, seq)
}

// restrictedUISessions is the UISessionStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedUISessions struct{ inner UISessionStore }

var _ UISessionStore = restrictedUISessions{}

func (v restrictedUISessions) CreateLoginCode(ctx context.Context, c *UILoginCode) error {
	return v.inner.CreateLoginCode(ctx, c)
}

func (v restrictedUISessions) ConsumeLoginCode(ctx context.Context, codeHash string, now time.Time) (*UILoginCode, error) {
	return v.inner.ConsumeLoginCode(ctx, codeHash, now)
}

func (v restrictedUISessions) Create(ctx context.Context, s *UISession) error {
	return v.inner.Create(ctx, s)
}

func (v restrictedUISessions) Lookup(_ context.Context, _ string, _ time.Time) (*UISession, error) {
	return nil, restrictedRead("UISessions", "Lookup")
}

func (v restrictedUISessions) Get(_ context.Context, _ string) (*UISession, error) {
	return nil, restrictedRead("UISessions", "Get")
}

func (v restrictedUISessions) Touch(ctx context.Context, id string, at time.Time, idleUntil time.Time) error {
	return v.inner.Touch(ctx, id, at, idleUntil)
}

func (v restrictedUISessions) Revoke(ctx context.Context, id string, at time.Time, reason string) error {
	return v.inner.Revoke(ctx, id, at, reason)
}

func (v restrictedUISessions) RevokeByTokenHash(ctx context.Context, tokenHash string, at time.Time, reason string) (int, error) {
	return v.inner.RevokeByTokenHash(ctx, tokenHash, at, reason)
}

func (v restrictedUISessions) List(_ context.Context, _ UISessionFilter) ([]*UISession, error) {
	return nil, restrictedRead("UISessions", "List")
}

func (v restrictedUISessions) Prune(ctx context.Context, before time.Time) (int, error) {
	return v.inner.Prune(ctx, before)
}

// restrictedPendingEntities is the PendingEntityStore of a restricted viewer: every read fails closed
// with ErrRestrictedViewer, every write reaches the driver.
type restrictedPendingEntities struct{ inner PendingEntityStore }

var _ PendingEntityStore = restrictedPendingEntities{}

func (v restrictedPendingEntities) Upsert(ctx context.Context, p *PendingEntity) error {
	return v.inner.Upsert(ctx, p)
}

func (v restrictedPendingEntities) List(_ context.Context, _ PendingEntityFilter) ([]*PendingEntity, error) {
	return nil, restrictedRead("PendingEntities", "List")
}

func (v restrictedPendingEntities) Get(_ context.Context, _ string) (*PendingEntity, error) {
	return nil, restrictedRead("PendingEntities", "Get")
}

func (v restrictedPendingEntities) Delete(ctx context.Context, id string) error {
	return v.inner.Delete(ctx, id)
}

func (v restrictedPendingEntities) DeleteEntity(ctx context.Context, registryName string, slug string) (int, error) {
	return v.inner.DeleteEntity(ctx, registryName, slug)
}

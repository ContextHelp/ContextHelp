package storage

import (
	"context"
	"fmt"
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

// principalRead is the error a per-principal read returns for a viewer
// with no principal (the nil viewer).
func principalRead(store, method string) error {
	return fmt.Errorf("%s.%s: no principal: %w", store, method, ErrRestrictedViewer)
}

// restrictedSavedSearches is the SavedSearchStore of a restricted
// viewer: the viewer's own searches only (ADR-080 §4, per-principal
// state). Create stores as the viewer's principal whatever the row
// says; GetByName, Update and Delete answer another principal's search
// as missing; List is pinned to the principal. A viewer with no
// principal refuses.
type restrictedSavedSearches struct {
	inner     SavedSearchStore
	principal string
}

var _ SavedSearchStore = restrictedSavedSearches{}

func (v restrictedSavedSearches) Create(ctx context.Context, s *SavedSearch) error {
	if v.principal == "" {
		return principalRead("SavedSearches", "Create")
	}
	s.OwnerPrincipal = v.principal
	return v.inner.Create(ctx, s)
}

// own returns the viewer's saved search named name, nil when there is
// none or it belongs to another principal.
func (v restrictedSavedSearches) own(ctx context.Context, name string) (*SavedSearch, error) {
	s, err := v.inner.GetByName(ctx, name)
	if err != nil || s == nil {
		return nil, err
	}
	if PrincipalOrOwner(s.OwnerPrincipal) != v.principal {
		return nil, nil
	}
	return s, nil
}

func (v restrictedSavedSearches) GetByName(ctx context.Context, name string) (*SavedSearch, error) {
	if v.principal == "" {
		return nil, principalRead("SavedSearches", "GetByName")
	}
	return v.own(ctx, name)
}

func (v restrictedSavedSearches) List(ctx context.Context, f SavedSearchFilter) ([]*SavedSearch, error) {
	if v.principal == "" {
		return nil, principalRead("SavedSearches", "List")
	}
	f.OwnerPrincipal = v.principal
	return v.inner.List(ctx, f)
}

func (v restrictedSavedSearches) Update(ctx context.Context, s *SavedSearch) error {
	if v.principal == "" {
		return principalRead("SavedSearches", "Update")
	}
	own, err := v.own(ctx, s.Name)
	if err != nil {
		return err
	}
	if own == nil {
		return fmt.Errorf("saved search %q: %w", s.Name, ErrNotFound)
	}
	s.OwnerPrincipal = v.principal
	return v.inner.Update(ctx, s)
}

func (v restrictedSavedSearches) Delete(ctx context.Context, name string) error {
	if v.principal == "" {
		return principalRead("SavedSearches", "Delete")
	}
	own, err := v.own(ctx, name)
	if err != nil {
		return err
	}
	if own == nil {
		return fmt.Errorf("saved search %q: %w", name, ErrNotFound)
	}
	return v.inner.Delete(ctx, name)
}

// restrictedSearchHistory is the SearchHistoryStore of a restricted
// viewer: Append records as the viewer's principal, List is pinned to
// it, and ClearByProfile, which clears every principal's history of a
// profile, refuses. A viewer with no principal refuses.
type restrictedSearchHistory struct {
	inner     SearchHistoryStore
	principal string
}

var _ SearchHistoryStore = restrictedSearchHistory{}

func (v restrictedSearchHistory) Append(ctx context.Context, e *SearchHistoryEntry) error {
	if v.principal == "" {
		return principalRead("SearchHistory", "Append")
	}
	e.OwnerPrincipal = v.principal
	return v.inner.Append(ctx, e)
}

func (v restrictedSearchHistory) List(ctx context.Context, f SearchHistoryFilter) ([]*SearchHistoryEntry, error) {
	if v.principal == "" {
		return nil, principalRead("SearchHistory", "List")
	}
	f.OwnerPrincipal = v.principal
	return v.inner.List(ctx, f)
}

func (v restrictedSearchHistory) ClearByProfile(_ context.Context, _ string) error {
	return restrictedRead("SearchHistory", "ClearByProfile")
}

// restrictedResurfacing is the ResurfacingQueueStore of a restricted
// viewer: Upsert queues as the viewer's principal, List is pinned to
// it, the per-entry and per-profile writes reach the driver. A viewer
// with no principal refuses.
type restrictedResurfacing struct {
	inner     ResurfacingQueueStore
	principal string
}

var _ ResurfacingQueueStore = restrictedResurfacing{}

func (v restrictedResurfacing) Upsert(ctx context.Context, e *ResurfacingEntry) error {
	if v.principal == "" {
		return principalRead("Resurfacing", "Upsert")
	}
	e.OwnerPrincipal = v.principal
	return v.inner.Upsert(ctx, e)
}

func (v restrictedResurfacing) List(ctx context.Context, f ResurfacingFilter) ([]*ResurfacingEntry, error) {
	if v.principal == "" {
		return nil, principalRead("Resurfacing", "List")
	}
	f.OwnerPrincipal = v.principal
	return v.inner.List(ctx, f)
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

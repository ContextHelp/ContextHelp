package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrQuotaExhausted is returned when a registry access event would exceed the
// declared hard limit. No silent fallback — callers must handle this error.
var ErrQuotaExhausted = errors.New("registry quota exhausted")

// defaultWarnPct is the fraction of the hard limit at which a WARN is emitted.
const defaultWarnPct = 0.80

// Meter tracks and enforces metering quotas for paid registry access.
// All methods are safe for concurrent use once wiring (SetQuota,
// SetFailClosed) is done.
type Meter struct {
	store      storage.MeteringStore
	quotas     map[string]map[storage.MeteringEventType]storage.QuotaConfig // registry → event → quota
	failClosed bool
}

// NewMeter creates a Meter backed by the given MeteringStore.
func NewMeter(store storage.MeteringStore) *Meter {
	return &Meter{
		store:  store,
		quotas: make(map[string]map[storage.MeteringEventType]storage.QuotaConfig),
	}
}

// SetQuota registers quota limits for a specific registry + event type combination.
// Call before using RecordEvent to enable quota enforcement.
// Limit == 0 means unlimited.
func (m *Meter) SetQuota(
	registryName string,
	eventType storage.MeteringEventType,
	cfg storage.QuotaConfig,
) {
	if m.quotas[registryName] == nil {
		m.quotas[registryName] = make(map[storage.MeteringEventType]storage.QuotaConfig)
	}
	m.quotas[registryName][eventType] = cfg
}

// SetFailClosed makes quota-check failures deny the event instead of
// proceeding unmetered. Inbound gating on non-private instances sets
// this; outbound registry metering stays advisory (fail-open). Call
// during wiring, before serving.
func (m *Meter) SetFailClosed(on bool) {
	m.failClosed = on
}

// RecordEvent records one metering event for the given registry and event type,
// then enforces quota limits:
//   - When usage >= hard limit: returns ErrQuotaExhausted (event NOT recorded).
//   - When usage >= warn threshold: logs a WARN and records the event.
//   - Otherwise: records the event silently.
//
// With a quota configured, the usage check and the insert run as one
// atomic storage operation (RecordCapped) so concurrent recorders can
// never overshoot the cap. A usage-check failure denies on fail-closed
// meters and logs + records unchecked on advisory ones.
//
// period is the billing period window used to sum counts. A zero period means
// "all time". Pass a non-zero After when limiting to a billing cycle.
func (m *Meter) RecordEvent(
	ctx context.Context,
	registryName string,
	eventType storage.MeteringEventType,
	namespace string,
	periodStart time.Time,
) error {
	event := &storage.MeteringEvent{
		ID:           uuid.New().String(),
		RegistryName: registryName,
		EventType:    eventType,
		Namespace:    namespace,
		Count:        1,
		OccurredAt:   time.Now().UTC(),
	}

	quota, hasQuota := m.quotaFor(registryName, eventType)
	if !hasQuota || quota.Limit <= 0 {
		if err := m.store.Record(ctx, event); err != nil {
			return fmt.Errorf("record metering event: %w", err)
		}
		return nil
	}

	recorded, err := m.store.RecordCapped(ctx, event, periodStart, quota.Limit)
	if err != nil {
		if m.failClosed {
			return fmt.Errorf("metering: quota check failed (fail-closed): %w", err)
		}
		// Advisory meter: log and record unchecked; metering keeps its
		// historical fail-open contract for outbound registry access.
		slog.WarnContext(ctx, "metering: failed to check usage; proceeding without quota check",
			"registry", registryName, "event_type", string(eventType), "err", err)
		if rerr := m.store.Record(ctx, event); rerr != nil {
			return fmt.Errorf("record metering event: %w", rerr)
		}
		return nil
	}
	if !recorded {
		return fmt.Errorf("%w: registry=%s event=%s limit=%d",
			ErrQuotaExhausted, registryName, eventType, quota.Limit)
	}

	// Warn threshold — advisory read after the atomic insert.
	warnAt := quota.WarnAt
	if warnAt == 0 {
		warnAt = int(float64(quota.Limit) * defaultWarnPct)
	}
	if usage, uerr := m.sumUsage(ctx, registryName, eventType, periodStart); uerr == nil && usage >= warnAt {
		slog.WarnContext(ctx, "registry quota warning: approaching limit",
			"registry", registryName,
			"event_type", string(eventType),
			"usage", usage,
			"limit", quota.Limit,
			"warn_at", warnAt,
		)
	}
	return nil
}

// UsageSummary returns aggregated event counts for a registry within a period.
// periodStart == zero means all-time.
func (m *Meter) UsageSummary(
	ctx context.Context,
	registryName string,
	periodStart time.Time,
) ([]*storage.MeteringAggregate, error) {
	f := storage.MeteringFilter{
		RegistryName: registryName,
		After:        periodStart,
	}
	return m.store.Aggregate(ctx, f)
}

// AllUsageSummary returns aggregated counts for all registries within the period.
func (m *Meter) AllUsageSummary(
	ctx context.Context,
	periodStart time.Time,
) ([]*storage.MeteringAggregate, error) {
	f := storage.MeteringFilter{After: periodStart}
	return m.store.Aggregate(ctx, f)
}

// QuotaFor returns the configured quota for a registry+event combination.
// ok=false means no quota is configured (unlimited).
func (m *Meter) QuotaFor(
	registryName string,
	eventType storage.MeteringEventType,
) (storage.QuotaConfig, bool) {
	return m.quotaFor(registryName, eventType)
}

// --- helpers ---

func (m *Meter) quotaFor(
	registryName string,
	eventType storage.MeteringEventType,
) (storage.QuotaConfig, bool) {
	if byReg, ok := m.quotas[registryName]; ok {
		if q, ok := byReg[eventType]; ok {
			return q, true
		}
	}
	return storage.QuotaConfig{}, false
}

func (m *Meter) sumUsage(
	ctx context.Context,
	registryName string,
	eventType storage.MeteringEventType,
	periodStart time.Time,
) (int, error) {
	aggs, err := m.store.Aggregate(ctx, storage.MeteringFilter{
		RegistryName: registryName,
		EventType:    eventType,
		After:        periodStart,
	})
	if err != nil {
		return 0, err
	}
	for _, a := range aggs {
		if a.RegistryName == registryName && a.EventType == eventType {
			return a.Total, nil
		}
	}
	return 0, nil
}

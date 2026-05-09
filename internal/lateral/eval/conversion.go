package eval

import (
	"context"
	"sync"
	"time"

	"hop.top/kit/go/runtime/bus"
)

// ConversionWindow is the default observation window for conversion
// rates: 14 days mirrors the substrate's lifecycle.cold_cycle_days
// default. Operators tune via NewConversionAggregator's options.
const ConversionWindow = 14 * 24 * time.Hour

// ConversionAggregator subscribes to candidate.created and
// candidate.promoted bus events and tracks per-strategy emission +
// promotion counts inside a rolling window.
//
// Distinct from ComputeMetrics: precision/recall measure offline
// dispatch correctness against a labelled set; conversion measures
// production effectiveness — what fraction of candidates a strategy
// emits go on to become Promoted entities. The two together drive
// the per-strategy quality score (T-0333).
//
// ConversionAggregator is safe for concurrent Subscribe / Snapshot.
// Subscribe is intended to be called once at boot; Snapshot is
// called by readers (the per-strategy quality metric, the dashboard
// API, etc.) on each tick.
type ConversionAggregator struct {
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	emits   map[string][]time.Time // strategy ID → emit timestamps
	promots map[string][]time.Time // strategy ID → promotion timestamps
}

// AggregatorOptions configure a ConversionAggregator.
//   - Window: rolling observation window; 0 → ConversionWindow (14d).
//   - Now: clock source; nil → time.Now. Tests override for
//     determinism.
type AggregatorOptions struct {
	Window time.Duration
	Now    func() time.Time
}

// NewConversionAggregator builds a clean aggregator. Subscribe must
// be called separately so callers can inject an existing bus
// instance (production wires the daemon's shared bus.Bus).
func NewConversionAggregator(opts AggregatorOptions) *ConversionAggregator {
	a := &ConversionAggregator{
		window:  opts.Window,
		now:     opts.Now,
		emits:   map[string][]time.Time{},
		promots: map[string][]time.Time{},
	}
	if a.window == 0 {
		a.window = ConversionWindow
	}
	if a.now == nil {
		a.now = time.Now
	}
	return a
}

// Subscribe wires the aggregator to b's candidate.created and
// candidate.promoted topics. Returns an unsubscribe func bundle so
// callers can tear down cleanly (test convenience; production
// daemon's lifecycle holds the subscription for process lifetime).
//
// Payloads are expected to be map[string]any with "strategy_id" key
// (set by daemon.lifecycle.handleCaptureEvent for created; by the
// substrate's promote pipeline for promoted). Unknown shapes are
// ignored; the aggregator never panics on a malformed payload.
func (a *ConversionAggregator) Subscribe(b bus.Bus) []bus.Unsubscribe {
	return []bus.Unsubscribe{
		b.Subscribe("ctxt.lateral.candidate.created", a.onCreated),
		b.Subscribe("ctxt.lateral.candidate.promoted", a.onPromoted),
	}
}

func (a *ConversionAggregator) onCreated(_ context.Context, e bus.Event) error {
	id := strategyIDFromPayload(e.Payload)
	if id == "" {
		return nil
	}
	a.mu.Lock()
	a.emits[id] = append(a.emits[id], a.now())
	a.mu.Unlock()
	return nil
}

func (a *ConversionAggregator) onPromoted(_ context.Context, e bus.Event) error {
	id := strategyIDFromPayload(e.Payload)
	if id == "" {
		return nil
	}
	a.mu.Lock()
	a.promots[id] = append(a.promots[id], a.now())
	a.mu.Unlock()
	return nil
}

func strategyIDFromPayload(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := m["strategy_id"].(string); ok && v != "" {
		return v
	}
	// Some payload shapes use nested Qualifiers / object keys. Be
	// permissive: a "strategy" key is also accepted.
	if v, ok := m["strategy"].(string); ok {
		return v
	}
	return ""
}

// ConversionRate is one strategy's snapshot. Emitted is the count of
// candidates the strategy emitted within the window; Promoted the
// count that reached Promoted within the window. Rate = Promoted /
// Emitted (NaN-safe: zero emits → 0).
type ConversionRate struct {
	StrategyID string  `json:"strategy_id"`
	Emitted    int     `json:"emitted"`
	Promoted   int     `json:"promoted"`
	Rate       float64 `json:"rate"`
}

// Snapshot returns the current per-strategy conversion rate. Safe for
// concurrent calls; the snapshot is a copy.
func (a *ConversionAggregator) Snapshot() map[string]ConversionRate {
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := a.now().Add(-a.window)
	out := map[string]ConversionRate{}

	// Trim per-strategy slices to the window so memory doesn't grow
	// unbounded across long-running snapshots.
	for id, ts := range a.emits {
		ts = trimBefore(ts, cutoff)
		a.emits[id] = ts
	}
	for id, ts := range a.promots {
		ts = trimBefore(ts, cutoff)
		a.promots[id] = ts
	}

	// Strategy IDs we know about: union of emit and promotion keys.
	ids := map[string]struct{}{}
	for id := range a.emits {
		ids[id] = struct{}{}
	}
	for id := range a.promots {
		ids[id] = struct{}{}
	}
	for id := range ids {
		em := len(a.emits[id])
		pr := len(a.promots[id])
		rate := 0.0
		if em > 0 {
			rate = float64(pr) / float64(em)
		}
		out[id] = ConversionRate{
			StrategyID: id,
			Emitted:    em,
			Promoted:   pr,
			Rate:       rate,
		}
	}
	return out
}

func trimBefore(ts []time.Time, cutoff time.Time) []time.Time {
	idx := 0
	for idx < len(ts) && ts[idx].Before(cutoff) {
		idx++
	}
	if idx == 0 {
		return ts
	}
	out := make([]time.Time, len(ts)-idx)
	copy(out, ts[idx:])
	return out
}

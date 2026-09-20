package github

import (
	"context"
	"sync"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/events"
)

// FailureRecorder is the package-local contract for emitting failure
// events. Probes call recordSubpathFailure / recordScanFailure to surface
// transient errors without coupling to a concrete bus implementation.
//
// Default behavior (when no recorder is wired) is silent — failure
// recording is best-effort and never blocks the probe pipeline.
type FailureRecorder interface {
	SubpathFailure(strategyID, objectID, mechanism string, err error)
	ScanFailure(strategyID, objectID, mechanism string, err error)
}

// BusFailureRecorder publishes failure events through a kit bus. Wire
// this from the daemon when a real bus is available; tests that don't
// care about events use the no-op default.
type BusFailureRecorder struct {
	Bus    bus.Bus
	Source string // emitter source string; e.g. "ctxt.lateral.github"
}

// NewBusFailureRecorder wires a bus + source into the recorder. Returns
// a no-op recorder if b is nil.
func NewBusFailureRecorder(b bus.Bus, source string) FailureRecorder {
	if b == nil {
		return noopRecorder{}
	}
	if source == "" {
		source = "ctxt.lateral.github"
	}
	return &BusFailureRecorder{Bus: b, Source: source}
}

// SubpathFailure emits ctxt.lateral.subpath.failed with Mechanism set to
// the strategy ID and Reason set to mechanism (the specific sub-path
// that failed, e.g. "list_repo_siblings"). Subject is the captured
// object ID.
func (r *BusFailureRecorder) SubpathFailure(strategyID, objectID, mechanism string, err error) {
	if r == nil || r.Bus == nil {
		return
	}
	q := bus.Qualifiers{Mechanism: strategyID, Reason: mechanism}
	if err != nil {
		q.Property = err.Error()
	}
	payload := events.FailurePayload{
		Qualifiers: q,
		Topic:      events.SubpathFailed,
		Subject:    objectID,
	}
	_ = r.Bus.Publish(context.Background(), bus.NewEvent(events.SubpathFailed, r.Source, payload))
}

// ScanFailure emits ctxt.lateral.scan.failed with Mechanism set to the
// strategy ID and Reason set to mechanism.
func (r *BusFailureRecorder) ScanFailure(strategyID, objectID, mechanism string, err error) {
	if r == nil || r.Bus == nil {
		return
	}
	q := bus.Qualifiers{Mechanism: strategyID, Reason: mechanism}
	if err != nil {
		q.Property = err.Error()
	}
	payload := events.FailurePayload{
		Qualifiers: q,
		Topic:      events.ScanFailed,
		Subject:    objectID,
	}
	_ = r.Bus.Publish(context.Background(), bus.NewEvent(events.ScanFailed, r.Source, payload))
}

// noopRecorder discards every failure call. Used as the default when no
// bus is wired.
type noopRecorder struct{}

func (noopRecorder) SubpathFailure(string, string, string, error) {}
func (noopRecorder) ScanFailure(string, string, string, error)    {}

// activeRecorder is mutable package state holding the current recorder.
// Probes call recordSubpathFailure / recordScanFailure which read it
// under a mutex. Daemon wiring calls SetFailureRecorder once at startup;
// tests that care about events install a stub.
//
// Process-global mutable state is not ideal, but the alternative
// (threading a recorder through every probe call) inflates the strategy
// surface for a strictly-best-effort observability concern. The mutex
// keeps it race-free under -race.
var (
	recorderMu sync.RWMutex
	recorder   FailureRecorder = noopRecorder{}
)

// SetFailureRecorder installs r as the active package-wide failure
// recorder. Pass a noopRecorder (or nil → noop) to disable recording.
// Safe to call concurrently with probes; subsequent failures use the
// new recorder atomically.
func SetFailureRecorder(r FailureRecorder) {
	recorderMu.Lock()
	defer recorderMu.Unlock()
	if r == nil {
		recorder = noopRecorder{}
		return
	}
	recorder = r
}

// recordSubpathFailure forwards to the active recorder.
func recordSubpathFailure(strategyID, objectID, mechanism string, err error) {
	recorderMu.RLock()
	r := recorder
	recorderMu.RUnlock()
	r.SubpathFailure(strategyID, objectID, mechanism, err)
}

// recordScanFailure forwards to the active recorder.
//
// No probe calls this yet, so scan failures are never emitted, while the
// sibling recordSubpathFailure is wired. The recorder side is complete
// (interface method, bus emitter, noop); the probe-side call is missing.
//
//nolint:unused // recorder path complete; probe-side call site not yet wired
func recordScanFailure(strategyID, objectID, mechanism string, err error) {
	recorderMu.RLock()
	r := recorder
	recorderMu.RUnlock()
	r.ScanFailure(strategyID, objectID, mechanism, err)
}

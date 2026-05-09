// Package lateral wires the lateral discovery pipeline. The retry/backoff for
// readParent uses hop.top/kit/go/core/util.Retry so the schedule stays
// consistent with other kit-backed pipelines and tests can swap RetryConfig
// rather than hand-roll a []time.Duration.
package lateral

import (
	"context"
	"errors"
	"time"

	"hop.top/kit/go/core/util"
	"hop.top/kit/go/runtime/bus"
)

// ErrNotYetReadable indicates the parent object is not yet readable from the
// canonical store. The discover pipeline retries on this error per the
// configured RetryConfig.
var ErrNotYetReadable = errors.New("parent not yet readable")

// Store is the subset of the canonical store that lateral.discover reads.
type Store interface {
	Read(ctx context.Context, id string) (map[string]any, error)
}

// DiscoverConfig wires a Discover's collaborators. RetryConfig (zero value
// uses defaults that approximate 4 attempts ramping 50ms → 3s) governs how
// readParent retries transient ErrNotYetReadable from the canonical store.
type DiscoverConfig struct {
	Bus         bus.Bus
	Registry    *Registry
	Store       Store
	RetryConfig util.RetryConfig
}

// Discover is the top-level lateral pipeline. It subscribes to capture events
// and dispatches strategies; the candidate scoring → resolution → cap gate →
// materialization phases land in later tasks. T03 ships only the subscription
// shell.
type Discover struct {
	cfg     DiscoverConfig
	cancels []bus.Unsubscribe
}

// NewDiscover constructs a Discover. Call Start to subscribe.
func NewDiscover(cfg DiscoverConfig) *Discover { return &Discover{cfg: cfg} }

// Start subscribes Discover to the capture-pipeline events. Subscriptions are
// torn down by Stop. Returns nil; subscription errors (if the underlying bus
// surfaces any) bubble up via the returned Unsubscribe handles.
func (d *Discover) Start(_ context.Context) error {
	d.cancels = append(
		d.cancels,
		d.cfg.Bus.Subscribe("ctxt.ingest.object.persisted", d.handlePersisted),
		d.cfg.Bus.Subscribe("ctxt.ingest.object.captured", d.handleCaptured),
	)
	return nil
}

// Stop tears down all subscriptions.
func (d *Discover) Stop() {
	for _, c := range d.cancels {
		c()
	}
	d.cancels = nil
}

// readParent returns the canonical record for the given object ID using
// kit's util.Retry for backoff. Default RetryConfig (zero value) is 4
// attempts with BaseDelay 50ms, MaxDelay 3s, no jitter — approximating the
// original 50ms/200ms/1s/3s schedule. Context cancellation short-circuits.
func (d *Discover) readParent(ctx context.Context, id string) (map[string]any, error) {
	cfg := d.cfg.RetryConfig
	if cfg == (util.RetryConfig{}) {
		cfg = util.RetryConfig{
			MaxAttempts: 4,
			BaseDelay:   50 * time.Millisecond,
			MaxDelay:    3 * time.Second,
		}
	}
	var rec map[string]any
	err := util.Retry(ctx, cfg, func() error {
		r, err := d.cfg.Store.Read(ctx, id)
		if err != nil {
			return err
		}
		rec = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func (d *Discover) handlePersisted(_ context.Context, _ bus.Event) error {
	// Later task wires this: read parent from canonical store via readParent,
	// dispatch strategies, score, gate, materialize.
	return nil
}

func (d *Discover) handleCaptured(_ context.Context, _ bus.Event) error {
	// Later task wires this: same flow as handlePersisted, but parent read
	// goes through readParent's full retry schedule (the .captured event
	// arrives before persistence is confirmed).
	return nil
}

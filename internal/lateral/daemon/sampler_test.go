package daemon_test

import (
	"context"
	"fmt"
	"testing"

	"hop.top/kit/go/runtime/bus"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	adapterbus "github.com/ideacrafterslabs/ctxt/internal/lateral/adapters/bus"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/daemon"
)

// TestLifecycle_SamplerShapesTraffic dispatches 200 distinct ObjectIDs
// at a 50% sample percent and asserts the strategy fires on a
// roughly half-sized subset.
func TestLifecycle_SamplerShapesTraffic(t *testing.T) {
	t.Parallel()

	reg := lateral.NewRegistry()
	st := &countingStrategy{id: "XStrategy", family: lateral.FamilyPlatform}
	reg.Register(st)

	b := bus.New(bus.WithEnforce(bus.ModeOff))
	defer b.Close(context.Background())
	pub := adapterbus.New(b)

	lc, err := daemon.NewLifecycle(daemon.LifecycleOptions{
		Bus: b, Registry: reg, Publisher: pub,
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	lc.ApplySampler(daemon.Config{
		SamplePercent: map[string]int{"XStrategy": 50},
	})

	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("event-%d", i)
		_ = lc.EnqueueDirect(context.Background(),
			lateral.CapturedEvent{ObjectID: id, SourceURL: "https://x"})
	}
	// Bounds: 50% of 200 ≈ 100. Allow [60, 140] for hash variance.
	if st.calls < 60 || st.calls > 140 {
		t.Errorf("calls = %d/200; want [60,140] for 50%% sample", st.calls)
	}
}

// TestLifecycle_SamplerSwapsAtomicallyOnReload proves ApplySampler
// updates the live shape: at 100% every event fires; flipping to 0%
// quiets all subsequent dispatches.
func TestLifecycle_SamplerSwapsAtomicallyOnReload(t *testing.T) {
	t.Parallel()

	reg := lateral.NewRegistry()
	st := &countingStrategy{id: "XStrategy", family: lateral.FamilyPlatform}
	reg.Register(st)

	b := bus.New(bus.WithEnforce(bus.ModeOff))
	defer b.Close(context.Background())
	pub := adapterbus.New(b)

	lc, err := daemon.NewLifecycle(daemon.LifecycleOptions{
		Bus: b, Registry: reg, Publisher: pub,
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	lc.ApplySampler(daemon.Config{
		SamplePercent: map[string]int{"XStrategy": 100},
	})

	for i := 0; i < 10; i++ {
		_ = lc.EnqueueDirect(context.Background(),
			lateral.CapturedEvent{ObjectID: fmt.Sprintf("e-%d", i), SourceURL: "https://x"})
	}
	if st.calls != 10 {
		t.Fatalf("after 100%% sample: calls = %d; want 10", st.calls)
	}

	// Reload to zero — subsequent dispatches must skip.
	lc.ApplySampler(daemon.Config{
		SamplePercent: map[string]int{"XStrategy": 0},
	})
	for i := 0; i < 10; i++ {
		_ = lc.EnqueueDirect(context.Background(),
			lateral.CapturedEvent{ObjectID: fmt.Sprintf("e2-%d", i), SourceURL: "https://x"})
	}
	if st.calls != 10 {
		t.Errorf("after 0%% sample: calls = %d; want still 10", st.calls)
	}
}

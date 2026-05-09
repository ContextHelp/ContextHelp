package daemon_test

import (
	"context"
	"testing"

	"hop.top/kit/go/runtime/bus"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	adapterbus "github.com/ideacrafterslabs/ctxt/internal/lateral/adapters/bus"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/daemon"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

func TestConfig_GateConfigCoversCommonStrategies(t *testing.T) {
	t.Parallel()

	cfg := daemon.Config{
		JIT: jit.Config{Enabled: true},
	}
	gate := cfg.GateConfig()
	// jit follows JIT.Enabled.
	if v, ok := gate["jit"]; !ok || !v {
		t.Errorf("jit = %v, ok=%v; want true", v, ok)
	}
	// Roster default-enabled.
	if v := gate["XStrategy"]; !v {
		t.Errorf("XStrategy = %v; want default-true", v)
	}
	if v := gate["GoogleSearchStrategy"]; !v {
		t.Errorf("GoogleSearchStrategy = %v; want default-true", v)
	}
}

func TestConfig_GateConfigRespectsExplicitFalse(t *testing.T) {
	t.Parallel()

	off := false
	cfg := daemon.Config{}
	cfg.Roster.XStrategy = &off
	gate := cfg.GateConfig()
	if v := gate["XStrategy"]; v {
		t.Errorf("XStrategy = %v; want false (explicitly disabled)", v)
	}
}

// TestApplyGate_FlipsRuntime simulates a SIGHUP reload that turns
// XStrategy off. The first dispatch fires; the second skips.
func TestApplyGate_FlipsRuntime(t *testing.T) {
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

	// Default cfg → all roster default-enabled → XStrategy fires.
	lc.ApplyGate(daemon.Config{})
	if err := lc.EnqueueDirect(context.Background(),
		lateral.CapturedEvent{ObjectID: "1", SourceURL: "https://x"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if st.calls != 1 {
		t.Fatalf("first dispatch calls = %d; want 1", st.calls)
	}

	// Simulate reload that flipped XStrategy off.
	off := false
	cfg2 := daemon.Config{}
	cfg2.Roster.XStrategy = &off
	lc.ApplyGate(cfg2)
	if err := lc.EnqueueDirect(context.Background(),
		lateral.CapturedEvent{ObjectID: "2", SourceURL: "https://x"}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if st.calls != 1 {
		t.Errorf("after reload-off: calls = %d; want still 1", st.calls)
	}
}

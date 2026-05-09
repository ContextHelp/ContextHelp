package daemon_test

import (
	"context"
	"testing"

	"hop.top/kit/go/runtime/bus"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	adapterbus "github.com/ideacrafterslabs/ctxt/internal/lateral/adapters/bus"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/daemon"
)

// TestLifecycle_KillSwitchTriggerCutsOff proves Trigger stops new
// dispatches without waiting for a config reload.
func TestLifecycle_KillSwitchTriggerCutsOff(t *testing.T) {
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

	// First dispatch: strategy fires.
	if err := lc.EnqueueDirect(context.Background(),
		lateral.CapturedEvent{ObjectID: "1", SourceURL: "https://x"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if st.calls != 1 {
		t.Fatalf("first calls = %d; want 1", st.calls)
	}

	// Trigger the kill-switch — no SIGHUP, no YAML edit.
	lc.KillSwitch().Trigger("XStrategy")
	if err := lc.EnqueueDirect(context.Background(),
		lateral.CapturedEvent{ObjectID: "2", SourceURL: "https://x"}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if st.calls != 1 {
		t.Errorf("after kill-switch: calls = %d; want still 1", st.calls)
	}

	if !lc.KillSwitch().Triggered("XStrategy") {
		t.Error("Triggered should report true")
	}

	// Restore.
	lc.KillSwitch().Restore("XStrategy")
	if err := lc.EnqueueDirect(context.Background(),
		lateral.CapturedEvent{ObjectID: "3", SourceURL: "https://x"}); err != nil {
		t.Fatalf("third: %v", err)
	}
	if st.calls != 2 {
		t.Errorf("after restore: calls = %d; want 2", st.calls)
	}
}

// TestLifecycle_KillSwitchPersistsThroughReload proves that an
// operator's emergency Trigger doesn't get clobbered by a SIGHUP that
// re-reads the (still-says-enabled) YAML — the next config reload
// re-asserts whatever the YAML says, which is the correct behaviour
// (operators should also edit the YAML when they want the kill to
// stick across reloads).
func TestLifecycle_KillSwitchOverriddenByReload(t *testing.T) {
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

	lc.KillSwitch().Trigger("XStrategy")
	if !lc.KillSwitch().Triggered("XStrategy") {
		t.Fatal("kill-switch should be triggered")
	}

	// Operator forgot to edit YAML — SIGHUP reload restores from
	// default-enabled config. This is documented behaviour: the
	// kill-switch is operational, not durable.
	lc.ApplyGate(daemon.Config{})
	if lc.KillSwitch().Triggered("XStrategy") {
		t.Error("after reload from default-enabled YAML, kill-switch state should be cleared")
	}
}

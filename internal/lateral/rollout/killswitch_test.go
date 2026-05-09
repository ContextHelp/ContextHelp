package rollout

import "testing"

func TestKillSwitch_TriggerCutsOff(t *testing.T) {
	t.Parallel()
	g := NewStrategyGate()
	k := NewKillSwitch(g)

	if !g.Allowed("github") {
		t.Fatal("default-allow")
	}
	k.Trigger("github")
	if g.Allowed("github") {
		t.Error("after Trigger, gate should deny")
	}
	if !k.Triggered("github") {
		t.Error("Triggered should report true")
	}
}

func TestKillSwitch_RestoreReenables(t *testing.T) {
	t.Parallel()
	g := NewStrategyGate()
	k := NewKillSwitch(g)

	k.Trigger("jit")
	k.Restore("jit")
	if !g.Allowed("jit") {
		t.Error("after Restore, gate should allow")
	}
	if k.Triggered("jit") {
		t.Error("Triggered should report false after restore")
	}
}

func TestKillSwitch_NilSafe(t *testing.T) {
	t.Parallel()
	var k *KillSwitch
	k.Trigger("any")
	k.Restore("any")
	if k.Triggered("any") {
		t.Error("nil killswitch should report not-triggered")
	}
}

func TestKillSwitch_SharesGateState(t *testing.T) {
	t.Parallel()
	g := NewStrategyGate()
	k := NewKillSwitch(g)

	// Set via gate, observe via KillSwitch.
	g.Set("x", false)
	if !k.Triggered("x") {
		t.Error("gate.Set(false) should be visible as Triggered")
	}
	// Set via KillSwitch, observe via gate.
	k.Trigger("y")
	if g.Allowed("y") {
		t.Error("KillSwitch.Trigger should be visible as gate.Allowed=false")
	}
}

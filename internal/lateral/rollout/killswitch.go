package rollout

// KillSwitch is the operator emergency lever: cold-stop a strategy
// without restarting the daemon and without waiting for the next
// SIGHUP cycle. Distinct from StrategyGate's regular enabled flag
// because:
//
//   - StrategyGate flips on SIGHUP-driven config reload — the
//     operator edits YAML and signals the daemon. Quick but not
//     instant.
//   - KillSwitch.Trigger flips immediately, in-process. Useful for
//     "this strategy is hammering an upstream API; stop it NOW"
//     scenarios where the YAML round-trip is too slow.
//
// Implementation note: KillSwitch is a thin wrapper over StrategyGate
// — they share the same atomic state. Trigger(id) is equivalent to
// gate.Set(id, false) but expresses the operational intent more
// clearly in dashboards / runbook references.
type KillSwitch struct {
	gate *StrategyGate
}

// NewKillSwitch wraps gate. The KillSwitch and the gate share state;
// flips through one are visible through the other. Pass the same
// *StrategyGate the daemon's Lifecycle holds.
func NewKillSwitch(gate *StrategyGate) *KillSwitch {
	return &KillSwitch{gate: gate}
}

// Trigger cold-stops strategy id. Subsequent Lifecycle.Dispatch calls
// skip it. Already-running Probe goroutines are NOT cancelled — they
// run to completion. This is intentional: aborting a Probe mid-flight
// could leak resources (open HTTP connections, partial cache writes).
// The operational claim is "no NEW probes after Trigger returns,"
// which the gate's atomic semantics satisfy.
func (k *KillSwitch) Trigger(id string) {
	if k == nil || k.gate == nil {
		return
	}
	k.gate.Set(id, false)
}

// Restore re-enables strategy id. Useful for runbook recovery flows
// after a triggered kill-switch — operators flip the strategy back
// on once the upstream issue clears.
func (k *KillSwitch) Restore(id string) {
	if k == nil || k.gate == nil {
		return
	}
	k.gate.Set(id, true)
}

// Triggered reports whether the kill-switch has fired for id.
// Equivalent to !gate.Allowed(id) but with explicit semantics.
func (k *KillSwitch) Triggered(id string) bool {
	if k == nil || k.gate == nil {
		return false
	}
	return !k.gate.Allowed(id)
}

// Package rollout owns the runtime knobs operators use to gate a
// strategy without restarting the daemon: enable flags, percentage
// traffic shaping, kill-switch.
//
// Each knob is implemented as a small primitive callers compose at
// dispatch time. The daemon's Lifecycle holds one StrategyGate +
// one Sampler instance per process; both are mutated in place on
// SIGHUP reload so already-running goroutines see the new
// gate/sample state on their next Dispatch.
//
//   - StrategyGate (gate.go): per-strategy enabled flag, atomically
//     reloadable. Dispatch consults Allowed(strategyID) before
//     invoking Probe.
//   - Sampler (sample.go): hash-based traffic shaping. Returns true
//     for `Percent` of stable input IDs.
//   - KillSwitch (killswitch.go): opt-in cold-stop on top of
//     StrategyGate; flips a strategy off mid-run without waiting
//     for the next dispatch tick.
//
// All three are zero-value safe: an unconfigured gate allows
// everything; an unset sampler routes everything; an empty kill list
// affects no one. This means existing daemon wiring stays a no-op
// until operators set knobs.
package rollout

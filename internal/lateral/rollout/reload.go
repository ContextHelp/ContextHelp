package rollout

// GateConfig is the daemon-side flat shape mapped from the layered
// config's strategies block. Reload (T-0328) translates the typed
// daemon.Config gates into this map and applies it to a StrategyGate.
//
// Wire-shape compatibility: the keys mirror the substrate's strategy
// IDs exactly (e.g. "GitHubStrategy", "GoogleSearchStrategy", "jit").
// Operators flipping `lateral.strategies.<name>.enabled = false` see
// the corresponding gate go false on the next SIGHUP.
type GateConfig map[string]bool

// Apply replaces gate's flags with cfg. Existing gate state is
// discarded — Reload is a full overwrite, not a merge, so removing
// a key from the YAML restores the default-allowed posture.
func (cfg GateConfig) Apply(gate *StrategyGate) {
	if gate == nil {
		return
	}
	gate.SetAll(cfg)
}

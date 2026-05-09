package rollout

import (
	"hash/fnv"
)

// Sampler shapes per-strategy traffic by hashing a stable input ID
// (typically lateral.CapturedEvent.ObjectID) and comparing against a
// percent threshold. Zero or unconfigured percent = full traffic.
//
// Per-strategy percent comes from cfg.<Strategy>.SamplePercent in
// the daemon's layered config. The default is 100 so existing
// deployments are unaffected by sampler introduction.
//
// The hash is stable across process restarts (FNV-1a 32-bit, no salt)
// so an event that lands in the sampled fraction at t0 will land
// there again at t1. This matters for replays + for operators
// reasoning about what fraction of production has seen a strategy.
type Sampler struct {
	// percent maps strategy ID → 0..100 inclusive. Missing key =
	// 100 (full traffic) for that strategy.
	percent map[string]int
}

// NewSampler builds a Sampler from a per-strategy percent map. Values
// outside [0,100] clamp to [0,100]. nil flags = empty Sampler (full
// traffic to every strategy).
func NewSampler(flags map[string]int) *Sampler {
	if flags == nil {
		return &Sampler{percent: map[string]int{}}
	}
	out := make(map[string]int, len(flags))
	for k, v := range flags {
		switch {
		case v < 0:
			v = 0
		case v > 100:
			v = 100
		}
		out[k] = v
	}
	return &Sampler{percent: out}
}

// Allow returns true if the strategy should run on input id. Pure
// (no I/O); deterministic for the same (strategyID, id) pair.
func (s *Sampler) Allow(strategyID, id string) bool {
	if s == nil {
		return true
	}
	pct, ok := s.percent[strategyID]
	if !ok {
		// Default: full traffic when no override.
		return true
	}
	if pct >= 100 {
		return true
	}
	if pct <= 0 {
		return false
	}
	h := fnv.New32a()
	// Mix the strategy ID into the hash so two strategies with the
	// same percent on the same event don't collude (one routes only
	// IDs the other also routed).
	_, _ = h.Write([]byte(strategyID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(id))
	return int(h.Sum32()%100) < pct
}

// Snapshot returns the configured per-strategy percent map (copied,
// safe to mutate).
func (s *Sampler) Snapshot() map[string]int {
	if s == nil {
		return nil
	}
	out := make(map[string]int, len(s.percent))
	for k, v := range s.percent {
		out[k] = v
	}
	return out
}

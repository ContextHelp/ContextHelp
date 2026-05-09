package jit

import (
	kitdomain "hop.top/kit/go/runtime/domain"

	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Config is the JIT strategy's daemon-side configuration block. The daemon
// loads it from whatever config layer it owns (typically `lateral.strategies.jit`
// in the same YAML hierarchy substrate's config package consumes) and passes
// the result to Register.
//
// Enabled is the gate: false → strategy not registered, no LLM calls, no
// fetches, zero overhead. Default is false so JIT is opt-in rather than
// silently active in fresh deployments.
type Config struct {
	Enabled bool `yaml:"enabled"`
}

// Deps bundles the collaborators the daemon constructs and hands to Register.
// All fields except Outage and Classifier are required — Outage defaults to
// NoOutage() and Classifier defaults to a no-op classifier (empty page-type)
// when nil. Publisher is also optional (events suppressed when nil).
type Deps struct {
	Proposer   Proposer
	Cache      ProposalCache
	Fetcher    Fetcher
	Publisher  kitdomain.EventPublisher
	Outage     OutageDetector
	Classifier PageTypeClassifier
}

// Register wires the JIT strategy into reg when cfg.Enabled is true. Returns
// the constructed strategy and ok=true on registration; (nil, false) when
// disabled. The daemon should still call Register on a disabled config —
// the (nil, false) return makes the gate decision visible in observability
// and lets callers branch on registration without re-reading cfg.
//
// Caller's responsibility:
//   - construct Deps.Proposer (kit/ai/llm.Completer adapter), Cache
//     (NewMemoryProposalCache or persistent variant), Fetcher (ibr adapter)
//   - pick or skip Publisher / Outage / Classifier based on operational needs
//   - drive the gate via Config — the helper itself only branches on cfg.Enabled
//
// Register PANICS when cfg.Enabled is true but a required dep (Proposer,
// Cache, Fetcher) is nil. Required deps are required at startup; failing
// loud beats a silent zero-candidate JIT in production.
func Register(reg *lateral.Registry, cfg Config, deps Deps) (*Strategy, bool) {
	if !cfg.Enabled {
		return nil, false
	}
	if deps.Proposer == nil {
		panic("jit.Register: cfg.Enabled but Deps.Proposer is nil")
	}
	if deps.Cache == nil {
		panic("jit.Register: cfg.Enabled but Deps.Cache is nil")
	}
	if deps.Fetcher == nil {
		panic("jit.Register: cfg.Enabled but Deps.Fetcher is nil")
	}

	proposer := NewCachedProposer(deps.Proposer, deps.Cache)
	executor := NewExecutor(deps.Fetcher)
	outage := deps.Outage
	if outage == nil {
		outage = NoOutage()
	}
	strategy := New(proposer, executor, deps.Publisher, outage, deps.Classifier)
	reg.Register(strategy)
	return strategy, true
}

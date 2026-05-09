package github

import (
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Config gates the github family registration. Mirrors the per-
// strategy enable knobs from the lateral spec amendment:
//
//   lateral:
//     strategies:
//       GitHubStrategy:           { enabled: true }
//       GistStrategy:             { enabled: true }
//       SecurityAdvisoryStrategy: { enabled: true }
//
// Each child can be independently disabled. The substrate's
// dispatcher tolerates registering the parent without the children
// (the parent claims everything as the lowest-specificity match).
//
// The substrate config (internal/lateral/config) doesn't yet ship a
// strategies block — Config here is the package-local view; the
// daemon translates from lateral-config to this struct. Decoupling:
// the github pkg never imports internal/lateral/config (no circular
// dep on lateral substrate).
type Config struct {
	// EnableParent registers GitHubStrategy when true.
	EnableParent bool
	// EnableGist registers GistStrategy when true.
	EnableGist bool
	// EnableSecurityAdvisory registers SecurityAdvisoryStrategy when true.
	EnableSecurityAdvisory bool
	// FloorWindow controls the trailing window for the rate-limit
	// dynamic-floor tracker Register builds when deps.Floor is nil.
	// Ignored when deps.Floor is non-nil. Zero falls back to 4h
	// (NewFloorTracker default) when at least one floor knob is set.
	FloorWindow time.Duration
	// FloorBounds clamps the floor adjustment in [MinPct, MaxPct] for
	// the tracker Register builds when deps.Floor is nil. Ignored when
	// deps.Floor is non-nil. Zero falls back to DefaultFloorBounds
	// (5%/90%).
	FloorBounds FloorBounds
}

// DefaultConfig returns the spec defaults: all three strategies
// enabled, default floor window + bounds.
func DefaultConfig() Config {
	return Config{
		EnableParent:           true,
		EnableGist:             true,
		EnableSecurityAdvisory: true,
		FloorWindow:            4 * time.Hour,
		FloorBounds:            DefaultFloorBounds(),
	}
}

// Registrar is the substrate Registry shape Register depends on. The
// real lateral.Registry satisfies it; tests inject a stub for unit
// coverage of the wiring helper without spinning up a substrate.
//
// Decoupling: we accept an interface here rather than the concrete
// *lateral.Registry so the daemon can swap in adapters / observability
// shims without forking this package.
type Registrar interface {
	Register(s lateral.LateralStrategy)
}

// SharedDeps bundles the dependencies shared across all three github
// strategies. The daemon constructs this once and passes it to
// Register; Register fans it out to per-strategy Dependencies.
type SharedDeps struct {
	APIClient APIClient
	Fetcher   Fetcher
	Breaker   Breaker
	Floor     *FloorTracker

	// FailureRecorder is wired into the package-global recorder via
	// SetFailureRecorder(). Pass nil to use the noop default.
	FailureRecorder FailureRecorder
}

// Register registers GitHubStrategy + (optionally) GistStrategy +
// SecurityAdvisoryStrategy into the substrate registry, gated by cfg.
//
// Behavior:
//   - When cfg.EnableParent == false, NONE of the github strategies
//     register (the parent is the universal entry point; disabling it
//     means disabling the family). Children-only registrations would
//     orphan plain-github URLs to JIT, which is wrong for v1.
//   - When cfg.EnableGist == true, GistStrategy registers in addition.
//   - When cfg.EnableSecurityAdvisory == true, SecurityAdvisoryStrategy
//     registers in addition.
//   - APIClient is required when any strategy is registered — strategies
//     can run in skeleton mode (deps.APIClient = nil) but in production
//     the daemon always wires one. Register panics if APIClient is nil
//     and any strategy is enabled, mirroring the fail-loud-at-init
//     pattern the substrate uses for unknown families.
//   - Breaker + Floor are optional. When wired, the APIClient passed to
//     each strategy is wrapped via NewGuardedAPI. When deps.Floor is nil
//     and cfg.FloorWindow / cfg.FloorBounds carry a non-zero value,
//     Register constructs a FloorTracker from those cfg knobs and uses
//     it for the wrap.
//   - FailureRecorder is installed via SetFailureRecorder; pass nil to
//     use the noop default.
//
// Returns the count of registered strategies for log/test inspection.
func Register(reg Registrar, cfg Config, deps SharedDeps) int {
	if !cfg.EnableParent {
		return 0
	}

	if deps.APIClient == nil {
		panic(fmt.Sprintf(
			"github.Register: APIClient is required when EnableParent=true; "+
				"cfg=%+v, gist=%v, advisory=%v",
			cfg, cfg.EnableGist, cfg.EnableSecurityAdvisory))
	}

	// Always install the recorder. SetFailureRecorder treats nil as
	// "reset to noop", which matches the docstring on SharedDeps.
	// Calling unconditionally avoids leaking a previously installed
	// recorder across Register invocations (tests, hot reload).
	SetFailureRecorder(deps.FailureRecorder)

	// Construct a FloorTracker from cfg when caller hasn't supplied one.
	// Daemon wiring shares a single tracker across the registry by passing
	// deps.Floor directly; standalone callers (tests, embedded use) lean
	// on cfg.FloorWindow/FloorBounds and let Register build the tracker.
	floor := deps.Floor
	if floor == nil && (cfg.FloorWindow > 0 || cfg.FloorBounds != (FloorBounds{})) {
		floor = NewFloorTracker(cfg.FloorWindow, cfg.FloorBounds)
	}

	api := deps.APIClient
	if deps.Breaker != nil || floor != nil {
		// Wrap with guardedAPI when either gate is wired.
		api = NewGuardedAPI(deps.APIClient, deps.Breaker, floor)
	}

	stratDeps := Dependencies{
		Fetcher:   deps.Fetcher,
		APIClient: api,
	}

	count := 0

	parent := NewGitHubStrategy(stratDeps)
	reg.Register(parent)
	count++

	if cfg.EnableGist {
		reg.Register(NewGistStrategy(stratDeps))
		count++
	}
	if cfg.EnableSecurityAdvisory {
		reg.Register(NewSecurityAdvisoryStrategy(stratDeps))
		count++
	}

	return count
}

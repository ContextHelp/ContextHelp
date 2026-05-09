// Package breaker adapts hop.top/kit/go/core/breaker.Breaker into the
// lateral strategies' two consumer interfaces:
//
//   - jit.OutageDetector: IsOutage() bool — reports whether the LLM is
//     currently unavailable so JIT serves cached recipes with the
//     llm_outage circumstance instead of retrying.
//   - github.Breaker: Allow() error + Record(success bool, n int64) —
//     gates github API fetches behind the rate-limit / failure-rate
//     state machine.
//
// One kit Breaker per upstream service maps to one Adapter; the daemon
// constructs N breakers (LLM, GitHub-API, future per-platform) and
// wraps each.
//
// Decoupling: lateral never imports kit/core/breaker directly. This is
// the ONLY package where the kit type crosses into lateral's domain.
package breaker

import (
	kitbreaker "hop.top/kit/go/core/breaker"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/github"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// KitBreaker is the subset of kit/core/breaker.Breaker the adapter
// depends on. Defining it as a local interface lets tests inject a
// stub without instantiating a full kit Breaker (which uses the
// failsafe-go state machine and registers itself globally — heavy for
// small unit tests).
//
// kit/core/breaker.Breaker satisfies KitBreaker verbatim.
type KitBreaker interface {
	Allow() error
	Record(success bool, n int64)
	State() kitbreaker.State
}

// Adapter wraps a kit Breaker so a single instance can be handed to
// every strategy interface that needs the breaker signal — the same
// upstream (e.g. the LLM service) is one breaker, observed by both
// jit.OutageDetector ("am I open?") and any code path that wants to
// gate calls via Allow() / Record().
type Adapter struct {
	inner KitBreaker
}

// New wraps b. Panics on nil — fail loud at construction; nil
// breakers in production are a wiring bug, not a runtime condition.
func New(b KitBreaker) *Adapter {
	if b == nil {
		panic("breaker.New: kit breaker is nil")
	}
	return &Adapter{inner: b}
}

// jit.OutageDetector contract.
var _ jit.OutageDetector = (*Adapter)(nil)

// IsOutage reports whether the underlying breaker is in the Open state
// (executions short-circuit). HalfOpen is treated as "still in outage"
// — the probe state means we're testing recovery; cached recipes are
// the safer default. Closed → false.
func (a *Adapter) IsOutage() bool {
	switch a.inner.State() {
	case kitbreaker.Open, kitbreaker.HalfOpen:
		return true
	default:
		return false
	}
}

// github.Breaker contract.
var _ github.Breaker = (*Adapter)(nil)

// Allow delegates to the kit breaker. Closed → nil; Open / HalfOpen
// → ErrBrokenCircuit. The github guardedAPI wrapper joins this with
// github.ErrBreakerOpen so callers can errors.Is on either sentinel.
func (a *Adapter) Allow() error {
	return a.inner.Allow()
}

// Record forwards success/failure outcomes plus byte count. Callers
// that don't track bytes pass 0; kit's "ops" counter still increments.
func (a *Adapter) Record(success bool, n int64) {
	a.inner.Record(success, n)
}

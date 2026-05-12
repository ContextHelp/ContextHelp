package jit

// OutageDetector is the lateral-side abstraction for "the upstream LLM is
// currently unavailable." Implementations are host-wired; the production
// adapter wraps a kit/core/breaker.Breaker (Open state → IsOutage()=true).
// Tests use a fixed-state fake.
//
// Decoupled from breaker so the jit package stays domain-focused and the
// outage signal can be sourced from anywhere (manual flag, external probe,
// telemetry threshold) without dragging the breaker package into domain code.
type OutageDetector interface {
	IsOutage() bool
}

// outageOff is the default detector when none is wired. IsOutage returns
// false unconditionally so cached recipes are served without the outage
// circumstance.
type outageOff struct{}

func (outageOff) IsOutage() bool { return false }

// NoOutage is the safe default OutageDetector for callers that don't have
// a real outage signal source (tests, single-host setups, daemon wiring
// not yet complete). Always reports IsOutage()=false.
func NoOutage() OutageDetector { return outageOff{} }

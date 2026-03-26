// Package telemetry provides a minimal telemetry stub that honours user
// privacy preferences. Default: disabled. A future real implementation only
// needs to swap the return value in New() once both kill-switches are clear.
package telemetry

import (
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

const (
	// EnvDisableTelemetry is the env var kill-switch.
	// Set to "true", "1", or "yes" to disable telemetry.
	EnvDisableTelemetry = "CH_DISABLE_TELEMETRY"
)

// Telemetry is the minimal interface for event tracking.
type Telemetry interface {
	// Track records a named event with optional properties.
	// Implementations MUST be safe to call concurrently.
	Track(event string, props map[string]any)

	// Flush ensures any buffered events are sent before process exit.
	Flush() error
}

// NoopTelemetry silently discards all events. Zero-allocation hot path.
type NoopTelemetry struct{}

// Track implements Telemetry. Does nothing.
func (NoopTelemetry) Track(_ string, _ map[string]any) {}

// Flush implements Telemetry. Always succeeds.
func (NoopTelemetry) Flush() error { return nil }

// isEnvDisabled returns true when the kill-switch env var is set to a truthy
// value ("true", "1", "yes" — case-insensitive).
func isEnvDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvDisableTelemetry)))
	return v == "true" || v == "1" || v == "yes"
}

// New returns a Telemetry implementation that honours the kill-switches:
//  1. CH_DISABLE_TELEMETRY env var (truthy value → disabled)
//  2. privacy.telemetry: false in config
//
// Default behaviour: disabled (local-first, privacy-respecting).
// When a real implementation is added, swap the final return to it only when
// both checks pass.
func New(cfg *config.Config) Telemetry {
	// Env var kill-switch takes priority.
	if isEnvDisabled() {
		return NoopTelemetry{}
	}

	// Config kill-switch.
	if cfg != nil && !cfg.Privacy.Telemetry {
		return NoopTelemetry{}
	}

	// Default: noop until a real backend is wired.
	return NoopTelemetry{}
}

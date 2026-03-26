package telemetry_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/telemetry"
)

func TestNoopTelemetryDoesNothing(t *testing.T) {
	n := telemetry.NoopTelemetry{}

	// Track must not panic and must be callable multiple times.
	n.Track("test.event", map[string]any{"key": "value"})
	n.Track("test.event", nil)
	n.Track("", nil)

	if err := n.Flush(); err != nil {
		t.Fatalf("Flush() returned unexpected error: %v", err)
	}
}

func TestTelemetryDisabledByEnv(t *testing.T) {
	t.Setenv(telemetry.EnvDisableTelemetry, "true")

	cfg := &config.Config{
		Privacy: config.PrivacyConfig{Telemetry: true}, // config says enabled
	}
	tel := telemetry.New(cfg)

	// Must still be a noop — env var wins.
	tel.Track("should.be.dropped", nil)
	if err := tel.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	// Verify it is indeed a NoopTelemetry (or at least does nothing).
	if _, ok := tel.(telemetry.NoopTelemetry); !ok {
		t.Error("expected NoopTelemetry when CH_DISABLE_TELEMETRY=true")
	}
}

func TestTelemetryDisabledByConfig(t *testing.T) {
	// Ensure env var is unset so config is the deciding factor.
	t.Setenv(telemetry.EnvDisableTelemetry, "")

	cfg := &config.Config{
		Privacy: config.PrivacyConfig{Telemetry: false},
	}
	tel := telemetry.New(cfg)

	tel.Track("should.be.dropped", nil)
	if err := tel.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	if _, ok := tel.(telemetry.NoopTelemetry); !ok {
		t.Error("expected NoopTelemetry when privacy.telemetry=false")
	}
}

func TestTelemetryEnvVariants(t *testing.T) {
	variants := []string{"true", "1", "yes", "TRUE", "Yes", "YES"}
	for _, v := range variants {
		t.Run(v, func(t *testing.T) {
			t.Setenv(telemetry.EnvDisableTelemetry, v)
			tel := telemetry.New(nil)
			if _, ok := tel.(telemetry.NoopTelemetry); !ok {
				t.Errorf("CH_DISABLE_TELEMETRY=%q: expected NoopTelemetry", v)
			}
		})
	}
}

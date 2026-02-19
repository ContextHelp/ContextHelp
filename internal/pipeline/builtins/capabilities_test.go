package builtins

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

func TestCapabilitiesFromFactory_NilFactory(t *testing.T) {
	caps := CapabilitiesFromFactory(nil)
	if len(caps) != 0 {
		t.Errorf("nil factory should return empty caps, got %v", caps)
	}
}

func TestCapabilitiesFromFactory_StubsOnly(t *testing.T) {
	f := providers.NewFactory(config.ProvidersConfig{
		OCR:           config.ProviderBackendConfig{Backend: "stub"},
		Vision:        config.ProviderBackendConfig{Backend: "stub"},
		Transcription: config.ProviderBackendConfig{Backend: "stub"},
		Diarization:   config.ProviderBackendConfig{Backend: "stub"},
	})
	caps := CapabilitiesFromFactory(f)
	if !caps["io"] {
		t.Error("io capability should always be present")
	}
	for _, name := range []string{"ocr", "vision", "transcription", "diarization"} {
		if caps[name] {
			t.Errorf("%s should not be available with stub backend", name)
		}
	}
}

func TestCapabilitiesFromFactory_IOAlwaysPresent(t *testing.T) {
	f := providers.NewFactory(config.ProvidersConfig{})
	caps := CapabilitiesFromFactory(f)
	if !caps["io"] {
		t.Error("io capability should always be present")
	}
}

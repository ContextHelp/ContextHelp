package builtins

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

func TestCapabilitiesFromFactory_NilFactory(t *testing.T) {
	caps := CapabilitiesFromFactory(nil)
	// io is always available regardless of factory; provider-backed caps are not.
	if !caps["io"] {
		t.Error("io capability should always be present even with nil factory")
	}
	for _, name := range []string{"ocr", "vision", "transcription", "diarization", "llm"} {
		if caps[name] {
			t.Errorf("%s should not be available with nil factory", name)
		}
	}
}

func TestCapabilitiesFromFactory_StubsOnly(t *testing.T) {
	f := providers.NewFactory(config.ProvidersConfig{
		OCR:           config.ProviderBackendConfig{Backend: "stub"},
		Vision:        config.ProviderBackendConfig{Backend: "stub"},
		Transcription: config.ProviderBackendConfig{Backend: "stub"},
		Diarization:   config.ProviderBackendConfig{Backend: "stub"},
	}, nil)
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
	f := providers.NewFactory(config.ProvidersConfig{}, nil)
	caps := CapabilitiesFromFactory(f)
	if !caps["io"] {
		t.Error("io capability should always be present")
	}
}

package builtins

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
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

func TestCapabilitiesFromOpts_IncludesBrowser(t *testing.T) {
	// Without browser client.
	caps := CapabilitiesFromOpts(BuildOpts{})
	if caps["browser"] {
		t.Error("browser should not be present without client")
	}

	// With browser client.
	c := browser.NewClient("http://localhost:1", "tok")
	caps = CapabilitiesFromOpts(BuildOpts{BrowserClient: c})
	if !caps["browser"] {
		t.Error("browser should be present when BrowserClient is set")
	}
}

// With every cloud key set and no named cloud backend, the factory's
// capabilities come from local backends only. Vision is unavailable
// when local Ollama does not answer.
func TestCapabilitiesFromFactory_CloudKeysDoNotGrantVision(t *testing.T) {
	for _, k := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"} {
		t.Setenv(k, "test-key-"+k)
	}
	var cfg config.ProvidersConfig
	cfg.Vision.Endpoint = "http://127.0.0.1:1"
	cfg.Vision.Model = config.DefaultVisionModel
	caps := CapabilitiesFromFactory(providers.NewFactory(cfg, nil))
	if caps["vision"] {
		t.Error("vision capability granted with no local backend")
	}
}

// Cloud API keys grant no capability. The host is made empty of tools (an
// empty PATH, a HOME with no ~/.local/bin) and the local Ollama is a server
// that has gone away, so the answer does not depend on the machine: the
// set with every cloud key set equals the set with none, and vision, which
// only a local Ollama with its model can grant, is false.
func TestCapabilitiesFromFactory_CloudKeysGrantNothingOnEmptyHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir())
	if !slices.Contains(providers.ExtraToolDirs(), filepath.Join(home, ".local", "bin")) {
		t.Fatalf("ExtraToolDirs %v does not follow HOME: the host is not isolated", providers.ExtraToolDirs())
	}

	gone := httptest.NewServer(http.NotFoundHandler())
	endpoint := gone.URL
	gone.Close()
	var cfg config.ProvidersConfig
	cfg.Vision.Endpoint = endpoint
	cfg.Vision.Model = config.DefaultVisionModel

	keys := []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"}
	for _, k := range keys {
		t.Setenv(k, "")
	}
	without := CapabilitiesFromFactory(providers.NewFactory(cfg, nil))
	for _, k := range keys {
		t.Setenv(k, "test-key-"+k)
	}
	with := CapabilitiesFromFactory(providers.NewFactory(cfg, nil))

	if with["vision"] {
		t.Error("vision granted by cloud keys with no local Ollama")
	}
	for name := range without {
		if with[name] != without[name] {
			t.Errorf("%s capability changed by cloud keys: %v -> %v", name, without[name], with[name])
		}
	}
	for name := range with {
		if with[name] != without[name] {
			t.Errorf("%s capability granted by cloud keys", name)
		}
	}
}

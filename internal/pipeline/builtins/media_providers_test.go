package builtins_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// toolsOnPath puts fake executables named names first on PATH and
// returns the directory.
func toolsOnPath(t *testing.T, names ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test requires Unix")
	}
	dir := t.TempDir()
	for _, n := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\nexit 0\n"), 0o755)) //nolint:gosec // fixture must be executable
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func be(name string) config.ProviderBackendConfig { return config.ProviderBackendConfig{Backend: name} }

// mediaCfg leaves ocr, video, frames and transcription on auto and pins every
// other role to the stub so no test probes a live service.
func mediaCfg(t *testing.T) config.ProvidersConfig {
	t.Helper()
	// whisper-cli needs a model file to be a transcription backend.
	model := filepath.Join(t.TempDir(), "ggml-base.bin")
	require.NoError(t, os.WriteFile(model, []byte("model"), 0o600))
	return config.ProvidersConfig{
		OCR: be("auto"), Video: be("auto"), Frames: be("auto"), Transcription: config.ProviderBackendConfig{Backend: "auto", Model: model},
		Vision: be("stub"), LLM: be("stub"), Summarization: be("stub"), Diarization: be("stub"), Document: be("auto"),
	}
}

// The video capability comes from the factory: with ffmpeg and ffprobe
// present, video.full and video.audio_only register.
func TestVideoPipelinesRegisterWhenFFmpegPresent(t *testing.T) {
	toolsOnPath(t, "ffmpeg", "ffprobe", "whisper-cli", "tesseract")
	f := providers.NewFactory(mediaCfg(t), nil)

	assert.True(t, builtins.CapabilitiesFromFactory(f)["video"], "video capability")

	reg, regErr := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: f})
	if regErr != nil {
		t.Fatal(regErr)
	}
	for _, name := range []string{"video.full", "video.audio_only"} {
		_, err := reg.Get(name)
		assert.NoError(t, err, name)
	}
}

// A stubbed video backend is not a video capability.
func TestVideoCapabilityAbsentForStub(t *testing.T) {
	cfg := mediaCfg(t)
	cfg.Video = be("stub")
	assert.False(t, builtins.CapabilitiesFromFactory(providers.NewFactory(cfg, nil))["video"])
}

// Diarization is optional enrichment: it is not a required provider, so
// a missing diarizer does not block transcription.
func TestDiarizationNotRequiredForTranscription(t *testing.T) {
	for _, name := range []string{"audio.transcribe", "video.audio_only"} {
		assert.NotContains(t, builtins.Defs()[name].Providers, "diarization", name)
	}

	toolsOnPath(t, "ffmpeg", "ffprobe", "whisper-cli", "tesseract")
	reg, regErr := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: providers.NewFactory(mediaCfg(t), nil)})
	if regErr != nil {
		t.Fatal(regErr)
	}
	for _, name := range []string{"audio.transcribe", "video.audio_only"} {
		p, err := reg.Get(name)
		require.NoError(t, err, name)
		for _, s := range p.Steps {
			assert.NotEqual(t, "speaker_diarizer", s.Name(), "%s: diarizer pruned when no diarization backend", name)
		}
	}
}

// A media pipeline never drops a provider step. Inserting a vision step
// into image.ocr with no vision backend would have pruned it silently
// and embedded the raw file; it now fails the build.
func TestMediaPipelineNeverPrunesProviderStep(t *testing.T) {
	toolsOnPath(t, "tesseract")
	cfg := mediaCfg(t)
	opts := builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}

	_, err := builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"image.ocr": {InsertSteps: []config.StepInsert{{Step: "vision_analyzer", After: "ocr_extractor"}}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image.ocr")
	assert.Contains(t, err.Error(), "vision")
}

// The stub-override probe: a per-pipeline stub OCR backend used to prune
// ocr_extractor and embed the PNG bytes. It now fails the build.
func TestStubOverrideOfRequiredRoleFailsBuild(t *testing.T) {
	toolsOnPath(t, "tesseract")
	cfg := mediaCfg(t)
	opts := builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}

	_, err := builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"image.ocr": {Providers: map[string]config.ProviderBackendConfig{"ocr": be("stub")}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image.ocr")
	assert.Contains(t, err.Error(), "ocr")
}

// Non-media pipelines keep their prune behaviour.
func TestNonMediaPipelineStillPrunes(t *testing.T) {
	cfg := mediaCfg(t)
	reg, err := builtins.ConfiguredRegistryWithPipelineOverrides(
		builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}, cfg, config.PipelinesConfig{
			Overrides: map[string]config.PipelineOverride{
				"text.short": {InsertSteps: []config.StepInsert{{Step: "graph_extractor"}}},
			},
		})
	require.NoError(t, err)
	p, err := reg.Get("text.short")
	require.NoError(t, err)
	for _, s := range p.Steps {
		assert.NotEqual(t, "graph_extractor", s.Name(), "llm step pruned without an llm backend")
	}
}

// An explicitly configured backend that is missing fails registry build.
func TestMissingExplicitBackendFailsRegistryBuild(t *testing.T) {
	cfg := mediaCfg(t)
	cfg.Transcription = be("ollama") // no ollama transcription exists
	_, err := builtins.ConfiguredRegistryWithPipelineOverrides(
		builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}, cfg, config.PipelinesConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "providers.transcription.backend")

	_, err = builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "providers.transcription.backend")
}

// A per-pipeline override naming a missing backend fails the build too.
func TestMissingExplicitBackendInOverrideFailsBuild(t *testing.T) {
	cfg := mediaCfg(t)
	opts := builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}
	_, err := builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"text.short": {Providers: map[string]config.ProviderBackendConfig{"transcription": be("ollama")}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "providers.transcription.backend")
}

// A pipeline left unregistered for a missing role says which pipeline,
// which role and how to install it.
func TestUnregisteredPipelineNamesRoleAndHint(t *testing.T) {
	cfg := mediaCfg(t)
	cfg.OCR = be("stub")
	reg, regErr := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)})
	if regErr != nil {
		t.Fatal(regErr)
	}

	_, err := reg.Get("image.ocr")
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{"image.ocr", "ocr", "install"} {
		assert.True(t, strings.Contains(strings.ToLower(msg), want), "message %q lacks %q", msg, want)
	}
}

// A nil Factory has no providers: media pipelines are unregistered, never
// built on stub steps, whatever tools are on PATH.
func TestNilFactoryRegistersNoMediaPipelines(t *testing.T) {
	toolsOnPath(t, "ffmpeg", "ffprobe", "whisper-cli", "tesseract", "pyannote")
	reg, err := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{})
	require.NoError(t, err)
	for _, name := range []string{"audio.transcribe", "image.ocr", "video.full", "video.audio_only", "image.analysis"} {
		_, err := reg.Get(name)
		assert.Error(t, err, name)
	}
	// Documents extract with the pure-Go provider, which needs no tool.
	for _, name := range []string{"doc.pdf", "doc.office", "url.generic"} {
		_, err := reg.Get(name)
		assert.NoError(t, err, name)
	}
}

// An unknown backend name is an unavailable role, not a capability.
func TestUnknownBackendIsNotACapability(t *testing.T) {
	cfg := mediaCfg(t)
	cfg.OCR = be("tesseractt")
	assert.False(t, builtins.CapabilitiesFromFactory(providers.NewFactory(cfg, nil))["ocr"])
}

// whisper-cli, the binary Homebrew installs, is probed.
func TestWhisperCLIIsTranscriptionCapability(t *testing.T) {
	toolsOnPath(t, "whisper-cli")
	cfg := mediaCfg(t)
	assert.True(t, builtins.CapabilitiesFromFactory(providers.NewFactory(cfg, nil))["transcription"])
}

// Overrides cannot inject stubs into video or document pipelines.
func TestStubOverrideOfVideoOrDocumentRoleFailsBuild(t *testing.T) {
	toolsOnPath(t, "ffmpeg", "ffprobe", "whisper-cli", "tesseract")
	cfg := mediaCfg(t)
	opts := builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}
	cases := []struct{ pipe, role string }{
		{"video.full", "ocr"},
		{"video.full", "video"},
		{"video.audio_only", "video"},
		{"doc.pdf", "document"},
		{"doc.office", "document"},
	}
	for _, tc := range cases {
		_, err := builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{
			Overrides: map[string]config.PipelineOverride{
				tc.pipe: {Providers: map[string]config.ProviderBackendConfig{tc.role: be("stub")}},
			},
		})
		require.Error(t, err, "%s %s", tc.pipe, tc.role)
		assert.Contains(t, err.Error(), tc.pipe)
		assert.Contains(t, err.Error(), tc.role)
	}
}

// Only media capabilities are never pruned: an llm step still is.
func TestLLMStepPrunedInMediaPipeline(t *testing.T) {
	toolsOnPath(t, "whisper-cli")
	cfg := mediaCfg(t)
	reg, err := builtins.ConfiguredRegistryWithPipelineOverrides(
		builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}, cfg, config.PipelinesConfig{
			Overrides: map[string]config.PipelineOverride{
				"audio.transcribe": {InsertSteps: []config.StepInsert{{Step: "graph_extractor"}}},
			},
		})
	require.NoError(t, err)
	p, err := reg.Get("audio.transcribe")
	require.NoError(t, err)
	for _, s := range p.Steps {
		assert.NotEqual(t, "graph_extractor", s.Name())
	}
}

// auto with nothing installed starts clean.
func TestAutoBackendsStartClean(t *testing.T) {
	_, err := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: providers.NewFactory(mediaCfg(t), nil)})
	require.NoError(t, err)
}

// A versioned lookup keeps the reason.
func TestVersionedLookupKeepsUnavailableReason(t *testing.T) {
	cfg := mediaCfg(t)
	cfg.OCR = be("stub")
	reg, err := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)})
	require.NoError(t, err)
	_, err = reg.Get("image.ocr@v0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs ocr")
}

// Each role is resolved once per registry build: the capability check and
// the step constructors share one provider, so a probe that answers once
// can never register a stub in the step. Vision auto probes a server, so
// the probe count is the observable.
func TestRoleResolvedOnceAcrossCapsAndSteps(t *testing.T) {
	toolsOnPath(t, "tesseract")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) > 1 {
			http.Error(w, "gone", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"vision-test:1b","model":"vision-test:1b"}]}`))
	}))
	defer srv.Close()

	cfg := mediaCfg(t)
	cfg.Vision = config.ProviderBackendConfig{Backend: "auto", Endpoint: srv.URL, Model: "vision-test:1b"}
	reg, err := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)})
	require.NoError(t, err)

	_, err = reg.Get("image.analysis")
	require.NoError(t, err, "vision answered the one probe, so image.analysis registers")
	assert.EqualValues(t, 1, hits.Load(), "vision resolved more than once per build")

	hits.Store(0)
	_, err = builtins.ConfiguredRegistryWithPipelineOverrides(builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}, cfg, config.PipelinesConfig{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, hits.Load(), "overrides builder resolved vision more than once")

	// An override that leaves vision alone re-resolves the other roles of
	// its pipeline on a derived factory; the vision answer is shared, not
	// probed again (the server answers the first probe only).
	hits.Store(0)
	_, err = builtins.ConfiguredRegistryWithPipelineOverrides(builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}, cfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"image.analysis": {Providers: map[string]config.ProviderBackendConfig{"ocr": be("tesseract")}},
			"image.ocr":      {Providers: map[string]config.ProviderBackendConfig{"ocr": be("tesseract")}},
		},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, hits.Load(), "an override of another role probed vision again")
}

// An override can enable a pipeline the base config leaves unavailable.
func TestOverrideEnablesPipelineBaseLacks(t *testing.T) {
	toolsOnPath(t, "tesseract")
	cfg := mediaCfg(t)
	cfg.OCR = be("stub")
	opts := builtins.BuildOpts{Factory: providers.NewFactory(cfg, nil)}

	reg, err := builtins.ConfiguredRegistryWithPipelineOverrides(opts, cfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"image.ocr": {Providers: map[string]config.ProviderBackendConfig{"ocr": be("tesseract")}},
		},
	})
	require.NoError(t, err)
	_, err = reg.Get("image.ocr")
	assert.NoError(t, err, "override names a working ocr backend")
}

package providers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// noTools makes every CLI tool missing: PATH is an empty directory and
// the common local directories are not searched.
func noTools(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	prev := extraToolDirs
	extraToolDirs = func() []string { return nil }
	t.Cleanup(func() { extraToolDirs = prev })
	return dir
}

// fakeTool installs an executable named name into dir.
func fakeTool(t *testing.T, dir, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test requires Unix")
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write fake %s: %v", name, err)
	}
}

// whisperModel writes a stand-in model file and returns its path.
func whisperModel(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ggml-base.bin")
	if err := os.WriteFile(path, []byte("model"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func backendModel(name, model string) config.ProviderBackendConfig {
	return config.ProviderBackendConfig{Backend: name, Model: model}
}

func backend(name string) config.ProviderBackendConfig {
	return config.ProviderBackendConfig{Backend: name}
}

// An explicitly named backend whose tool is missing is a configuration
// error naming the role, the backend and the install hint. It is never a
// stub or another backend.
func TestValidate_ExplicitMissingBackendIsConfigError(t *testing.T) {
	noTools(t)

	cases := []struct {
		name string
		cfg  config.ProvidersConfig
		role string
		tool string
	}{
		{"ocr", config.ProvidersConfig{OCR: backend(backendTesseract)}, "ocr", "tesseract"},
		{"video", config.ProvidersConfig{Video: backend(backendFFmpeg)}, "video", "ffmpeg"},
		{"document", config.ProvidersConfig{Document: backend(pdftotextTool)}, "document", "pdftotext"},
		{"transcription", config.ProvidersConfig{Transcription: backend(backendWhisper)}, "transcription", "whisper"},
		{"diarization", config.ProvidersConfig{Diarization: backend(pyannoteName)}, "diarization", "pyannote"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewFactory(tc.cfg, nil).Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want a configuration error for a missing %s backend", tc.role)
			}
			msg := err.Error()
			for _, want := range []string{"providers." + tc.role + ".backend", tc.tool, "Install"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not contain %q", msg, want)
				}
			}
			var toolErr *ToolNotInstalledError
			if !errors.As(err, &toolErr) {
				t.Errorf("error does not wrap *ToolNotInstalledError: %v", err)
			}
		})
	}
}

// Ollama has no transcription support: naming it is a configuration
// error, not a silent stub.
func TestValidate_OllamaTranscriptionIsConfigError(t *testing.T) {
	err := NewFactory(config.ProvidersConfig{Transcription: backend(backendOllama)}, nil).Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error for transcription backend ollama")
	}
	if !strings.Contains(err.Error(), "providers.transcription.backend") {
		t.Errorf("error %q does not name the config key", err)
	}
}

// Every missing backend is reported at once, not just the first.
func TestValidate_ReportsEveryMissingBackend(t *testing.T) {
	noTools(t)
	err := NewFactory(config.ProvidersConfig{
		OCR:   backend(backendTesseract),
		Video: backend(backendFFmpeg),
	}, nil).Validate()
	if err == nil {
		t.Fatal("Validate() = nil")
	}
	for _, want := range []string{"providers.ocr.backend", "providers.video.backend"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

// Auto, empty and stub name no tool, so they are never a configuration
// error.
func TestValidate_AutoEmptyAndStubPass(t *testing.T) {
	noTools(t)
	for _, name := range []string{"", "auto", backendStub} {
		cfg := config.ProvidersConfig{
			Video: backend(name), Document: backend(name), OCR: backend(name),
			Transcription: backend(name), Vision: backend(name), Diarization: backend(name),
		}
		if err := NewFactory(cfg, nil).Validate(); err != nil {
			t.Errorf("backend %q: Validate() = %v, want nil", name, err)
		}
	}
}

// A named backend whose tool is installed passes.
func TestValidate_InstalledBackendPasses(t *testing.T) {
	dir := noTools(t)
	fakeTool(t, dir, "tesseract")
	fakeTool(t, dir, "ffmpeg")
	fakeTool(t, dir, "ffprobe")
	fakeTool(t, dir, "pdftotext")
	fakeTool(t, dir, "whisper-cli")
	fakeTool(t, dir, "pyannote")
	err := NewFactory(config.ProvidersConfig{
		OCR:           backend(backendTesseract),
		Video:         backend(backendFFmpeg),
		Document:      backend(pdftotextTool),
		Transcription: backendModel(backendWhisper, whisperModel(t)),
		Diarization:   backend(pyannoteName),
	}, nil).Validate()
	if err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// A typo in a backend name is a configuration error naming the key and
// the valid values, never a silent auto.
func TestValidate_UnknownBackendNamesKeyAndValidValues(t *testing.T) {
	cases := []struct {
		role string
		cfg  config.ProvidersConfig
		want string
	}{
		{"ocr", config.ProvidersConfig{OCR: backend("tesseractt")}, "auto, tesseract, stub"},
		{"video", config.ProvidersConfig{Video: backend("ffmpg")}, "ffmpeg"},
		{"document", config.ProvidersConfig{Document: backend("pdftotxt")}, "golib"},
		{"transcription", config.ProvidersConfig{Transcription: backend("ollama")}, "whisper-cli"},
		{"diarization", config.ProvidersConfig{Diarization: backend("pyanote")}, "pyannote"},
		{"vision", config.ProvidersConfig{Vision: backend("gpt")}, "openrouter"},
		{"llm", config.ProvidersConfig{LLM: backend("gpt")}, "anthropic"},
	}
	for _, tc := range cases {
		err := NewFactory(tc.cfg, nil).Validate()
		if err == nil {
			t.Errorf("%s: Validate() = nil", tc.role)
			continue
		}
		for _, want := range []string{"providers." + tc.role + ".backend", tc.want} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q lacks %q", tc.role, err, want)
			}
		}
	}
}

// A getter never returns a stub for a backend that cannot run: it returns
// a provider whose every call fails with the cause, so a path that skips
// Validate fails per job instead of storing placeholder text.
func TestGettersReturnFailedProviderNotStub(t *testing.T) {
	noTools(t)
	ctx := context.Background()
	f := NewFactory(config.ProvidersConfig{
		OCR: backend(backendTesseract), Video: backend(backendFFmpeg), Document: backend(pdftotextTool),
		Transcription: backend(backendWhisper), Diarization: backend(pyannoteName),
	}, nil)

	var toolErr *ToolNotInstalledError
	if _, err := f.OCR().Extract(ctx, nil, ""); !errors.As(err, &toolErr) {
		t.Errorf("OCR.Extract err = %v, want *ToolNotInstalledError", err)
	}
	if _, err := f.Transcription().Transcribe(ctx, "a.wav", TranscribeOptions{}); !errors.As(err, &toolErr) {
		t.Errorf("Transcribe err = %v", err)
	}
	if _, err := f.Diarization().Diarize(ctx, "a.wav", nil); !errors.As(err, &toolErr) {
		t.Errorf("Diarize err = %v", err)
	}
	if _, err := f.Video().ProbeMetadata(ctx, "a.mp4"); !errors.As(err, &toolErr) {
		t.Errorf("ProbeMetadata err = %v", err)
	}
	if _, err := f.Document().ExtractPDF(ctx, "a.pdf"); !errors.As(err, &toolErr) {
		t.Errorf("ExtractPDF err = %v", err)
	}
	for _, p := range []any{f.OCR(), f.Transcription(), f.Diarization(), f.Video(), f.Document()} {
		if !IsFailed(p) {
			t.Errorf("%T: IsFailed = false", p)
		}
	}

	// An unknown name fails the same way.
	u := NewFactory(config.ProvidersConfig{OCR: backend("bogus")}, nil)
	if _, err := u.OCR().Extract(ctx, nil, ""); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("unknown backend Extract err = %v", err)
	}
	// Stub is not failed.
	if IsFailed(NewFactory(config.ProvidersConfig{OCR: backend(backendStub)}, nil).OCR()) {
		t.Error("stub reported as failed")
	}
}

// whisper-cli, which Homebrew installs, satisfies the whisper-cli backend
// once a model is configured.
func TestWhisperCLIBinaryIsFound(t *testing.T) {
	dir := noTools(t)
	fakeTool(t, dir, "whisper-cli")
	f := NewFactory(config.ProvidersConfig{Transcription: backendModel(backendWhisper, whisperModel(t))}, nil)
	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	if IsFailed(f.Transcription()) || f.Transcription().Name() != backendWhisper {
		t.Error("whisper-cli on PATH with a model not used")
	}
	if !strings.Contains(InstallHint("transcription"), "whisper-cli") {
		t.Errorf("hint %q does not name whisper-cli", InstallHint("transcription"))
	}
}

// whisper-cli cannot run without a model, so it is unavailable until
// providers.transcription.model names an existing file: auto leaves the
// role unavailable, a named backend is a configuration error.
func TestWhisperNeedsAnExistingModel(t *testing.T) {
	dir := noTools(t)
	fakeTool(t, dir, "whisper-cli")

	auto := NewFactory(config.ProvidersConfig{Transcription: backend("auto")}, nil)
	if _, isStub := auto.Transcription().(*StubTranscriptionProvider); !isStub {
		t.Errorf("auto without a model: got %T, want the unavailable stub", auto.Transcription())
	}

	for name, model := range map[string]string{"unset": "", "missing": filepath.Join(t.TempDir(), "nope.bin")} {
		err := NewFactory(config.ProvidersConfig{Transcription: backendModel(backendWhisper, model)}, nil).Validate()
		if err == nil || !strings.Contains(err.Error(), "providers.transcription.model") {
			t.Errorf("%s model: Validate() = %v, want an error naming providers.transcription.model", name, err)
		}
	}
}

// The openai-whisper "whisper" binary takes other flags; it is never used.
func TestOpenAIWhisperBinaryIsNotProbed(t *testing.T) {
	dir := noTools(t)
	fakeTool(t, dir, "whisper")
	f := NewFactory(config.ProvidersConfig{Transcription: backendModel("auto", whisperModel(t))}, nil)
	if _, isStub := f.Transcription().(*StubTranscriptionProvider); !isStub {
		t.Errorf("auto with only whisper on PATH: got %T, want the unavailable stub", f.Transcription())
	}
}

// InstallHint names what to install for a role, for the 422 message.
func TestInstallHint(t *testing.T) {
	for _, role := range []string{"ocr", "video", "transcription", "diarization", "vision"} {
		if InstallHint(role) == "" {
			t.Errorf("InstallHint(%q) is empty", role)
		}
	}
	if got := InstallHint("no-such-role"); got != "" {
		t.Errorf("InstallHint(unknown) = %q, want empty", got)
	}
}

package providers

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestFactoryStubBackends(t *testing.T) {
	cfg := config.ProvidersConfig{
		Video:         config.ProviderBackendConfig{Backend: "stub"},
		Document:      config.ProviderBackendConfig{Backend: "stub"},
		OCR:           config.ProviderBackendConfig{Backend: "stub"},
		Transcription: config.ProviderBackendConfig{Backend: "stub"},
		Vision:        config.ProviderBackendConfig{Backend: "stub"},
		Diarization:   config.ProviderBackendConfig{Backend: "stub"},
	}
	f := NewFactory(cfg, nil)

	if f.Video().Name() != "stub" {
		t.Errorf("Video: got %q", f.Video().Name())
	}
	if f.Document().Name() != "stub" {
		t.Errorf("Document: got %q", f.Document().Name())
	}
	if f.OCR().Name() != "stub" {
		t.Errorf("OCR: got %q", f.OCR().Name())
	}
	if f.Transcription().Name() != "stub" {
		t.Errorf("Transcription: got %q", f.Transcription().Name())
	}
	if f.Vision().Name() != "stub" {
		t.Errorf("Vision: got %q", f.Vision().Name())
	}
	if f.Diarization().Name() != "stub" {
		t.Errorf("Diarization: got %q", f.Diarization().Name())
	}
}

func TestFactoryAutoFallback(t *testing.T) {
	// "auto" should always return a non-nil provider (fallback to stub if tool missing).
	cfg := config.ProvidersConfig{
		Video:         config.ProviderBackendConfig{Backend: "auto"},
		Document:      config.ProviderBackendConfig{Backend: "auto"},
		OCR:           config.ProviderBackendConfig{Backend: "auto"},
		Transcription: config.ProviderBackendConfig{Backend: "auto"},
		Vision:        config.ProviderBackendConfig{Backend: "auto"},
		Diarization:   config.ProviderBackendConfig{Backend: "auto"},
	}
	f := NewFactory(cfg, nil)

	// These should never be nil — auto falls back to stub.
	if f.Video() == nil {
		t.Error("Video() returned nil")
	}
	if f.Document() == nil {
		t.Error("Document() returned nil")
	}
	if f.OCR() == nil {
		t.Error("OCR() returned nil")
	}
	if f.Transcription() == nil {
		t.Error("Transcription() returned nil")
	}
	if f.Vision() == nil {
		t.Error("Vision() returned nil")
	}
	if f.Diarization() == nil {
		t.Error("Diarization() returned nil")
	}
}

func TestFactoryAutoDocument(t *testing.T) {
	cfg := config.ProvidersConfig{
		Document: config.ProviderBackendConfig{Backend: "auto"},
	}
	f := NewFactory(cfg, nil)
	// auto for document should always return golib (pure Go, always available).
	if f.Document().Name() != "golib" {
		t.Errorf("auto document: got %q, want golib", f.Document().Name())
	}
}

func TestFactoryGolibDocument(t *testing.T) {
	cfg := config.ProvidersConfig{
		Document: config.ProviderBackendConfig{Backend: "golib"},
	}
	f := NewFactory(cfg, nil)
	if f.Document().Name() != "golib" {
		t.Errorf("golib document: got %q", f.Document().Name())
	}
}

func TestFactoryAutoVision(t *testing.T) {
	cfg := config.ProvidersConfig{
		Vision: config.ProviderBackendConfig{Backend: "auto"},
	}
	f := NewFactory(cfg, nil)
	// auto for vision picks the best available backend; must be non-nil.
	if f.Vision() == nil {
		t.Error("auto vision: got nil provider")
	}
}

func TestFactoryFFmpegIfAvailable(t *testing.T) {
	if _, err := LookupTool("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	if _, err := LookupTool("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	cfg := config.ProvidersConfig{
		Video: config.ProviderBackendConfig{Backend: "auto"},
	}
	f := NewFactory(cfg, nil)
	if f.Video().Name() != "ffmpeg" {
		t.Errorf("auto video with ffmpeg installed: got %q, want ffmpeg", f.Video().Name())
	}
}

func TestFactoryTesseractIfAvailable(t *testing.T) {
	if _, err := LookupTool("tesseract"); err != nil {
		t.Skip("tesseract not installed")
	}
	cfg := config.ProvidersConfig{
		OCR: config.ProviderBackendConfig{Backend: "auto"},
	}
	f := NewFactory(cfg, nil)
	if f.OCR().Name() != "tesseract" {
		t.Errorf("auto OCR with tesseract installed: got %q, want tesseract", f.OCR().Name())
	}
}

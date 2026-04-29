package providers

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
	"hop.top/kit/go/storage/secret"
	"hop.top/kit/go/storage/secret/env"
)

// Factory resolves the best available provider for each type based on configuration.
type Factory struct {
	cfg     config.ProvidersConfig
	secrets secret.Store
}

// NewFactory creates a Factory from the providers configuration.
// If store is nil, an env-backed store is used (backward-compatible default).
func NewFactory(cfg config.ProvidersConfig, store secret.Store) *Factory {
	if store == nil {
		store = env.New("")
	}
	return &Factory{cfg: cfg, secrets: store}
}

// apiKey fetches a secret by key via the store, falling back to os.Getenv.
func (f *Factory) apiKey(key string) string {
	if got, err := f.secrets.Get(context.Background(), key); err == nil {
		return string(got.Value)
	}
	return os.Getenv(key)
}

// Video returns the best available VideoProvider.
func (f *Factory) Video() VideoProvider {
	switch f.cfg.Video.Backend {
	case "ffmpeg":
		return f.mustFFmpeg()
	case "stub":
		return NewStubVideoProvider()
	default: // "auto" or empty
		if p := f.tryFFmpeg(); p != nil {
			return p
		}
		slog.Debug("providers: no video backend found, using stub")
		return NewStubVideoProvider()
	}
}

// Document returns the best available DocumentProvider.
func (f *Factory) Document() DocumentProvider {
	switch f.cfg.Document.Backend {
	case "pdftotext":
		return f.mustPdftotext()
	case "golib":
		return f.newGolib()
	case "stub":
		return NewStubDocumentProvider()
	default: // "auto"
		// Prefer golib (pure Go, always available), then pdftotext for PDF
		return f.newGolib()
	}
}

// OCR returns the best available OCRProvider.
func (f *Factory) OCR() OCRProvider {
	switch f.cfg.OCR.Backend {
	case "tesseract":
		return f.mustTesseract()
	case "stub":
		return NewStubOCRProvider()
	default: // "auto"
		if p := f.tryTesseract(); p != nil {
			return p
		}
		slog.Debug("providers: no OCR backend found, using stub")
		return NewStubOCRProvider()
	}
}

// Transcription returns the best available TranscriptionProvider.
func (f *Factory) Transcription() TranscriptionProvider {
	switch f.cfg.Transcription.Backend {
	case "whisper-cli":
		return f.mustWhisper()
	case "ollama":
		return f.newOllamaTranscription()
	case "stub":
		return NewStubTranscriptionProvider()
	default: // "auto"
		if p := f.tryWhisper(); p != nil {
			return p
		}
		slog.Debug("providers: no transcription backend found, using stub")
		return NewStubTranscriptionProvider()
	}
}

// Vision returns the best available vision.Provider.
func (f *Factory) Vision() vision.Provider {
	switch f.cfg.Vision.Backend {
	case "ollama":
		return f.newOllamaVision()
	case "openai":
		return f.newOpenAIVision()
	case "anthropic":
		return f.newAnthropicVision()
	case "gemini":
		return f.newGeminiVision()
	case "openrouter":
		return f.newOpenRouterVision()
	case "stub":
		return vision.NewStubProvider()
	default: // "auto"
		if p := f.tryOllamaVision(); p != nil {
			return p
		}
		if f.apiKey("OPENAI_API_KEY") != "" {
			return f.newOpenAIVision()
		}
		if f.apiKey("ANTHROPIC_API_KEY") != "" {
			return f.newAnthropicVision()
		}
		if f.apiKey("GEMINI_API_KEY") != "" {
			return f.newGeminiVision()
		}
		if f.apiKey("OPENROUTER_API_KEY") != "" {
			return f.newOpenRouterVision()
		}
		slog.Debug("providers: no vision backend found, using stub")
		return vision.NewStubProvider()
	}
}

// Diarization returns the best available DiarizationProvider.
func (f *Factory) Diarization() DiarizationProvider {
	switch f.cfg.Diarization.Backend {
	case "pyannote":
		return f.mustPyannote()
	case "stub":
		return NewStubDiarizationProvider()
	default: // "auto"
		if p := f.tryPyannote(); p != nil {
			return p
		}
		slog.Debug("providers: no diarization backend found, using stub")
		return NewStubDiarizationProvider()
	}
}

// LLM returns the best available LLMProvider.
func (f *Factory) LLM() LLMProvider {
	switch f.cfg.LLM.Backend {
	case "openai":
		return NewOpenAILLMProvider(f.cfg.LLM.Model)
	case "anthropic":
		return NewAnthropicLLMProvider(f.cfg.LLM.Model)
	case "ollama":
		return NewOllamaLLMProvider(f.cfg.LLM.Endpoint, f.cfg.LLM.Model)
	case "stub":
		return NewStubLLMProvider()
	default: // "auto" or empty
		// Try env vars in priority order.
		if f.apiKey("ANTHROPIC_API_KEY") != "" {
			return NewAnthropicLLMProvider(f.cfg.LLM.Model)
		}
		if f.apiKey("OPENAI_API_KEY") != "" {
			return NewOpenAILLMProvider(f.cfg.LLM.Model)
		}
		// Fall back to Ollama (local).
		endpoint := f.cfg.LLM.Endpoint
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		return NewOllamaLLMProvider(endpoint, f.cfg.LLM.Model)
	}
}

// Embedding returns the best available EmbeddingProvider.
func (f *Factory) Embedding() EmbeddingProvider {
	switch f.cfg.Embedding.Backend {
	case "ollama":
		return NewOllamaEmbeddingProvider(f.cfg.Embedding.Endpoint, f.cfg.Embedding.Model)
	case "stub":
		return NewStubEmbeddingProvider()
	default: // "auto" or empty
		return NewOllamaEmbeddingProvider(f.cfg.Embedding.Endpoint, f.cfg.Embedding.Model)
	}
}

// --- FFmpeg ---

func (f *Factory) tryFFmpeg() VideoProvider {
	if _, err := LookupTool("ffprobe"); err != nil {
		return nil
	}
	if _, err := LookupTool("ffmpeg"); err != nil {
		return nil
	}
	return NewFFmpegVideoProvider()
}

func (f *Factory) mustFFmpeg() VideoProvider {
	if p := f.tryFFmpeg(); p != nil {
		return p
	}
	slog.Debug("providers: ffmpeg requested but not found, using stub")
	return NewStubVideoProvider()
}

// --- Golib (document) ---

func (f *Factory) newGolib() DocumentProvider {
	return NewGolibDocumentProvider()
}

// --- Pdftotext ---

func (f *Factory) tryPdftotext() DocumentProvider {
	if _, err := LookupTool("pdftotext"); err != nil {
		return nil
	}
	return NewPdftotextDocumentProvider()
}

func (f *Factory) mustPdftotext() DocumentProvider {
	if p := f.tryPdftotext(); p != nil {
		return p
	}
	slog.Debug("providers: pdftotext requested but not found, using stub")
	return NewStubDocumentProvider()
}

// --- Tesseract ---

func (f *Factory) tryTesseract() OCRProvider {
	if _, err := LookupTool("tesseract"); err != nil {
		return nil
	}
	lang := f.cfg.OCR.Language
	if lang == "" {
		lang = "eng"
	}
	return NewTesseractOCRProvider(lang)
}

func (f *Factory) mustTesseract() OCRProvider {
	if p := f.tryTesseract(); p != nil {
		return p
	}
	slog.Debug("providers: tesseract requested but not found, using stub")
	return NewStubOCRProvider()
}

// --- Whisper ---

func (f *Factory) tryWhisper() TranscriptionProvider {
	// Try whisper-cpp first, then whisper
	for _, name := range []string{"whisper-cpp", "whisper"} {
		if _, err := LookupTool(name); err == nil {
			return NewWhisperTranscriptionProvider(name)
		}
	}
	return nil
}

func (f *Factory) mustWhisper() TranscriptionProvider {
	if p := f.tryWhisper(); p != nil {
		return p
	}
	slog.Debug("providers: whisper-cli requested but not found, using stub")
	return NewStubTranscriptionProvider()
}

// --- Ollama (transcription) ---

func (f *Factory) newOllamaTranscription() TranscriptionProvider {
	// Ollama doesn't natively support audio transcription yet,
	// so this falls back to stub with a warning.
	slog.Debug("providers: ollama transcription not yet supported, using stub")
	return NewStubTranscriptionProvider()
}

// --- Vision providers ---

func (f *Factory) ollamaVisionEndpoint() string {
	if f.cfg.Vision.Endpoint != "" {
		return f.cfg.Vision.Endpoint
	}
	return "http://localhost:11434"
}

// tryOllamaVision probes the Ollama endpoint. Returns nil if unreachable.
func (f *Factory) tryOllamaVision() vision.Provider {
	endpoint := f.ollamaVisionEndpoint()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(strings.TrimRight(endpoint, "/") + "/api/tags")
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	resp.Body.Close()
	model := f.cfg.Vision.Model
	if model == "" {
		model = "llava"
	}
	return vision.NewOllamaProvider(endpoint, model)
}

func (f *Factory) newOllamaVision() vision.Provider {
	model := f.cfg.Vision.Model
	if model == "" {
		model = "llava"
	}
	return vision.NewOllamaProvider(f.ollamaVisionEndpoint(), model)
}

func (f *Factory) newOpenAIVision() vision.Provider {
	return vision.NewOpenAIProvider(f.cfg.Vision.Model)
}

func (f *Factory) newAnthropicVision() vision.Provider {
	return vision.NewAnthropicProvider(f.cfg.Vision.Model)
}

func (f *Factory) newGeminiVision() vision.Provider {
	return vision.NewGeminiProvider(f.cfg.Vision.Model)
}

func (f *Factory) newOpenRouterVision() vision.Provider {
	return vision.NewOpenRouterProvider(f.cfg.Vision.Model)
}

// --- Pyannote ---

func (f *Factory) tryPyannote() DiarizationProvider {
	// Try standard binary names.
	for _, name := range []string{"pyannote-audio", "pyannote"} {
		if path, err := LookupTool(name); err == nil {
			return NewPyannoteDiarizationProvider(path)
		}
	}

	// Try python module.
	if _, err := LookupTool("python3"); err == nil {
		// Quick check if module is importable.
		res, err := RunCommand(context.Background(), "python3", "-c", "import pyannote.audio; print('ok')")
		if err == nil && strings.TrimSpace(res.Stdout) == "ok" {
			return NewPyannoteDiarizationProvider("python3", "-m", "pyannote.audio")
		}
	}

	return nil
}

func (f *Factory) mustPyannote() DiarizationProvider {
	if p := f.tryPyannote(); p != nil {
		return p
	}
	slog.Debug("providers: pyannote requested but not found, using stub")
	return NewStubDiarizationProvider()
}

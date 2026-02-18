package providers

import (
	"log"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// Factory resolves the best available provider for each type based on configuration.
type Factory struct {
	cfg config.ProvidersConfig
}

// NewFactory creates a Factory from the providers configuration.
func NewFactory(cfg config.ProvidersConfig) *Factory {
	return &Factory{cfg: cfg}
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
		log.Println("providers: no video backend found, using stub")
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
		log.Println("providers: no OCR backend found, using stub")
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
		log.Println("providers: no transcription backend found, using stub")
		return NewStubTranscriptionProvider()
	}
}

// Vision returns the best available VisionProvider.
func (f *Factory) Vision() VisionProvider {
	switch f.cfg.Vision.Backend {
	case "ollama":
		return f.newOllamaVision()
	case "stub":
		return NewStubVisionProvider()
	default: // "auto"
		return f.newOllamaVision()
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
		log.Println("providers: no diarization backend found, using stub")
		return NewStubDiarizationProvider()
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
	log.Println("providers: ffmpeg requested but not found, using stub")
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
	log.Println("providers: pdftotext requested but not found, using stub")
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
	log.Println("providers: tesseract requested but not found, using stub")
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
	log.Println("providers: whisper-cli requested but not found, using stub")
	return NewStubTranscriptionProvider()
}

// --- Ollama (transcription) ---

func (f *Factory) newOllamaTranscription() TranscriptionProvider {
	// Ollama doesn't natively support audio transcription yet,
	// so this falls back to stub with a warning.
	log.Println("providers: ollama transcription not yet supported, using stub")
	return NewStubTranscriptionProvider()
}

// --- Ollama (vision) ---

func (f *Factory) newOllamaVision() VisionProvider {
	endpoint := f.cfg.Vision.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	model := f.cfg.Vision.Model
	if model == "" {
		model = "llava"
	}
	return NewOllamaVisionProvider(endpoint, model)
}

// --- Pyannote ---

func (f *Factory) tryPyannote() DiarizationProvider {
	if _, err := LookupTool("pyannote"); err != nil {
		return nil
	}
	return NewPyannoteDiarizationProvider()
}

func (f *Factory) mustPyannote() DiarizationProvider {
	if p := f.tryPyannote(); p != nil {
		return p
	}
	log.Println("providers: pyannote requested but not found, using stub")
	return NewStubDiarizationProvider()
}

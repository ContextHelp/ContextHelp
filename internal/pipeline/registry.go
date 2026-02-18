package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

type registry struct {
	pipelines map[string]*Pipeline
}

// NewRegistry creates an empty pipeline registry.
func NewRegistry() Registry {
	return &registry{pipelines: make(map[string]*Pipeline)}
}

func (r *registry) Register(name string, p *Pipeline) error {
	if _, exists := r.pipelines[name]; exists {
		return fmt.Errorf("pipeline %q already registered", name)
	}
	r.pipelines[name] = p
	return nil
}

func (r *registry) Get(name string) (*Pipeline, error) {
	p, ok := r.pipelines[name]
	if !ok {
		return nil, fmt.Errorf("pipeline %q not found", name)
	}
	return p, nil
}

func (r *registry) List() []string {
	names := make([]string, 0, len(r.pipelines))
	for k := range r.pipelines {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func (r *registry) SelectPipeline(content string) string {
	lower := strings.ToLower(content)

	// Image formats
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".tiff", ".tif", ".bmp", ".gif"} {
		if strings.HasSuffix(lower, ext) {
			return "image.ocr"
		}
	}

	// Audio formats
	for _, ext := range []string{".mp3", ".wav", ".ogg", ".flac", ".m4a"} {
		if strings.HasSuffix(lower, ext) {
			return "audio.transcribe"
		}
	}

	// Video formats
	for _, ext := range []string{".mp4", ".mov", ".avi", ".mkv", ".webm"} {
		if strings.HasSuffix(lower, ext) {
			return "video.full"
		}
	}

	// Document formats
	if strings.HasSuffix(lower, ".pdf") {
		return "doc.pdf"
	}
	for _, ext := range []string{".md", ".markdown"} {
		if strings.HasSuffix(lower, ext) {
			return "doc.markdown"
		}
	}
	for _, ext := range []string{".go", ".py", ".js", ".ts", ".rs", ".java", ".rb", ".cpp", ".c", ".cs"} {
		if strings.HasSuffix(lower, ext) {
			return "doc.code"
		}
	}
	for _, ext := range []string{".docx", ".doc", ".odt", ".rtf", ".epub"} {
		if strings.HasSuffix(lower, ext) {
			return "doc.office"
		}
	}

	// Default: text pipelines by length
	if len(content) < 500 {
		return "text.short"
	}
	return "text.long"
}

// DefaultRegistry returns a registry pre-loaded with built-in pipelines.
func DefaultRegistry() Registry {
	r := NewRegistry()

	r.Register("text.short", &Pipeline{
		PipelineName: "text.short",
		Description:  "Short text pipeline (< 500 chars)",
		Steps: []PipelineStep{
			steps.NewTypeDetector(),
			steps.NewTagger(),
		},
	})

	r.Register("text.long", &Pipeline{
		PipelineName: "text.long",
		Description:  "Long text pipeline (>= 500 chars)",
		Steps: []PipelineStep{
			steps.NewTypeDetector(),
			steps.NewSectioner(),
			steps.NewTagger(),
		},
	})

	// Image pipelines
	r.Register("image.ocr", &Pipeline{
		PipelineName: "image.ocr",
		Description:  "Image OCR extraction pipeline",
		Steps: []PipelineStep{
			steps.NewFileReader(),
			steps.NewFormatDetector(),
			steps.NewOCRExtractor(),
			steps.NewTextCleaner(),
			steps.NewTagger(),
			steps.NewEmbeddingGenerator(),
		},
	})

	r.Register("image.analysis", &Pipeline{
		PipelineName: "image.analysis",
		Description:  "Image vision analysis pipeline",
		Steps: []PipelineStep{
			steps.NewFileReader(),
			steps.NewFormatDetector(),
			steps.NewOCRExtractor(),
			steps.NewVisionAnalyzer(),
			steps.NewTextCleaner(),
			steps.NewTagger(),
			steps.NewEmbeddingGenerator(),
		},
	})

	// Audio pipeline
	r.Register("audio.transcribe", &Pipeline{
		PipelineName: "audio.transcribe",
		Description:  "Audio transcription pipeline",
		Steps: []PipelineStep{
			steps.NewFileReader(),
			steps.NewFormatDetector(),
			steps.NewAudioTranscriber(),
			steps.NewSpeakerDiarizer(false),
			steps.NewTimestampAligner(),
			steps.NewSectioner(),
			steps.NewTagger(),
			steps.NewEmbeddingGenerator(),
		},
	})

	return r
}

// ConfiguredRegistry returns a registry using real providers resolved by the factory.
func ConfiguredRegistry(f *providers.Factory) Registry {
	r := NewRegistry()

	// Text pipelines don't need providers.
	r.Register("text.short", &Pipeline{
		PipelineName: "text.short",
		Description:  "Short text pipeline (< 500 chars)",
		Steps: []PipelineStep{
			steps.NewTypeDetector(),
			steps.NewTagger(),
		},
	})

	r.Register("text.long", &Pipeline{
		PipelineName: "text.long",
		Description:  "Long text pipeline (>= 500 chars)",
		Steps: []PipelineStep{
			steps.NewTypeDetector(),
			steps.NewSectioner(),
			steps.NewTagger(),
		},
	})

	// Image pipelines — inject OCR and Vision providers.
	r.Register("image.ocr", &Pipeline{
		PipelineName: "image.ocr",
		Description:  "Image OCR extraction pipeline",
		Steps: []PipelineStep{
			steps.NewFileReader(),
			steps.NewFormatDetector(),
			steps.NewOCRExtractor(steps.WithOCRProvider(f.OCR())),
			steps.NewTextCleaner(),
			steps.NewTagger(),
			steps.NewEmbeddingGenerator(),
		},
	})

	r.Register("image.analysis", &Pipeline{
		PipelineName: "image.analysis",
		Description:  "Image vision analysis pipeline",
		Steps: []PipelineStep{
			steps.NewFileReader(),
			steps.NewFormatDetector(),
			steps.NewOCRExtractor(steps.WithOCRProvider(f.OCR())),
			steps.NewVisionAnalyzer(steps.WithVisionProvider(f.Vision())),
			steps.NewTextCleaner(),
			steps.NewTagger(),
			steps.NewEmbeddingGenerator(),
		},
	})

	// Audio pipeline — inject Transcription and Diarization providers.
	r.Register("audio.transcribe", &Pipeline{
		PipelineName: "audio.transcribe",
		Description:  "Audio transcription pipeline",
		Steps: []PipelineStep{
			steps.NewFileReader(),
			steps.NewFormatDetector(),
			steps.NewAudioTranscriber(steps.WithTranscriptionProvider(f.Transcription())),
			steps.NewSpeakerDiarizer(false, steps.WithDiarizationProvider(f.Diarization())),
			steps.NewTimestampAligner(),
			steps.NewSectioner(),
			steps.NewTagger(),
			steps.NewEmbeddingGenerator(),
		},
	})

	return r
}

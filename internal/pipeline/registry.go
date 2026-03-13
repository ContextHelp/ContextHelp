package pipeline

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

type registry struct {
	pipelines map[string]*Pipeline
	selector  SelectorFunc
	detectors []Detector
}

// NewRegistry creates an empty pipeline registry.
func NewRegistry() Registry {
	return &registry{
		pipelines: make(map[string]*Pipeline),
		selector:  defaultSelector,
	}
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
	return r.Detect(DetectInput{Source: content, Sniff: content})
}

// SetSelectors replaces the pipeline selection function.
func (r *registry) SetSelectors(fn SelectorFunc) {
	r.selector = fn
}

// RegisterDetector appends a detector to the selection chain.
func (r *registry) RegisterDetector(d Detector) {
	r.detectors = append(r.detectors, d)
}

// Detect runs registered detectors in order, falling back to the selector.
func (r *registry) Detect(in DetectInput) string {
	for _, d := range r.detectors {
		name, err := d.Detect(in)
		if err == nil {
			return name
		}
		if err != ErrDelegate {
			log.Printf("pipeline: detector error (delegating): %v", err)
		}
	}
	return r.selector(in.Source)
}

// Detectors returns a copy of the registered detectors slice.
func (r *registry) Detectors() []Detector {
	out := make([]Detector, len(r.detectors))
	copy(out, r.detectors)
	return out
}

// defaultSelector is the fallback when no selectors have been configured.
// It uses a simple text-length heuristic.
func defaultSelector(content string) string {
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

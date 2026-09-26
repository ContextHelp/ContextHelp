package pipeline

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
)

type registry struct {
	pipelines map[string]*Pipeline
	selectors Selectors
	detectors []Detector
}

// NewRegistry creates an empty pipeline registry.
func NewRegistry() Registry {
	return &registry{
		pipelines: make(map[string]*Pipeline),
		selectors: Selectors{Source: defaultSourceSelector, Content: defaultContentSelector},
	}
}

// DefaultRegistry is an alias for NewRegistry for use in tests and service wiring.
func DefaultRegistry() Registry {
	return NewRegistry()
}

func (r *registry) Register(name string, p *Pipeline) error {
	if _, exists := r.pipelines[name]; exists {
		return fmt.Errorf("pipeline %q already registered", name)
	}
	r.pipelines[name] = p
	return nil
}

func (r *registry) Upsert(name string, p *Pipeline) {
	r.pipelines[name] = p
}

func (r *registry) Get(name string) (*Pipeline, error) {
	// Direct hit on the exact registered key.
	if p, ok := r.pipelines[name]; ok {
		return p, nil
	}
	// ADR-070 §2: pipeline values use "<name>@vN" convention. Existing
	// rows / callers may pass either bare names or versioned names; the
	// registry must resolve both consistently:
	//   - Bare "text.short" → resolved as "@v0".
	//   - "text.short@v0" → resolved as bare "text.short" if no "@v0" entry
	//     was explicitly registered (legacy registrations are implicitly v0).
	parsed, version, ok := ParseVersionedName(name)
	if !ok {
		return nil, fmt.Errorf("pipeline %q not found", name)
	}
	if !strings.Contains(name, "@") {
		// Bare name → try the @v0 alias.
		if p, ok2 := r.pipelines[FormatVersionedName(parsed, 0)]; ok2 {
			return p, nil
		}
	} else if version == 0 {
		// "<name>@v0" → fall back to the bare registration.
		if p, ok2 := r.pipelines[parsed]; ok2 {
			return p, nil
		}
	}
	return nil, fmt.Errorf("pipeline %q not found", name)
}

func (r *registry) List() []string {
	names := make([]string, 0, len(r.pipelines))
	for k := range r.pipelines {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// SelectPipeline picks the pipeline for content captured from source: the
// source's URL or extension when it names one, the content otherwise.
func (r *registry) SelectPipeline(source, content string) string {
	return r.Detect(DetectInput{Source: source, Content: content})
}

// SetSelectors replaces the fallback source and content rules.
func (r *registry) SetSelectors(sel Selectors) {
	r.selectors = sel
}

// RegisterDetector appends a detector to the selection chain.
func (r *registry) RegisterDetector(d Detector) {
	r.detectors = append(r.detectors, d)
}

// Detect runs registered detectors in order, then the source rules against
// in.Source, then the content rules against in.Content.
func (r *registry) Detect(in DetectInput) string {
	for _, d := range r.detectors {
		name, err := d.Detect(in)
		if err == nil {
			return name
		}
		if !errors.Is(err, ErrDelegate) {
			log.Printf("pipeline: detector error (delegating): %v", err)
		}
	}
	if name, ok := r.selectors.Source(in.Source); ok {
		return name
	}
	return r.selectors.Content(in.Content)
}

// Detectors returns a copy of the registered detectors slice.
func (r *registry) Detectors() []Detector {
	out := make([]Detector, len(r.detectors))
	copy(out, r.detectors)
	return out
}

// defaultSourceSelector is the source fallback when no selectors have been
// configured: a file extension names the pipeline.
func defaultSourceSelector(source string) (string, bool) {
	name, ok := defaultExtensions[strings.ToLower(filepath.Ext(source))]
	return name, ok
}

var defaultExtensions = map[string]string{
	".png": "image.ocr", ".jpg": "image.ocr", ".jpeg": "image.ocr", ".webp": "image.ocr",
	".tiff": "image.ocr", ".tif": "image.ocr", ".bmp": "image.ocr", ".gif": "image.ocr",
	".mp3": "audio.transcribe", ".wav": "audio.transcribe", ".ogg": "audio.transcribe",
	".flac": "audio.transcribe", ".m4a": "audio.transcribe",
	".mp4": "video.full", ".mov": "video.full", ".avi": "video.full", ".mkv": "video.full", ".webm": "video.full",
	".pdf": "doc.pdf",
	".md":  "doc.markdown", ".markdown": "doc.markdown",
	".go": "doc.code", ".py": "doc.code", ".js": "doc.code", ".ts": "doc.code", ".rs": "doc.code",
	".java": "doc.code", ".rb": "doc.code", ".cpp": "doc.code", ".c": "doc.code", ".cs": "doc.code",
	".docx": "doc.office",
}

// defaultContentSelector is the content fallback when no selectors have been
// configured: text pipelines by length.
func defaultContentSelector(content string) string {
	if len(content) < 500 {
		return "text.short"
	}
	return "text.long"
}

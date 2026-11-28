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
	// unavailable maps a built-in pipeline that was left unregistered to
	// why (the missing roles and how to install them).
	unavailable map[string]string
	// baselines maps every pipeline definition, registered or left out, to
	// its baseline signature.
	baselines map[string]string
	// baselineEntries are the entries of each baseline, for a pipeline
	// left out for a missing provider (a registered one carries its own).
	baselineEntries map[string][]string
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
	r.noteBaseline(name, p)
	return nil
}

// SetUnavailable records why a built-in pipeline is not registered, so a
// lookup of it can say what is missing instead of just "not found".
func (r *registry) SetUnavailable(name, reason string) {
	if r.unavailable == nil {
		r.unavailable = make(map[string]string)
	}
	r.unavailable[name] = reason
}

// Unavailable reports why name was left unregistered, if it was.
func (r *registry) Unavailable(name string) (string, bool) {
	reason, ok := r.unavailable[name]
	return reason, ok
}

// MarkUnavailable records on r why the pipeline name is not registered.
// It is a no-op for registries that do not track this.
func MarkUnavailable(r Registry, name, reason string) {
	if m, ok := r.(interface{ SetUnavailable(name, reason string) }); ok {
		m.SetUnavailable(name, reason)
	}
}

// UnavailableReason reports why the pipeline name is not registered when
// it is a built-in pipeline left out for a missing provider.
func UnavailableReason(r Registry, name string) (string, bool) {
	if u, ok := r.(interface {
		Unavailable(name string) (string, bool)
	}); ok {
		return u.Unavailable(name)
	}
	return "", false
}

func (r *registry) Upsert(name string, p *Pipeline) {
	r.pipelines[name] = p
	r.noteBaseline(name, p)
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
		return nil, r.notFound(name)
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
	return nil, r.notFound(name)
}

// notFound is the Get error for name, with the reason a built-in
// pipeline is unavailable when there is one.
func (r *registry) notFound(name string) error {
	// Versioned lookups (image.ocr@v0) carry the reason of the bare name.
	bare := name
	if parsed, _, ok := ParseVersionedName(name); ok {
		bare = parsed
	}
	if reason, ok := r.unavailable[bare]; ok {
		return fmt.Errorf("pipeline %q not found: %s", name, reason)
	}
	return fmt.Errorf("pipeline %q not found", name)
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

// The built-in text pipelines, which defaultContentSelector picks between.
const (
	TextShort = "text.short"
	TextLong  = "text.long"
)

// defaultContentSelector is the content fallback when no selectors have been
// configured: text pipelines by length.
func defaultContentSelector(content string) string {
	if len(content) < 500 {
		return TextShort
	}
	return TextLong
}

func (r *registry) noteBaseline(name string, p *Pipeline) {
	if p != nil && p.BaselineSignature != "" {
		r.SetBaseline(name, p.BaselineSignature, p.BaselineEntries)
	}
}

// SetBaseline records the baseline signature of pipeline name and the
// entries it hashes.
func (r *registry) SetBaseline(name, sig string, entries []string) {
	if r.baselines == nil {
		r.baselines = make(map[string]string)
		r.baselineEntries = make(map[string][]string)
	}
	r.baselines[name] = sig
	r.baselineEntries[name] = entries
}

// BaselineEntries returns the entries of every recorded baseline.
func (r *registry) BaselineEntries() map[string][]string {
	out := make(map[string][]string, len(r.baselineEntries))
	for k, v := range r.baselineEntries {
		out[k] = v
	}
	return out
}

// Baselines returns the baseline signature of every pipeline definition.
func (r *registry) Baselines() map[string]string {
	out := make(map[string]string, len(r.baselines))
	for k, v := range r.baselines {
		out[k] = v
	}
	return out
}

// MarkBaseline records on r the baseline signature of a pipeline that is
// not registered (see MarkUnavailable). It is a no-op for registries that
// do not track this.
func MarkBaseline(r Registry, name, sig string, entries []string) {
	if m, ok := r.(interface {
		SetBaseline(name, sig string, entries []string)
	}); ok {
		m.SetBaseline(name, sig, entries)
	}
}

// Baselines returns the baseline signature of every pipeline r knows,
// registered or left out for a missing provider; for a registry that does
// not track unavailable ones, the registered pipelines' own.
func Baselines(r Registry) map[string]string {
	if b, ok := r.(interface{ Baselines() map[string]string }); ok {
		return b.Baselines()
	}
	out := map[string]string{}
	for _, name := range r.List() {
		if p, err := r.Get(name); err == nil && p.BaselineSignature != "" {
			out[name] = p.BaselineSignature
		}
	}
	return out
}

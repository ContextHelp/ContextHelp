package builtins

import (
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// Def is a declarative pipeline definition.
type Def struct {
	Description string
	Extensions  []string              // file extensions this pipeline handles
	ContentTest func(string) bool     // optional content-based selector
	Steps       []string              // step names, resolved to constructors at build time
	Providers   []string              // provider types needed: "ocr", "vision", "transcription", "diarization"
}

// Package-level registry populated by init() calls.
var defs = map[string]Def{}

// MustRegister adds a pipeline definition. Panics on duplicate name.
func MustRegister(name string, d Def) {
	if _, exists := defs[name]; exists {
		panic(fmt.Sprintf("builtins: duplicate pipeline %q", name))
	}
	defs[name] = d
}

// stepConstructors maps step names to zero-arg constructors.
var stepConstructors = map[string]func() pipeline.PipelineStep{
	"typedetector":      func() pipeline.PipelineStep { return steps.NewTypeDetector() },
	"sectioner":         func() pipeline.PipelineStep { return steps.NewSectioner() },
	"tagger":            func() pipeline.PipelineStep { return steps.NewTagger() },
	"filereader":        func() pipeline.PipelineStep { return steps.NewFileReader() },
	"formatdetector":    func() pipeline.PipelineStep { return steps.NewFormatDetector() },
	"textcleaner":       func() pipeline.PipelineStep { return steps.NewTextCleaner() },
	"embedding":         func() pipeline.PipelineStep { return steps.NewEmbeddingGenerator() },
	"timestamp_aligner": func() pipeline.PipelineStep { return steps.NewTimestampAligner() },
	"noop":              func() pipeline.PipelineStep { return steps.NewNoop() },
}

// providerStepConstructors maps step names to provider-aware constructors.
var providerStepConstructors = map[string]func(*providers.Factory) pipeline.PipelineStep{
	"ocr_extractor": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewOCRExtractor(steps.WithOCRProvider(f.OCR()))
	},
	"vision_analyzer": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewVisionAnalyzer(steps.WithVisionProvider(f.Vision()))
	},
	"audio_transcriber": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewAudioTranscriber(steps.WithTranscriptionProvider(f.Transcription()))
	},
	"speaker_diarizer": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewSpeakerDiarizer(false, steps.WithDiarizationProvider(f.Diarization()))
	},
}

// resolveStep builds a PipelineStep from a step name, optionally using a Factory for provider-aware steps.
func resolveStep(name string, f *providers.Factory) (pipeline.PipelineStep, error) {
	if f != nil {
		if ctor, ok := providerStepConstructors[name]; ok {
			return ctor(f), nil
		}
	}
	if ctor, ok := stepConstructors[name]; ok {
		return ctor(), nil
	}
	// Provider step without factory: use zero-arg fallback (stub providers).
	switch name {
	case "ocr_extractor":
		return steps.NewOCRExtractor(), nil
	case "vision_analyzer":
		return steps.NewVisionAnalyzer(), nil
	case "audio_transcriber":
		return steps.NewAudioTranscriber(), nil
	case "speaker_diarizer":
		return steps.NewSpeakerDiarizer(false), nil
	}
	return nil, fmt.Errorf("builtins: unknown step %q", name)
}

// buildPipeline constructs a Pipeline from a Def, optionally injecting providers.
func buildPipeline(name string, d Def, f *providers.Factory) (*pipeline.Pipeline, error) {
	pipelineSteps := make([]pipeline.PipelineStep, 0, len(d.Steps))
	for _, stepName := range d.Steps {
		s, err := resolveStep(stepName, f)
		if err != nil {
			return nil, fmt.Errorf("pipeline %q: %w", name, err)
		}
		pipelineSteps = append(pipelineSteps, s)
	}
	return &pipeline.Pipeline{
		PipelineName: name,
		Description:  d.Description,
		Steps:        pipelineSteps,
	}, nil
}

// selector pairs a pipeline name with its extension and content-test criteria.
type selector struct {
	PipelineName string
	Extensions   []string
	ContentTest  func(string) bool
}

// buildSelectors returns a list of selectors from the registered defs.
func buildSelectors() []selector {
	sels := make([]selector, 0, len(defs))
	for name, d := range defs {
		if len(d.Extensions) > 0 || d.ContentTest != nil {
			sels = append(sels, selector{
				PipelineName: name,
				Extensions:   d.Extensions,
				ContentTest:  d.ContentTest,
			})
		}
	}
	return sels
}

// selectPipeline picks the best pipeline for the given content string using
// extension matching first, then content tests, then the default fallback.
func selectPipeline(selectors []selector, content string) string {
	lower := strings.ToLower(content)

	// Extension-based matching.
	for _, sel := range selectors {
		for _, ext := range sel.Extensions {
			if strings.HasSuffix(lower, ext) {
				return sel.PipelineName
			}
		}
	}

	// Content-based fallback.
	for _, sel := range selectors {
		if sel.ContentTest != nil && sel.ContentTest(content) {
			return sel.PipelineName
		}
	}

	return "text.short"
}

// Registry builds a pipeline.Registry from all registered defs (no providers).
func Registry() pipeline.Registry {
	return buildRegistry(nil)
}

// ConfiguredRegistry builds a pipeline.Registry with real providers injected.
func ConfiguredRegistry(f *providers.Factory) pipeline.Registry {
	return buildRegistry(f)
}

func buildRegistry(f *providers.Factory) pipeline.Registry {
	r := pipeline.NewRegistry()
	selectors := buildSelectors()

	for name, d := range defs {
		p, err := buildPipeline(name, d, f)
		if err != nil {
			panic(fmt.Sprintf("builtins: %v", err))
		}
		if err := r.Register(name, p); err != nil {
			panic(fmt.Sprintf("builtins: %v", err))
		}
	}

	r.SetSelectors(pipeline.SelectorFunc(func(content string) string {
		return selectPipeline(selectors, content)
	}))

	return r
}

// Defs returns a copy of the registered pipeline definitions (for testing/inspection).
func Defs() map[string]Def {
	cp := make(map[string]Def, len(defs))
	for k, v := range defs {
		cp[k] = v
	}
	return cp
}

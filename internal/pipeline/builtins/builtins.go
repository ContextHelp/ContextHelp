package builtins

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Def is a declarative pipeline definition.
type Def struct {
	Description string
	Extensions  []string                         // file extensions this pipeline handles
	URLPattern  *regexp.Regexp                   // optional URL pattern; matched before url.generic fallback
	ContentTest func(string) bool                // optional content-based selector
	Priority    int                              // ContentTest ordering: lower = higher priority (default 0)
	Steps       []string                         // step names, resolved to constructors at build time
	Providers   []string                         // provider types needed: "ocr", "vision", "transcription", "diarization"
	Overrides   map[string]pipeline.StepOverride // per-step contract overrides, keyed by step name
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
	"typedetector":        func() pipeline.PipelineStep { return steps.NewTypeDetector() },
	"sectioner":           func() pipeline.PipelineStep { return steps.NewSectioner() },
	"tagger":              func() pipeline.PipelineStep { return steps.NewTagger() },
	"filereader":          func() pipeline.PipelineStep { return steps.NewFileReader() },
	"formatdetector":      func() pipeline.PipelineStep { return steps.NewFormatDetector() },
	"textcleaner":         func() pipeline.PipelineStep { return steps.NewTextCleaner() },
	"html_cleaner":        func() pipeline.PipelineStep { return steps.NewHTMLCleaner() },
	"entity_extractor":    func() pipeline.PipelineStep { return steps.NewEntityExtractor() },
	"entity_resolver":     func() pipeline.PipelineStep { return steps.NewEntityResolver() },
	"timestamp_aligner":   func() pipeline.PipelineStep { return steps.NewTimestampAligner() },
	"noop":                func() pipeline.PipelineStep { return steps.NewNoop() },
	"url_fetcher":         func() pipeline.PipelineStep { return steps.NewURLFetcher() },
	"content_type_router": func() pipeline.PipelineStep { return steps.NewContentTypeRouter() },
	// Dropbox pipeline steps.
	"dropbox_fetcher":  func() pipeline.PipelineStep { return steps.NewDropboxFetcher() },
	"dropbox_enqueuer": func() pipeline.PipelineStep { return steps.NewDropboxEnqueuer() },
	// Feed pipeline steps.
	"feed_fetcher":      func() pipeline.PipelineStep { return steps.NewFeedFetcher() },
	"feed_parser":       func() pipeline.PipelineStep { return steps.NewFeedParser() },
	"item_deduplicator": func() pipeline.PipelineStep { return steps.NewItemDeduplicator(nil) },
	"item_enqueuer":     func() pipeline.PipelineStep { return steps.NewItemEnqueuer() },
	// Batch pipeline steps.
	"jsonl_parser":     func() pipeline.PipelineStep { return steps.NewJSONLParser() },
	"csv_parser":       func() pipeline.PipelineStep { return steps.NewCSVParser() },
	"record_validator": func() pipeline.PipelineStep { return steps.NewRecordValidator() },
	"batch_enqueuer":   func() pipeline.PipelineStep { return steps.NewBatchEnqueuer() },
	// Video pipeline steps.
	"scene_detector":     func() pipeline.PipelineStep { return steps.NewSceneDetector() },
	"timeline_assembler": func() pipeline.PipelineStep { return steps.NewTimelineAssembler() },
	// Slack import pipeline steps.
	"slack_parser": func() pipeline.PipelineStep { return steps.NewSlackParser() },
	// Discord import pipeline steps.
	"discord_parser": func() pipeline.PipelineStep { return steps.NewDiscordParser() },
	// Document pipeline steps.
	"markdown_parser":      func() pipeline.PipelineStep { return steps.NewMarkdownParser() },
	"heading_splitter":     func() pipeline.PipelineStep { return steps.NewHeadingSplitter() },
	"code_block_extractor": func() pipeline.PipelineStep { return steps.NewCodeBlockExtractor() },
	"language_detector":    func() pipeline.PipelineStep { return steps.NewLanguageDetector() },
	"function_extractor":   func() pipeline.PipelineStep { return steps.NewFunctionExtractor() },
	"comment_extractor":    func() pipeline.PipelineStep { return steps.NewCommentExtractor() },
	"table_extractor":      func() pipeline.PipelineStep { return steps.NewTableExtractor() },
	// Social media archive steps.
	"twitter_archive_parser":   func() pipeline.PipelineStep { return steps.NewTwitterArchiveParser() },
	"linkedin_posts_parser":    func() pipeline.PipelineStep { return steps.NewLinkedInPostsParser() },
	"linkedin_articles_parser": func() pipeline.PipelineStep { return steps.NewLinkedInArticlesParser() },
	// Email pipeline steps.
	"email_parser":   func() pipeline.PipelineStep { return steps.NewEmailParser() },
	"email_filter":   func() pipeline.PipelineStep { return newDefaultEmailFilter() },
	"email_enqueuer": func() pipeline.PipelineStep { return steps.NewEmailEnqueuer() },
	// Graph enrichment: detects "alternative" relationships and creates proximity edges.
	// Registered with nil stores (passthrough mode); stores are injected at runtime.
	"alternative_detector": func() pipeline.PipelineStep { return steps.NewAlternativeDetector() },
	// Dependency enrichment step.
	"dependency_enricher": func() pipeline.PipelineStep { return steps.NewDependencyEnricher() },
	// Graph extractor: no-op without LLM; provider-aware constructor below.
	"graph_extractor": func() pipeline.PipelineStep { return steps.NewGraphExtractor() },
	// Structured metadata: no-op without LLM; provider-aware constructor below.
	"structured_metadata": func() pipeline.PipelineStep { return steps.NewStructuredMetadataExtractor() },
	// Content classification via hop.top/c12n; graceful no-op without cgo.
	"c12n_classify": func() pipeline.PipelineStep { return steps.NewC12nClassifier() },
	// Browser-based fetcher: nil client → returns error at run time.
	"ibr_fetcher": func() pipeline.PipelineStep { return steps.NewIBRFetcher(nil) },
}

// fetchStepConstructors maps the steps that fetch captured URLs to
// constructors taking the fetch guard (BuildOpts.FetchGuard), so the
// fetch.* config governs which addresses they may reach.
var fetchStepConstructors = map[string]func(*safefetch.Guard) pipeline.PipelineStep{
	"url_fetcher": func(g *safefetch.Guard) pipeline.PipelineStep {
		return steps.NewURLFetcher(steps.WithURLFetchGuard(g))
	},
	"feed_fetcher": func(g *safefetch.Guard) pipeline.PipelineStep {
		return steps.NewFeedFetcher(steps.WithFeedFetchGuard(g))
	},
}

// browserStepConstructors maps step names to browser-client-aware constructors.
var browserStepConstructors = map[string]func(*browser.Client, *safefetch.Guard) pipeline.PipelineStep{
	"ibr_fetcher": func(c *browser.Client, g *safefetch.Guard) pipeline.PipelineStep {
		return steps.NewIBRFetcher(c, steps.WithIBRFetchGuard(g))
	},
}

// blobStepConstructors maps step names to blob-store-aware constructors.
var blobStepConstructors = map[string]func(storage.BlobStore, int64) pipeline.PipelineStep{
	"externalize_content": func(bs storage.BlobStore, threshold int64) pipeline.PipelineStep {
		return steps.NewExternalizer(bs, threshold)
	},
}

// BuildOpts carries optional dependencies for registry construction.
type BuildOpts struct {
	Factory       *providers.Factory
	BlobStore     storage.BlobStore
	BlobThreshold int64
	BrowserClient *browser.Client
	// FetchGuard is what url_fetcher, feed_fetcher and ibr_fetcher fetch
	// captured URLs through (config.FetchGuard). Nil uses the default
	// guard: every private-network address refused.
	FetchGuard *safefetch.Guard

	// Models is the registry the embedding step reads its populate set
	// from, and dedup its default model.
	Models embeddings.ModelSource
	// Resolver builds each registered model's embedding provider.
	Resolver embeddings.ProviderResolver
	// Embeddings is the per-model vector index dedup searches.
	Embeddings storage.EmbeddingStore
	// Audit records each dedup decision.
	Audit storage.AuditStore
	// Duplicates configures near-duplicate detection. With CheckSimilar
	// set, every pipeline that embeds runs dedup right after embedding.
	Duplicates config.DuplicatesConfig

	// External resolves the installed steps pipelines.overrides inserts
	// (`dpkms pipeline step install`). Nil: no step is installed.
	External ExternalSteps
}

// ExternalSteps resolves installed (out-of-process, ADR-058) steps by name.
type ExternalSteps interface {
	// Has reports whether a step of that name is installed.
	Has(name string) bool
	// Step builds the installed step ins names for pipelineName, with
	// the run options ins sets.
	Step(pipelineName string, ins config.StepInsert) (pipeline.PipelineStep, error)
}

// embeddingStepConstructors maps step names to constructors that take the
// embedding write path's dependencies. Missing dependencies make the steps
// no-ops, so these always resolve.
var embeddingStepConstructors = map[string]func(BuildOpts) pipeline.PipelineStep{
	"embedding": func(o BuildOpts) pipeline.PipelineStep {
		return steps.NewEmbeddingGenerator(o.Models, o.Resolver)
	},
	"dedup": func(o BuildOpts) pipeline.PipelineStep {
		return steps.NewDedupStep(o.Models, o.Embeddings, o.Audit, o.Duplicates)
	},
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
	"tagger": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewTaggerWithLLM(f.LLM())
	},
	"sectioner": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewSectionerWithLLM(f.LLM())
	},
	"entity_extractor": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewEntityExtractorWithLLM(f.LLM())
	},
	"graph_extractor": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewGraphExtractorWithLLM(f.LLM())
	},
	"structured_metadata": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewStructuredMetadataExtractorWithLLM(f.LLM())
	},
	"audio_extractor": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewAudioExtractor(steps.WithVideoProvider(f.Video()))
	},
	"frame_sampler": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewFrameSampler(steps.WithFrameSamplerVideoProvider(f.Video()))
	},
	"frame_ocr": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewFrameOCR(steps.WithFrameOCRProvider(f.OCR()))
	},
	"pdf_extractor": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewPDFExtractor(steps.WithDocumentProvider(f.Document()))
	},
	"office_extractor": func(f *providers.Factory) pipeline.PipelineStep {
		return steps.NewOfficeExtractor(steps.WithOfficeDocumentProvider(f.Document()))
	},
}

// resolveStep builds a PipelineStep from a step name, using BuildOpts for provider/blob-aware steps.
func resolveStep(name string, opts BuildOpts) (pipeline.PipelineStep, error) {
	if ctor, ok := embeddingStepConstructors[name]; ok {
		return ctor(opts), nil
	}
	if opts.Factory != nil {
		if ctor, ok := providerStepConstructors[name]; ok {
			return ctor(opts.Factory), nil
		}
	}
	guard := opts.FetchGuard
	if guard == nil {
		guard = safefetch.Default()
	}
	if ctor, ok := fetchStepConstructors[name]; ok {
		return ctor(guard), nil
	}
	if opts.BrowserClient != nil {
		if ctor, ok := browserStepConstructors[name]; ok {
			return ctor(opts.BrowserClient, guard), nil
		}
	}
	if opts.BlobStore != nil {
		if ctor, ok := blobStepConstructors[name]; ok {
			return ctor(opts.BlobStore, opts.BlobThreshold), nil
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
	case "audio_extractor":
		return steps.NewAudioExtractor(), nil
	case "frame_sampler":
		return steps.NewFrameSampler(), nil
	case "frame_ocr":
		return steps.NewFrameOCR(), nil
	case "pdf_extractor":
		return steps.NewPDFExtractor(), nil
	case "office_extractor":
		return steps.NewOfficeExtractor(), nil
	}
	return nil, fmt.Errorf("builtins: unknown step %q", name)
}

// stepSpec is one step of a pipeline being built: its name and, for a
// step pipelines.overrides inserted, the insert entry.
type stepSpec struct {
	name   string
	insert *config.StepInsert
}

func specsOf(names []string) []stepSpec {
	out := make([]stepSpec, len(names))
	for i, n := range names {
		out[i] = stepSpec{name: n}
	}
	return out
}

// buildPipeline constructs a Pipeline from a Def, optionally injecting providers.
// If strict is true, unsatisfied capabilities cause an error; otherwise they are pruned.
func buildPipeline(name string, d Def, opts BuildOpts, strict bool) (*pipeline.Pipeline, error) {
	return buildPipelineSpecs(name, d, specsOf(d.Steps), opts, strict)
}

// buildPipelineSpecs is buildPipeline over an explicit step list: a
// built-in step resolves through the constructor tables, an installed
// one through opts.External.
func buildPipelineSpecs(name string, d Def, specs []stepSpec, opts BuildOpts, strict bool) (*pipeline.Pipeline, error) {
	pipelineSteps := make([]pipeline.PipelineStep, 0, len(specs))
	for _, spec := range specs {
		var s pipeline.PipelineStep
		var err error
		if !IsStepName(spec.name) && opts.External != nil && opts.External.Has(spec.name) {
			ins := config.StepInsert{Step: spec.name}
			if spec.insert != nil {
				ins = *spec.insert
			}
			s, err = opts.External.Step(name, ins)
		} else {
			s, err = resolveStep(spec.name, opts)
		}
		if err != nil {
			return nil, fmt.Errorf("pipeline %q: %w", name, err)
		}
		pipelineSteps = append(pipelineSteps, s)
	}

	// Capability check: prune or reject steps with unsatisfied capabilities.
	caps := CapabilitiesFromOpts(opts)
	unsatisfied := pipeline.ValidateCapabilities(pipelineSteps, caps)
	if len(unsatisfied) > 0 {
		if strict {
			return nil, fmt.Errorf("pipeline %q: steps at indices %v require unavailable capabilities", name, unsatisfied)
		}
		for _, idx := range unsatisfied {
			log.Printf("builtins: pipeline %q: pruning step %q (missing capability)", name, pipelineSteps[idx].Name())
		}
		pipelineSteps = pipeline.RemoveIndices(pipelineSteps, unsatisfied)
	}

	// Apply per-step contract overrides.
	if len(d.Overrides) > 0 {
		for i, s := range pipelineSteps {
			if o, ok := d.Overrides[s.Name()]; ok {
				pipelineSteps[i] = pipeline.ApplyOverride(s, o)
			}
		}
	}

	// Composability check: verify data flow between steps.
	if err := pipeline.ValidateComposability(pipelineSteps); err != nil {
		return nil, fmt.Errorf("pipeline %q: %w", name, err)
	}

	return &pipeline.Pipeline{
		PipelineName: name,
		Description:  d.Description,
		Steps:        pipelineSteps,
	}, nil
}

// selector pairs a pipeline name with its extension, URL pattern, and content-test criteria.
type selector struct {
	PipelineName string
	Extensions   []string
	URLPattern   *regexp.Regexp
	ContentTest  func(string) bool
	Priority     int
}

// buildSelectors returns a list of selectors from the registered defs.
// URL-pattern selectors are sorted by pattern length descending so that
// more-specific patterns (longer strings) are tried before general ones.
func buildSelectors() []selector {
	sels := make([]selector, 0, len(defs))
	for name, d := range defs {
		if len(d.Extensions) > 0 || d.URLPattern != nil || d.ContentTest != nil {
			sels = append(sels, selector{
				PipelineName: name,
				Extensions:   d.Extensions,
				URLPattern:   d.URLPattern,
				ContentTest:  d.ContentTest,
				Priority:     d.Priority,
			})
		}
	}
	// Sort selectors:
	// 1. URL-pattern selectors first (non-nil URLPattern before nil).
	// 2. Among URL-pattern selectors: fewer alternations (|) = more specific; tie-break by length.
	// 3. Among ContentTest-only selectors: lower Priority value = higher priority.
	sort.SliceStable(sels, func(i, j int) bool {
		pi, pj := sels[i].URLPattern, sels[j].URLPattern
		if (pi == nil) != (pj == nil) {
			return pi != nil // URL-pattern selectors before content-test-only
		}
		if pi != nil && pj != nil {
			altsI := strings.Count(pi.String(), "|")
			altsJ := strings.Count(pj.String(), "|")
			if altsI != altsJ {
				return altsI < altsJ // fewer alternations = more specific
			}
			return len(pi.String()) > len(pj.String()) // longer = more specific
		}
		// Both are ContentTest-only: lower Priority wins.
		return sels[i].Priority < sels[j].Priority
	})
	return sels
}

// builtinSelectors splits the defs' rules by the signal each one reads: URL
// patterns and extensions match the source, content tests match the content.
func builtinSelectors(selectors []selector) pipeline.Selectors {
	return pipeline.Selectors{
		Source:  func(source string) (string, bool) { return selectBySource(selectors, source) },
		Content: func(content string) string { return selectByContent(selectors, content) },
	}
}

// selectBySource routes a source that names a location: an http/https URL
// goes to the first matching URL pattern, else url.generic; a file path goes
// to the pipeline claiming its extension. Anything else (a capture label, an
// unknown extension) reports ok=false.
func selectBySource(selectors []selector, source string) (string, bool) {
	source = strings.TrimSpace(source)
	lower := strings.ToLower(source)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		for _, sel := range selectors {
			if sel.URLPattern != nil && sel.URLPattern.MatchString(source) {
				return sel.PipelineName, true
			}
		}
		return "url.generic", true
	}

	ext := filepath.Ext(lower)
	if ext == "" {
		return "", false
	}
	for _, sel := range selectors {
		if slices.Contains(sel.Extensions, ext) {
			return sel.PipelineName, true
		}
	}
	return "", false
}

// selectByContent routes by the content's own shape: the first content test
// that matches, in Priority order; text.short when none does.
func selectByContent(selectors []selector, content string) string {
	for _, sel := range selectors {
		if sel.ContentTest != nil && sel.ContentTest(content) {
			return sel.PipelineName
		}
	}
	return "text.short"
}

// Registry builds a pipeline.Registry from all registered defs (no providers).
// Steps with unsatisfied capabilities are pruned silently.
func Registry() pipeline.Registry {
	return buildRegistry(BuildOpts{}, false)
}

// ConfiguredRegistry builds a pipeline.Registry with real providers injected.
// Steps with unsatisfied capabilities are pruned silently.
func ConfiguredRegistry(f *providers.Factory) pipeline.Registry {
	return buildRegistry(BuildOpts{Factory: f}, false)
}

// ConfiguredRegistryWithOpts builds a pipeline.Registry with full BuildOpts (providers + blob store).
func ConfiguredRegistryWithOpts(opts BuildOpts) pipeline.Registry {
	return buildRegistry(opts, false)
}

// ConfiguredRegistryStrict builds a pipeline.Registry that rejects pipelines
// with unsatisfied capabilities instead of pruning them.
func ConfiguredRegistryStrict(f *providers.Factory) pipeline.Registry {
	return buildRegistry(BuildOpts{Factory: f}, true)
}

// ConfiguredRegistryWithPipelineOverrides builds a registry from base where
// each pipeline can override individual provider backends via
// PipelinesConfig.Overrides (base.Factory is built from baseCfg), drop
// steps (skip_steps) and insert steps, built-in or installed
// (insert_steps). An override that names an unknown pipeline, step or
// anchor fails the build with one line per problem; nothing panics and
// nothing is ignored.
func ConfiguredRegistryWithPipelineOverrides(
	base BuildOpts,
	baseCfg config.ProvidersConfig,
	pipelinesCfg config.PipelinesConfig,
) (pipeline.Registry, error) {
	has := func(string) bool { return false }
	if base.External != nil {
		has = base.External.Has
	}
	verrs := append(pipelinesCfg.Validate(), ValidateOverrides(pipelinesCfg, base.Duplicates, has)...)
	if len(verrs) > 0 {
		lines := make([]string, len(verrs))
		for i, e := range verrs {
			lines[i] = e.Error()
		}
		return nil, fmt.Errorf("invalid pipelines config:\n  %s", strings.Join(lines, "\n  "))
	}

	r := pipeline.NewRegistry()
	selectors := buildSelectors()

	// Pre-compute capabilities to skip pipelines with unsatisfied providers.
	baseCaps := CapabilitiesFromOpts(base)

	for name, d := range defs {
		if !defProvidersSatisfied(d, baseCaps) {
			log.Printf("builtins: skipping pipeline %q (required provider(s) %v not available)", name, d.Providers)
			continue
		}

		opts := base
		d.Steps = InjectDedupStep(d.Steps, base.Duplicates)
		specs := specsOf(d.Steps)

		if override, ok := pipelinesCfg.Overrides[name]; ok {
			// 1. Structural overrides: skip_steps, then insert_steps.
			var problems []config.ValidationError
			specs, problems = applyStructuralOverrides(name, specs, override, nil)
			if len(problems) > 0 {
				return nil, errors.New("invalid pipelines config: " + problems[0].Error())
			}

			// 2. Handle provider overrides.
			if len(override.Providers) > 0 {
				merged := baseCfg
				for role, bc := range override.Providers {
					switch role {
					case "llm":
						merged.LLM = bc
					case "embedding":
						// Each registered model's entry decides its
						// provider (ADR-071); a pipeline cannot.
						log.Printf("builtins: pipeline %q: provider role \"embedding\" cannot be overridden per pipeline (ignored); the embedding model's registry entry decides its provider", name)
					case "ocr":
						merged.OCR = bc
					case "vision":
						merged.Vision = bc
					case "transcription":
						merged.Transcription = bc
					case "diarization":
						merged.Diarization = bc
					case "video":
						merged.Video = bc
					case "document":
						merged.Document = bc
					default:
						log.Printf("builtins: pipeline %q: unknown provider role %q in override (ignored)", name, role)
					}
				}
				// Same secrets store as the base factory: keys resolve identically.
				opts.Factory = base.Factory.WithConfig(merged)
			}
		}

		p, err := buildPipelineSpecs(name, d, specs, opts, false)
		if err != nil {
			return nil, err
		}
		if err := r.Register(name, p); err != nil {
			return nil, err
		}
	}

	r.SetSelectors(builtinSelectors(selectors))

	return r, nil
}

// ValidateOverrides checks every pipelines.overrides entry against the
// registered pipelines: the pipeline exists, each skip_steps name is one
// of its steps, each inserted step is built in or installed (installed
// reports which names are; nil means none), and each before/after anchor
// is in the pipeline once skip_steps and the earlier inserts apply.
// dedup counts as a pipeline step when dups.CheckSimilar inserts it.
// Problems come back one per field, sorted by pipeline.
func ValidateOverrides(p config.PipelinesConfig, dups config.DuplicatesConfig, installed func(string) bool) []config.ValidationError {
	names := make([]string, 0, len(p.Overrides))
	for name := range p.Overrides {
		names = append(names, name)
	}
	sort.Strings(names)

	var errs []config.ValidationError
	for _, name := range names {
		d, ok := defs[name]
		if !ok {
			errs = append(errs, config.ValidationError{
				Field:   "pipelines.overrides." + name,
				Message: fmt.Sprintf("unknown pipeline %q (dpkms pipeline list)", name),
			})
			continue
		}
		_, problems := applyStructuralOverrides(name, specsOf(InjectDedupStep(d.Steps, dups)), p.Overrides[name], installed)
		errs = append(errs, problems...)
	}
	return errs
}

// applyStructuralOverrides drops o.SkipSteps from specs, then applies
// o.InsertSteps in order. Every name that does not resolve is a problem;
// installed nil skips the inserted-step name check (the step builder
// reports it).
func applyStructuralOverrides(pipelineName string, specs []stepSpec, o config.PipelineOverride, installed func(string) bool) ([]stepSpec, []config.ValidationError) {
	ov := overrideApplier{pipeline: pipelineName, base: "pipelines.overrides." + pipelineName, installed: installed}
	out := slices.Clone(specs)
	for i, skip := range o.SkipSteps {
		if _, n := indexOfStep(out, skip); n == 0 {
			if _, inOrig := indexOfStep(specs, skip); inOrig == 0 {
				ov.notIn(fmt.Sprintf("%s.skip_steps[%d]", ov.base, i), skip, specs)
			}
			continue
		}
		out = slices.DeleteFunc(out, func(s stepSpec) bool { return s.name == skip })
	}
	for i := range o.InsertSteps {
		out = ov.insert(out, i, &o.InsertSteps[i])
	}
	return out, ov.problems
}

// overrideApplier collects the problems one pipeline's override has.
type overrideApplier struct {
	pipeline  string
	base      string
	installed func(string) bool
	problems  []config.ValidationError
}

func (ov *overrideApplier) problem(field, format string, args ...any) {
	ov.problems = append(ov.problems, config.ValidationError{Field: field, Message: fmt.Sprintf(format, args...)})
}

func (ov *overrideApplier) notIn(field, step string, in []stepSpec) {
	names := make([]string, len(in))
	for i, s := range in {
		names[i] = s.name
	}
	ov.problem(field, "step %q is not in pipeline %s (steps: %s)", step, ov.pipeline, strings.Join(names, ", "))
}

// insert places insert_steps[i] in out, or records why it cannot.
func (ov *overrideApplier) insert(out []stepSpec, i int, ins *config.StepInsert) []stepSpec {
	field := fmt.Sprintf("%s.insert_steps[%d]", ov.base, i)
	builtin := IsStepName(ins.Step)
	if !builtin && ov.installed != nil && !ov.installed(ins.Step) {
		ov.problem(field+".step", "unknown step %q: not a built-in step and not installed (dpkms pipeline step list)", ins.Step)
	}
	if builtin && (ins.OnError != "" || ins.Timeout != 0 || ins.Isolation != "" || len(ins.Config) > 0) {
		ov.problem(field, "on_error, timeout, isolation and config apply to installed steps only; %q is built in", ins.Step)
	}
	anchorField, anchor := "", ""
	switch {
	case ins.Before != "" && ins.After != "":
		return out // PipelinesConfig.Validate reports it
	case ins.Before != "":
		anchorField, anchor = field+".before", ins.Before
	case ins.After != "":
		anchorField, anchor = field+".after", ins.After
	default:
		return append(out, stepSpec{name: ins.Step, insert: ins})
	}
	idx, n := indexOfStep(out, anchor)
	switch {
	case n == 0:
		ov.notIn(anchorField, anchor, out)
		return out
	case n > 1:
		ov.problem(anchorField, "step %q appears %d times in pipeline %s; the anchor must be unique", anchor, n, ov.pipeline)
		return out
	}
	if ins.After != "" {
		idx++
	}
	return slices.Insert(out, idx, stepSpec{name: ins.Step, insert: ins})
}

// indexOfStep is the first index of step in specs and how often it occurs.
func indexOfStep(specs []stepSpec, step string) (idx, count int) {
	idx = -1
	for i, s := range specs {
		if s.name == step {
			if idx < 0 {
				idx = i
			}
			count++
		}
	}
	return idx, count
}

// IsStepName reports whether name is a built-in step.
func IsStepName(name string) bool {
	if _, ok := stepConstructors[name]; ok {
		return true
	}
	if _, ok := providerStepConstructors[name]; ok {
		return true
	}
	if _, ok := embeddingStepConstructors[name]; ok {
		return true
	}
	if _, ok := blobStepConstructors[name]; ok {
		return true
	}
	if _, ok := fetchStepConstructors[name]; ok {
		return true
	}
	if _, ok := browserStepConstructors[name]; ok {
		return true
	}
	return false
}

// defProvidersSatisfied returns true if all providers listed in d.Providers
// are present in caps. An empty Providers list is always satisfied.
func defProvidersSatisfied(d Def, caps pipeline.CapabilitySet) bool {
	for _, p := range d.Providers {
		if !caps[p] {
			return false
		}
	}
	return true
}

func buildRegistry(opts BuildOpts, strict bool) pipeline.Registry {
	r := pipeline.NewRegistry()
	selectors := buildSelectors()
	caps := CapabilitiesFromOpts(opts)

	for name, d := range defs {
		if !defProvidersSatisfied(d, caps) {
			log.Printf("builtins: skipping pipeline %q (required provider(s) %v not available)", name, d.Providers)
			continue
		}
		d.Steps = InjectDedupStep(d.Steps, opts.Duplicates)

		p, err := buildPipeline(name, d, opts, strict)
		if err != nil {
			panic(fmt.Sprintf("builtins: %v", err))
		}
		if err := r.Register(name, p); err != nil {
			panic(fmt.Sprintf("builtins: %v", err))
		}
	}

	r.SetSelectors(builtinSelectors(selectors))

	return r
}

// newDefaultEmailFilter creates an EmailFilter using the built-in default ruleset.
// It panics on invalid regex (which indicates a coding error, not a runtime error).
func newDefaultEmailFilter() pipeline.PipelineStep {
	f, err := steps.NewEmailFilter(steps.DefaultRuleset())
	if err != nil {
		panic(fmt.Sprintf("builtins: email_filter: %v", err))
	}
	return f
}

// Defs returns a copy of the registered pipeline definitions (for testing/inspection).
func Defs() map[string]Def {
	cp := make(map[string]Def, len(defs))
	for k, v := range defs {
		cp[k] = v
	}
	return cp
}

// InjectDedupStep inserts the dedup step after the embedding step in a pipeline
// definition when near-duplicate checking is enabled. dedup reads the
// vectors embedding writes, so it never runs before it; a pipeline without
// an embedding step, or one that already lists dedup, is returned as is.
// Both registry builders call it before per-pipeline skip_steps apply, so
// skip_steps: [dedup] opts a pipeline out.
func InjectDedupStep(stepNames []string, cfg config.DuplicatesConfig) []string {
	if !cfg.CheckSimilar || slices.Contains(stepNames, "dedup") {
		return stepNames
	}
	out := make([]string, 0, len(stepNames)+1)
	for _, s := range stepNames {
		out = append(out, s)
		if s == "embedding" {
			out = append(out, "dedup")
		}
	}
	return out
}

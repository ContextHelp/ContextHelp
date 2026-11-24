// Package providers supplies the media and model backends pipeline steps
// call (LLM, vision, OCR, transcription, diarization, video, document);
// Factory picks each from config. auto is local-only: it resolves to a local
// backend or, for a role with none, a stub that only tests reach (the role
// is unavailable). A hosted backend runs only when config names it. A named
// backend that is missing or unknown is a configuration error (Validate),
// and its getter returns a failed provider, never a stub.
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
	"hop.top/kit/go/storage/secret"
	"hop.top/kit/go/storage/secret/env"
)

// Factory resolves the provider for each role from configuration. A role's
// "auto" backend is local-only: it never reads hosted API keys to decide
// what to run. Hosted backends are selected only by name.
//
// The embedding provider is not built here: it has its own layered
// resolution (flags, env, -c, model registry, config, defaults) in
// internal/embeddings, which every embedding consumer goes through.
type Factory struct {
	cfg     config.ProvidersConfig
	secrets secret.Store
	// httpClient, when set, carries every hosted-API client's requests.
	// Nil means each client's default.
	httpClient *http.Client
	// ctx scopes tool lookups and CLI probes; nil means context.Background().
	// Tests carry a recorded-command seam in it.
	ctx context.Context
	// autoVision memoises the local vision probe: every caller of Vision
	// on this factory sees the same answer. Each derived factory gets its
	// own, because its config differs.
	autoVision *autoVisionResult
}

type autoVisionResult struct {
	once sync.Once
	p    vision.Provider
}

// Backend names: the values a providers.<kind>.backend key selects, and
// what each provider's Name reports.
const (
	backendStub       = "stub"
	backendAuto       = "auto" // local-only resolution; "" means the same
	backendOllama     = "ollama"
	backendOpenAI     = "openai"
	backendAnthropic  = "anthropic"
	backendGemini     = "gemini"
	backendOpenRouter = "openrouter"
	backendFFmpeg     = "ffmpeg"
	backendTesseract  = "tesseract"
	backendWhisper    = "whisper-cli"
)

// defaultOllamaEndpoint is where a local Ollama listens unless configured.
const defaultOllamaEndpoint = "http://localhost:11434"

// defaultOCRLanguage is tesseract's language when none is configured.
const defaultOCRLanguage = "eng"

// NewFactory creates a Factory from the providers configuration.
// If store is nil, an env-backed store is used.
func NewFactory(cfg config.ProvidersConfig, store secret.Store) *Factory {
	if store == nil {
		store = env.New("")
	}
	return &Factory{cfg: cfg, secrets: store, autoVision: &autoVisionResult{}}
}

// WithHTTPClient returns a copy of f whose hosted-API clients (OpenAI,
// Anthropic, Gemini, OpenRouter) send their requests through c.
func (f *Factory) WithHTTPClient(c *http.Client) *Factory {
	cp := *f
	cp.httpClient = c
	cp.autoVision = &autoVisionResult{}
	return &cp
}

// WithContext returns a copy of f whose tool lookups and CLI probes run
// under ctx, so a [WithCommandRunner] or [WithToolLookup] seam in ctx
// reaches them.
func (f *Factory) WithContext(ctx context.Context) *Factory {
	cp := *f
	cp.ctx = ctx
	cp.autoVision = &autoVisionResult{}
	return &cp
}

func (f *Factory) context() context.Context {
	if f.ctx != nil {
		return f.ctx
	}
	return context.Background()
}

func (f *Factory) lookupTool(name string) (string, error) {
	return LookupToolContext(f.context(), name)
}

// WithConfig returns a Factory for cfg that resolves API keys through the
// same secrets store as f. A nil f gets the env-backed store.
func (f *Factory) WithConfig(cfg config.ProvidersConfig) *Factory {
	if f == nil {
		return NewFactory(cfg, nil)
	}
	c := *f
	c.cfg = cfg
	// The vision probe answers for one vision config: a derived factory
	// that leaves it alone shares the answer instead of probing again.
	if cfg.Vision != f.cfg.Vision {
		c.autoVision = &autoVisionResult{}
	}
	return &c
}

// Hosted provider API key names: the secrets store key and the
// environment variable share the name.
const (
	keyOpenAI     = "OPENAI_API_KEY"
	keyAnthropic  = "ANTHROPIC_API_KEY"
	keyGemini     = "GEMINI_API_KEY"
	keyOpenRouter = "OPENROUTER_API_KEY"
)

// apiKey resolves a hosted provider's API key: the secrets store first,
// then the process environment. It is the only place provider keys are
// read; the factory passes the result to the client constructor, so the
// key that selects a provider in auto mode is the key that client sends.
// An empty value in the store counts as unset. "" means neither has it.
func (f *Factory) apiKey(name string) string {
	if got, err := f.secrets.Get(context.Background(), name); err == nil && len(got.Value) > 0 {
		return string(got.Value)
	}
	return os.Getenv(name)
}

// Validate reports every provider config that cannot work, as one error
// naming each offending config key:
//   - a backend name the role does not know, with the valid values, and a
//     frames or summarization setting that is out of range
//     (config.ValidateProviders);
//   - a named CLI backend that is not installed, with the install hint;
//   - a named ollama backend of vision, llm or summarization with no
//     model (providers.<role>.model);
//   - a named hosted backend whose API key does not resolve.
//
// Registry builds call it first, so a bad name, missing tool, model or key
// stops startup instead of degrading. auto, empty and stub always pass.
func (f *Factory) Validate() error {
	var errs []error
	for _, e := range config.ValidateProviders(f.cfg) {
		errs = append(errs, e)
	}
	for _, r := range []struct {
		role string
		cfg  config.ProviderBackendConfig
		get  func() any // nil: no tool to find
	}{
		{"video", f.cfg.Video, func() any { return f.Video() }},
		{"frames", f.cfg.Frames, func() any { return f.Frames() }},
		{"document", f.cfg.Document, func() any { return f.Document() }},
		{"ocr", f.cfg.OCR, func() any { return f.OCR() }},
		{"transcription", f.cfg.Transcription, func() any { return f.Transcription() }},
		{"diarization", f.cfg.Diarization, func() any { return f.Diarization() }},
		{"vision", f.cfg.Vision, nil},
		{"llm", f.cfg.LLM, nil},
		{"summarization", f.cfg.Summarization, nil},
	} {
		b := r.cfg.Backend
		if b == "" || b == backendAuto || b == backendStub || !slices.Contains(roleBackends[r.role], b) {
			continue // always valid, or an unknown name already reported
		}
		if r.get != nil {
			if u, ok := r.get().(interface{ Unavailable() error }); ok {
				errs = append(errs, u.Unavailable())
			}
		}
		if err := f.namedBackendError(r.role, r.cfg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// hostedKeys names the API key each hosted backend needs.
var hostedKeys = map[string]string{
	backendOpenAI: keyOpenAI, backendAnthropic: keyAnthropic,
	backendGemini: keyGemini, backendOpenRouter: keyOpenRouter,
}

// namedBackendError reports why a role's named backend cannot run for want
// of config: an ollama backend with no model to run, or a hosted backend
// with no API key.
func (f *Factory) namedBackendError(role string, c config.ProviderBackendConfig) error {
	if c.Backend == backendOllama && c.Model == "" && slices.Contains([]string{"vision", "llm", "summarization"}, role) {
		return config.ValidationError{Field: "providers." + role + ".model", Message: "backend ollama needs a model: set providers." + role + ".model"}
	}
	if key, hosted := hostedKeys[c.Backend]; hosted && f.apiKey(key) == "" {
		return config.ValidationError{Field: "providers." + role + ".backend", Message: fmt.Sprintf("backend %q needs %s: set it in the secrets store or the environment", c.Backend, key)}
	}
	return nil
}

// missingTool returns the first tool of names that is not installed.
func (f *Factory) missingTool(names ...string) error {
	for _, n := range names {
		if _, err := f.lookupTool(n); err != nil {
			return err
		}
	}
	return nil
}

// Resolved is each provider role resolved once. A registry build resolves
// the factory once and uses it for both the capability check and the step
// constructors, so a probe that succeeds for one cannot fail for the other
// and register a stub, and probes and keyring lookups happen once.
type Resolved struct {
	LLM           LLMProvider
	OCR           OCRProvider
	Vision        vision.Provider
	Transcription TranscriptionProvider
	Diarization   DiarizationProvider
	Video         VideoProvider
	Frames        FrameProvider
	FrameOptions  FrameOptions
	Summarization SummarizationProvider
	Document      DocumentProvider
}

// Resolve resolves every role once.
func (f *Factory) Resolve() *Resolved {
	return &Resolved{
		LLM: f.LLM(), OCR: f.OCR(), Vision: f.Vision(), Transcription: f.Transcription(),
		Diarization: f.Diarization(), Video: f.Video(), Frames: f.Frames(),
		FrameOptions: f.FrameOptions(), Summarization: f.Summarization(), Document: f.Document(),
	}
}

// Video returns the best available VideoProvider. A named backend that
// cannot run yields a failed provider, never a stub.
func (f *Factory) Video() VideoProvider {
	switch b := f.cfg.Video.Backend; b {
	case backendFFmpeg:
		return f.mustFFmpeg()
	case backendStub:
		return NewStubVideoProvider()
	case "", backendAuto:
		if p := f.tryFFmpeg(); p != nil {
			return p
		}
		slog.Debug("providers: no video backend found")
		return NewStubVideoProvider()
	default:
		return failedVideo{failed{unknownBackendError("video", b)}}
	}
}

// Frames returns the FrameProvider for the frames role. ffmpeg is the
// interim local default. auto without ffmpeg is a stub (the role is
// unavailable); a named ffmpeg that is missing, or an unknown name, is a
// failed provider, never a stub.
func (f *Factory) Frames() FrameProvider {
	switch b := f.cfg.Frames.Backend; b {
	case backendFFmpeg:
		if err := f.missingTool(toolFFmpeg, "ffprobe"); err != nil {
			return failedFrames{failed{backendError("frames", backendFFmpeg, err)}}
		}
		return NewFFmpegFrameProvider()
	case backendStub:
		return NewStubFrameProvider()
	case "", backendAuto:
		if f.missingTool(toolFFmpeg, "ffprobe") == nil {
			return NewFFmpegFrameProvider()
		}
		slog.Debug("providers: no frames backend found")
		return NewStubFrameProvider()
	default:
		return failedFrames{failed{unknownBackendError("frames", b)}}
	}
}

// FrameOptions returns the frame selection options from providers.frames:
// scene detection on unless disabled, and each unset bound, the frame
// cap and the frame size from the built-in defaults.
func (f *Factory) FrameOptions() FrameOptions {
	c := f.cfg.Frames
	return FrameOptions{
		MinFPS:         c.MinFPS,
		MaxFPS:         c.MaxFPS,
		SceneDetection: c.SceneDetection == nil || *c.SceneDetection,
		SceneThreshold: c.SceneThreshold,
		MaxFrames:      c.MaxFrames,
		MaxDimension:   c.MaxDimension,
	}.WithDefaults()
}

// Summarization returns the SummarizationProvider. The local Ollama chat
// model is the default; a hosted backend runs only when named. A bad
// prompt template or an unknown backend comes back as a provider whose
// Summarize fails with it.
func (f *Factory) Summarization() SummarizationProvider {
	c := f.cfg.Summarization
	prompt, err := NewSummaryPrompt(c.PromptTemplate)
	if err != nil {
		return failedSummarizer{failed{fmt.Errorf("providers.summarization.prompt_template: %w", err)}}
	}
	switch b := c.Backend; b {
	case backendStub:
		return NewStubSummarizationProvider()
	case backendOpenAI:
		llm := NewOpenAILLMProvider(f.apiKey(keyOpenAI), c.Model)
		if f.httpClient != nil {
			llm = llm.WithHTTPClient(f.httpClient)
		}
		return NewLLMSummarizer(llm, prompt)
	case backendAnthropic:
		llm := NewAnthropicLLMProvider(f.apiKey(keyAnthropic), c.Model)
		if f.httpClient != nil {
			llm = llm.WithHTTPClient(f.httpClient)
		}
		return NewLLMSummarizer(llm, prompt)
	case "", backendAuto, backendOllama: // local only
		s := NewOllamaSummarizer(c.Endpoint, c.Model, prompt).WithNumCtx(c.NumCtx)
		if f.httpClient != nil {
			s = s.WithHTTPClient(f.httpClient)
		}
		return s
	default:
		return failedSummarizer{failed{unknownBackendError("summarization", b)}}
	}
}

// Document returns the best available DocumentProvider.
func (f *Factory) Document() DocumentProvider {
	switch b := f.cfg.Document.Backend; b {
	case pdftotextTool:
		return f.mustPdftotext()
	case backendGolib, "", backendAuto:
		// golib is pure Go and always available; auto prefers it.
		return f.newGolib()
	case backendStub:
		return NewStubDocumentProvider()
	default:
		return failedDocument{failed{unknownBackendError("document", b)}}
	}
}

// OCR returns the best available OCRProvider.
func (f *Factory) OCR() OCRProvider {
	switch b := f.cfg.OCR.Backend; b {
	case backendTesseract:
		return f.mustTesseract()
	case backendStub:
		return NewStubOCRProvider()
	case "", backendAuto:
		if p := f.tryTesseract(); p != nil {
			return p
		}
		slog.Debug("providers: no OCR backend found")
		return NewStubOCRProvider()
	default:
		return failedOCR{failed{unknownBackendError("ocr", b)}}
	}
}

// Transcription returns the best available TranscriptionProvider.
func (f *Factory) Transcription() TranscriptionProvider {
	switch b := f.cfg.Transcription.Backend; b {
	case backendWhisper:
		return f.mustWhisper()
	case backendStub:
		return NewStubTranscriptionProvider()
	case "", backendAuto:
		if p := f.tryWhisper(); p != nil {
			return p
		}
		slog.Debug("providers: no transcription backend found")
		return NewStubTranscriptionProvider()
	default:
		return failedTranscription{failed{unknownBackendError("transcription", b)}}
	}
}

// Vision returns the best available vision.Provider.
func (f *Factory) Vision() vision.Provider {
	switch f.cfg.Vision.Backend {
	case backendOllama:
		return f.newOllamaVision()
	case backendOpenAI:
		return f.openAIVision(f.apiKey(keyOpenAI))
	case backendAnthropic:
		return f.anthropicVision(f.apiKey(keyAnthropic))
	case backendGemini:
		return f.geminiVision(f.apiKey(keyGemini))
	case backendOpenRouter:
		return f.openRouterVision(f.apiKey(keyOpenRouter))
	case backendStub:
		return vision.NewStubProvider()
	default: // "auto": local Ollama or unavailable, never a hosted backend
		f.autoVision.once.Do(func() {
			if p := f.tryOllamaVision(); p != nil {
				f.autoVision.p = p
				return
			}
			slog.Debug("providers: no local vision backend found, using stub")
			f.autoVision.p = vision.NewStubProvider()
		})
		return f.autoVision.p
	}
}

// Diarization returns the best available DiarizationProvider.
func (f *Factory) Diarization() DiarizationProvider {
	switch b := f.cfg.Diarization.Backend; b {
	case pyannoteName:
		return f.mustPyannote()
	case backendStub:
		return NewStubDiarizationProvider()
	case "", backendAuto:
		if p := f.tryPyannote(); p != nil {
			return p
		}
		slog.Debug("providers: no diarization backend found")
		return NewStubDiarizationProvider()
	default:
		return failedDiarization{failed{unknownBackendError("diarization", b)}}
	}
}

// LLM returns the best available LLMProvider.
func (f *Factory) LLM() LLMProvider {
	switch f.cfg.LLM.Backend {
	case backendOpenAI:
		return f.openAILLM(f.apiKey(keyOpenAI))
	case backendAnthropic:
		return f.anthropicLLM(f.apiKey(keyAnthropic))
	case backendOllama:
		return f.ollamaLLM()
	case backendStub:
		return NewStubLLMProvider()
	default: // "auto" or empty: local Ollama; hosted only when named
		return f.ollamaLLM()
	}
}

func (f *Factory) ollamaLLM() LLMProvider {
	p := NewOllamaLLMProvider(f.localEndpoint(f.cfg.LLM), f.cfg.LLM.Model)
	if f.httpClient != nil {
		p = p.WithHTTPClient(f.httpClient)
	}
	return p
}

// --- Hosted LLMs ---

func (f *Factory) openAILLM(key string) LLMProvider {
	p := NewOpenAILLMProvider(key, hostedModel(f.cfg.LLM.Model, config.DefaultLLMModel))
	if f.httpClient != nil {
		p = p.WithHTTPClient(f.httpClient)
	}
	return p
}

func (f *Factory) anthropicLLM(key string) LLMProvider {
	p := NewAnthropicLLMProvider(key, hostedModel(f.cfg.LLM.Model, config.DefaultLLMModel))
	if f.httpClient != nil {
		p = p.WithHTTPClient(f.httpClient)
	}
	return p
}

// --- FFmpeg ---

func (f *Factory) tryFFmpeg() VideoProvider {
	if f.missingTool(toolFFmpeg, "ffprobe") != nil {
		return nil
	}
	return NewFFmpegVideoProvider()
}

func (f *Factory) mustFFmpeg() VideoProvider {
	if p := f.tryFFmpeg(); p != nil {
		return p
	}
	return failedVideo{failed{backendError("video", backendFFmpeg, f.missingTool(toolFFmpeg, "ffprobe"))}}
}

// --- Golib (document) ---

func (f *Factory) newGolib() DocumentProvider {
	return NewGolibDocumentProvider()
}

// --- Pdftotext ---

func (f *Factory) tryPdftotext() DocumentProvider {
	if _, err := f.lookupTool(pdftotextTool); err != nil {
		return nil
	}
	return NewPdftotextDocumentProvider()
}

func (f *Factory) mustPdftotext() DocumentProvider {
	if p := f.tryPdftotext(); p != nil {
		return p
	}
	return failedDocument{failed{backendError("document", pdftotextTool, f.missingTool(pdftotextTool))}}
}

// --- Tesseract ---

func (f *Factory) tryTesseract() OCRProvider {
	if _, err := f.lookupTool(toolTesseract); err != nil {
		return nil
	}
	lang := f.cfg.OCR.Language
	if lang == "" {
		lang = defaultOCRLanguage
	}
	return NewTesseractOCRProvider(lang)
}

func (f *Factory) mustTesseract() OCRProvider {
	if p := f.tryTesseract(); p != nil {
		return p
	}
	return failedOCR{failed{backendError("ocr", backendTesseract, f.missingTool(toolTesseract))}}
}

// --- Whisper ---

// whisperBinary finds whisper-cli, which Homebrew's whisper-cpp installs,
// or the older whisper-cpp name. The openai-whisper "whisper" binary takes
// other flags and is never used.
func (f *Factory) whisperBinary() string {
	for _, name := range []string{backendWhisper, "whisper-cpp"} {
		if _, err := f.lookupTool(name); err == nil {
			return name
		}
	}
	return ""
}

// whisperModelError is why no whisper model is usable: whisper.cpp cannot
// run without one, so the backend is unavailable until
// providers.transcription.model names an existing file.
func (f *Factory) whisperModelError() error {
	model := f.cfg.Transcription.Model
	if model == "" {
		return errors.New("providers.transcription.model is not set: whisper-cli needs a whisper.cpp model file")
	}
	if st, err := os.Stat(model); err != nil || st.IsDir() {
		return fmt.Errorf("providers.transcription.model %q is not a readable file", model)
	}
	return nil
}

func (f *Factory) tryWhisper() TranscriptionProvider {
	bin := f.whisperBinary()
	if bin == "" || f.whisperModelError() != nil {
		return nil
	}
	return NewWhisperTranscriptionProvider(bin)
}

func (f *Factory) mustWhisper() TranscriptionProvider {
	if f.whisperBinary() == "" {
		return failedTranscription{failed{backendError("transcription", backendWhisper, &ToolNotInstalledError{
			Tool: backendWhisper, Hint: installHints[backendWhisper][runtime.GOOS],
		})}}
	}
	if err := f.whisperModelError(); err != nil {
		return failedTranscription{failed{backendError("transcription", backendWhisper, err)}}
	}
	return f.tryWhisper()
}

// --- Vision providers ---

// localEndpoint is the Ollama endpoint a role's config names, or the
// local default.
func (f *Factory) localEndpoint(c config.ProviderBackendConfig) string {
	if c.Endpoint != "" {
		return c.Endpoint
	}
	return defaultOllamaEndpoint
}

// hostedModel is the model a hosted backend is asked for: the configured
// one, unless it is the built-in local default (a local model name means
// nothing to a hosted API), in which case "" selects the backend's own
// default.
func hostedModel(model, localDefault string) string {
	if model == localDefault {
		return ""
	}
	return model
}

// visionProbeTimeout bounds the local vision probe.
const visionProbeTimeout = 2 * time.Second

// tryOllamaVision probes the local Ollama endpoint. Returns nil if it is
// unreachable, no vision model is configured, or the configured model is
// not installed. The probe runs under the factory's context and sends its
// request through the factory's HTTP client, when one is injected.
func (f *Factory) tryOllamaVision() vision.Provider {
	model := f.cfg.Vision.Model
	if model == "" {
		return nil
	}
	endpoint := f.localEndpoint(f.cfg.Vision)
	ctx, cancel := context.WithTimeout(f.context(), visionProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/api/tags", http.NoBody)
	if err != nil {
		return nil
	}
	client := f.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var tags struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTagsBody)).Decode(&tags); err != nil {
		return nil
	}
	for _, m := range tags.Models {
		if sameOllamaModel(m.Name, model) || sameOllamaModel(m.Model, model) {
			return f.ollamaVision(endpoint, model)
		}
	}
	slog.Debug("providers: vision model not installed in local ollama", "model", model)
	return nil
}

// sameOllamaModel compares model names the way Ollama resolves them: a
// name without a tag means ":latest".
func sameOllamaModel(a, b string) bool {
	return withLatest(a) == withLatest(b)
}

// maxTagsBody bounds the /api/tags response the vision probe reads.
const maxTagsBody = 4 << 20

// withLatest adds ":latest" to a name with no tag. A tag follows the last
// "/": the ":" in a registry host's port ("localhost:5000/llava") is not
// one.
func withLatest(name string) string {
	last := name[strings.LastIndex(name, "/")+1:]
	if name == "" || strings.Contains(last, ":") {
		return name
	}
	return name + ":latest"
}

func (f *Factory) ollamaVision(endpoint, model string) vision.Provider {
	p := vision.NewOllamaProvider(endpoint, model)
	if f.httpClient != nil {
		p = p.WithHTTPClient(f.httpClient)
	}
	return p
}

func (f *Factory) newOllamaVision() vision.Provider {
	return f.ollamaVision(strings.TrimRight(f.localEndpoint(f.cfg.Vision), "/"), f.cfg.Vision.Model)
}

func (f *Factory) openAIVision(key string) vision.Provider {
	p := vision.NewOpenAIProvider(key, hostedModel(f.cfg.Vision.Model, config.DefaultVisionModel))
	if f.httpClient != nil {
		p = p.WithHTTPClient(f.httpClient)
	}
	return p
}

func (f *Factory) anthropicVision(key string) vision.Provider {
	p := vision.NewAnthropicProvider(key, hostedModel(f.cfg.Vision.Model, config.DefaultVisionModel))
	if f.httpClient != nil {
		p = p.WithHTTPClient(f.httpClient)
	}
	return p
}

func (f *Factory) geminiVision(key string) vision.Provider {
	p := vision.NewGeminiProvider(key, hostedModel(f.cfg.Vision.Model, config.DefaultVisionModel))
	if f.httpClient != nil {
		p = p.WithHTTPClient(f.httpClient)
	}
	return p
}

func (f *Factory) openRouterVision(key string) vision.Provider {
	p := vision.NewOpenRouterProvider(key, hostedModel(f.cfg.Vision.Model, config.DefaultVisionModel))
	if f.httpClient != nil {
		p = p.WithHTTPClient(f.httpClient)
	}
	return p
}

// --- Pyannote ---

func (f *Factory) tryPyannote() DiarizationProvider {
	// Try standard binary names.
	for _, name := range []string{"pyannote-audio", pyannoteName} {
		if path, err := f.lookupTool(name); err == nil {
			return NewPyannoteDiarizationProvider(path)
		}
	}

	// Try python module.
	if _, err := f.lookupTool("python3"); err == nil {
		// Quick check if module is importable.
		res, err := RunCommand(f.context(), "python3", "-c", "import pyannote.audio; print('ok')")
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
	return failedDiarization{failed{backendError("diarization", pyannoteName, f.missingTool(pyannoteName))}}
}

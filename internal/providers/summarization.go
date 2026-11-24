package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// Media kinds a summary is written for; they pick the built-in prompt.
const (
	MediumGeneric = ""
	MediumAudio   = "audio"
	MediumVideo   = "video"
	MediumMeeting = "meeting"
)

// SummarizeRequest is the text to summarize and what it came from.
type SummarizeRequest struct {
	// Medium selects the built-in prompt: MediumMeeting, MediumVideo,
	// MediumAudio, or anything else for a generic summary.
	Medium string
	// Text is the transcript or document text.
	Text string
	// Language is a hint for the summary's language; empty means the text's.
	Language string
}

// SummaryResult is a produced summary and where it came from.
type SummaryResult struct {
	Text string
	// Provider and Model are the provenance of the summary.
	Provider string
	Model    string
}

// SummarizationProvider condenses text into a summary (the summarization
// role).
type SummarizationProvider interface {
	Name() string
	Summarize(ctx context.Context, req SummarizeRequest) (*SummaryResult, error)
}

// Summarization errors.
var (
	// ErrEmptySummaryInput: there is no text to summarize.
	ErrEmptySummaryInput = errors.New("summarization: empty text")
	// ErrNoSummaryModel: the backend needs a model and none is configured.
	ErrNoSummaryModel = errors.New("summarization: no model configured (set providers.summarization.model)")
	// ErrInvalidPromptTemplate: a configured prompt template cannot be used.
	ErrInvalidPromptTemplate = errors.New("summarization: invalid prompt template")
)

// Built-in prompt templates, one per medium. Templates are text/template
// with .Text, .Medium and .Language. The instructions follow the text: a
// model that truncates a long input from the front would otherwise lose
// them.
const (
	promptGeneric = `Text:
{{.Text}}

Summarize the text above in a short paragraph. Keep names, numbers and decisions exact. Do not add anything that is not in the text.{{if .Language}} Write the summary in {{.Language}}.{{end}}`
	promptAudio = `Transcript:
{{.Text}}

Summarize the audio transcript above as a short overview of what is discussed. Keep names, numbers and decisions exact. Do not add anything that is not in the transcript.{{if .Language}} Write the summary in {{.Language}}.{{end}}`
	promptVideo = `Transcript:
{{.Text}}

Summarize the video transcript above as a short overview: the subject, the main points in order, and any conclusion. Keep names, numbers and decisions exact. Do not add anything that is not in the transcript.{{if .Language}} Write the summary in {{.Language}}.{{end}}`
	promptMeeting = `Transcript:
{{.Text}}

Summarize the meeting transcript above under three headings: Decisions, Action items (with owner when stated), Topics discussed. Keep names, numbers and dates exact. Leave a heading out when the transcript has nothing for it. Do not add anything that is not in the transcript.{{if .Language}} Write the summary in {{.Language}}.{{end}}`
)

// BuiltinSummaryPrompt returns the built-in prompt template for a medium.
func BuiltinSummaryPrompt(medium string) string {
	switch medium {
	case MediumMeeting:
		return promptMeeting
	case MediumVideo:
		return promptVideo
	case MediumAudio:
		return promptAudio
	default:
		return promptGeneric
	}
}

var builtinSummaryTemplates = func() map[string]*template.Template {
	m := map[string]*template.Template{}
	for _, medium := range []string{MediumGeneric, MediumAudio, MediumVideo, MediumMeeting} {
		t, err := config.ParseSummaryTemplate(BuiltinSummaryPrompt(medium))
		if err != nil {
			panic(err) // a built-in prompt that fails its own check is a bug
		}
		m[medium] = t
	}
	return m
}()

// SummaryPrompt renders the prompt for a request from a configured
// template, or from the built-in template for the request's medium when
// none is configured.
type SummaryPrompt struct {
	tmpl *template.Template
}

// NewSummaryPrompt parses and checks a configured template (the check is
// config.ParseSummaryTemplate, the same one config validation runs).
// Empty means the built-in prompts.
func NewSummaryPrompt(text string) (SummaryPrompt, error) {
	if text == "" {
		return SummaryPrompt{}, nil
	}
	t, err := config.ParseSummaryTemplate(text)
	if err != nil {
		return SummaryPrompt{}, fmt.Errorf("%w: %w", ErrInvalidPromptTemplate, err)
	}
	return SummaryPrompt{tmpl: t}, nil
}

// Render returns the prompt for req.
func (p SummaryPrompt) Render(req SummarizeRequest) (string, error) {
	if strings.TrimSpace(req.Text) == "" {
		return "", ErrEmptySummaryInput
	}
	t := p.tmpl
	if t == nil {
		var ok bool
		if t, ok = builtinSummaryTemplates[req.Medium]; !ok {
			t = builtinSummaryTemplates[MediumGeneric]
		}
	}
	var b bytes.Buffer
	if err := t.Execute(&b, config.SummaryTemplateData{Text: req.Text, Medium: req.Medium, Language: req.Language}); err != nil {
		return "", err
	}
	return b.String(), nil
}

// LLMSummarizer summarizes through any LLMProvider: the route for hosted
// backends (OpenAI, Anthropic), which are chosen per role in config.
type LLMSummarizer struct {
	llm    LLMProvider
	model  string
	prompt SummaryPrompt
}

// modelNamer is implemented by LLM backends that report the model they
// resolved, defaults included.
type modelNamer interface{ Model() string }

// NewLLMSummarizer wraps llm. The model recorded as provenance is the one
// llm resolved, so it is never empty for the backends that report it.
func NewLLMSummarizer(llm LLMProvider, prompt SummaryPrompt) *LLMSummarizer {
	model := ""
	if m, ok := llm.(modelNamer); ok {
		model = m.Model()
	}
	return &LLMSummarizer{llm: llm, model: model, prompt: prompt}
}

func (s *LLMSummarizer) Name() string { return s.llm.Name() }

func (s *LLMSummarizer) Summarize(ctx context.Context, req SummarizeRequest) (*SummaryResult, error) {
	prompt, err := s.prompt.Render(req)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, err := s.llm.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("summarization (%s): %w", s.llm.Name(), err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, fmt.Errorf("summarization (%s): empty summary", s.llm.Name())
	}
	return &SummaryResult{Text: out, Provider: s.llm.Name(), Model: s.model}, nil
}

// StubSummarizationProvider returns a placeholder summary for testing.
type StubSummarizationProvider struct{}

func NewStubSummarizationProvider() *StubSummarizationProvider {
	return &StubSummarizationProvider{}
}

func (p *StubSummarizationProvider) Name() string { return backendStub }

func (p *StubSummarizationProvider) Summarize(_ context.Context, req SummarizeRequest) (*SummaryResult, error) {
	if strings.TrimSpace(req.Text) == "" {
		return nil, ErrEmptySummaryInput
	}
	return &SummaryResult{Text: "[Summary of text]", Provider: backendStub, Model: backendStub}, nil
}

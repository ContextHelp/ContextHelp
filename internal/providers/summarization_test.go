package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestSummaryPromptBuiltinPerMedium(t *testing.T) {
	p, err := NewSummaryPrompt("")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		MediumMeeting: "Action items",
		MediumVideo:   "video transcript",
		MediumAudio:   "audio transcript",
		MediumGeneric: "Summarize the text",
	}
	for medium, frag := range want {
		out, err := p.Render(SummarizeRequest{Medium: medium, Text: "BODY"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, frag) || !strings.Contains(out, "BODY") {
			t.Errorf("medium %q: prompt lacks %q or the text:\n%s", medium, frag, out)
		}
	}
}

func TestSummaryPromptCustomTemplate(t *testing.T) {
	p, err := NewSummaryPrompt("In {{.Language}}, one line for a {{.Medium}}: {{.Text}}")
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Render(SummarizeRequest{Medium: "video", Language: "fr", Text: "BODY"})
	if err != nil || out != "In fr, one line for a video: BODY" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestSummaryPromptInstructionsFollowTheText(t *testing.T) {
	// A model that truncates a long input from the front must not lose
	// the instructions.
	p, _ := NewSummaryPrompt("")
	for _, medium := range []string{MediumGeneric, MediumAudio, MediumVideo, MediumMeeting} {
		out, err := p.Render(SummarizeRequest{Medium: medium, Text: "BODY"})
		if err != nil {
			t.Fatal(err)
		}
		if i := strings.Index(out, "BODY"); i < 0 || !strings.Contains(out[i:], "Summarize") {
			t.Errorf("medium %q: instructions do not follow the text:\n%s", medium, out)
		}
	}
}

func TestSummaryPromptRejectsBadTemplates(t *testing.T) {
	for _, tmpl := range []string{
		"{{.Text",
		"no text placeholder",
		"{{.Nope}} {{.Text}}",
		// The text is only placed on some paths, or not as itself.
		"{{if .Language}}{{.Text}}{{end}}",
		"{{with .Medium}}{{.Text}}{{end}}",
		`{{range .Text}}{{.}}{{end}}`,
		`{{.Text | printf "%.5s"}}`,
		`{{len .Text}}`,
		"{{$t := .Text}}",
		// An unknown field in a branch only some requests take.
		`{{.Text}}{{if eq .Medium "meeting"}}{{.Missing}}{{end}}`,
		`{{.Text}}{{if .Language}}{{.Missing}}{{end}}`,
	} {
		if _, err := NewSummaryPrompt(tmpl); !errors.Is(err, ErrInvalidPromptTemplate) {
			t.Errorf("%q: err = %v", tmpl, err)
		}
	}
}

func TestOllamaSummarizerRequest(t *testing.T) {
	var gotURL, gotBody string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotURL, gotBody = r.URL.String(), string(b)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"message":{"content":" done "}}`)), Request: r}, nil
	})}
	p, _ := NewSummaryPrompt("")
	s := NewOllamaSummarizer("http://host:1/", "chat-m", p).WithHTTPClient(client)
	res, err := s.Summarize(context.Background(), SummarizeRequest{Medium: MediumMeeting, Text: "BODY"})
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "http://host:1/api/chat" {
		t.Errorf("url %q", gotURL)
	}
	for _, frag := range []string{`"model":"chat-m"`, `"stream":false`, "BODY", "Action items"} {
		if !strings.Contains(gotBody, frag) {
			t.Errorf("request body lacks %s: %s", frag, gotBody)
		}
	}
	if res.Text != "done" || res.Provider != "ollama" || res.Model != "chat-m" {
		t.Errorf("result %+v", res)
	}
}

func TestOllamaSummarizerNumCtx(t *testing.T) {
	var bodies []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"message":{"content":"ok"}}`)), Request: r}, nil
	})}
	p, _ := NewSummaryPrompt("")
	base := NewOllamaSummarizer("", "m", p).WithHTTPClient(client)
	for _, s := range []*OllamaSummarizer{base, base.WithNumCtx(32768)} {
		if _, err := s.Summarize(context.Background(), SummarizeRequest{Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(bodies[0], "num_ctx") {
		t.Errorf("unset num_ctx was sent: %s", bodies[0])
	}
	if !strings.Contains(bodies[1], `"options":{"num_ctx":32768}`) {
		t.Errorf("num_ctx missing: %s", bodies[1])
	}
}

func TestLLMSummarizerRecordsResolvedModel(t *testing.T) {
	p, _ := NewSummaryPrompt("")
	for name, llm := range map[string]LLMProvider{
		"openai":    NewOpenAILLMProvider("k", ""),
		"anthropic": NewAnthropicLLMProvider("k", ""),
	} {
		if got := NewLLMSummarizer(llm, p).model; got == "" {
			t.Errorf("%s: default model not recorded", name)
		}
	}
	// Ollama has no silent default model: it comes from config.
	if got := NewLLMSummarizer(NewOllamaLLMProvider("", ""), p).model; got != "" {
		t.Errorf("ollama: model %q recorded without being configured", got)
	}
	if got := NewLLMSummarizer(NewOllamaLLMProvider("", "chosen"), p).model; got != "chosen" {
		t.Errorf("ollama configured model: %q", got)
	}
	if got := NewLLMSummarizer(NewOpenAILLMProvider("k", "chosen"), p).model; got != "chosen" {
		t.Errorf("configured model: %q", got)
	}
}

func TestFactorySummarizationWithoutModel(t *testing.T) {
	f := NewFactory(config.ProvidersConfig{}, nil) // auto, nothing configured
	_, err := f.Summarization().Summarize(context.Background(), SummarizeRequest{Text: "x"})
	if !errors.Is(err, ErrNoSummaryModel) {
		t.Fatalf("err = %v, want ErrNoSummaryModel", err)
	}
}

func TestFactoryUnknownRoleBackends(t *testing.T) {
	f := NewFactory(config.ProvidersConfig{
		Summarization: config.ProviderBackendConfig{Backend: "gpt-sorcery"},
		Frames:        config.ProviderBackendConfig{Backend: "magic"},
	}, nil)
	if _, err := f.Summarization().Summarize(context.Background(), SummarizeRequest{Text: "x"}); err == nil || !strings.Contains(err.Error(), "unknown backend") {
		t.Errorf("summarization: err = %v", err)
	}
	if err := f.Frames().ExtractFrames(context.Background(), FileSource("/x.mp4"), DefaultFrameOptions(), func(Frame) error { return nil }); err == nil || !strings.Contains(err.Error(), "unknown backend") {
		t.Errorf("frames: err = %v", err)
	}
}

func TestOllamaSummarizerNeedsModel(t *testing.T) {
	p, _ := NewSummaryPrompt("")
	_, err := NewOllamaSummarizer("", "", p).Summarize(context.Background(), SummarizeRequest{Text: "x"})
	if !errors.Is(err, ErrNoSummaryModel) {
		t.Fatalf("err = %v", err)
	}
}

func TestOllamaSummarizerErrorStatus(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{"error":"model not found"}`)), Request: r}, nil
	})}
	p, _ := NewSummaryPrompt("")
	_, err := NewOllamaSummarizer("", "m", p).WithHTTPClient(client).Summarize(context.Background(), SummarizeRequest{Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestFactorySummarization(t *testing.T) {
	cfg := func(c config.ProviderBackendConfig) *Factory {
		return NewFactory(config.ProvidersConfig{Summarization: c}, nil)
	}
	if got := cfg(config.ProviderBackendConfig{}).Summarization().Name(); got != "ollama" {
		t.Errorf("auto: %q, want ollama (local only)", got)
	}
	if got := cfg(config.ProviderBackendConfig{Backend: "stub"}).Summarization().Name(); got != "stub" {
		t.Errorf("stub: %q", got)
	}
	if got := cfg(config.ProviderBackendConfig{Backend: "openai", Model: "m"}).Summarization().Name(); got != "openai" {
		t.Errorf("openai: %q", got)
	}
	bad := cfg(config.ProviderBackendConfig{PromptTemplate: "{{.Text"}).Summarization()
	if _, err := bad.Summarize(context.Background(), SummarizeRequest{Text: "x"}); !errors.Is(err, ErrInvalidPromptTemplate) {
		t.Errorf("bad template: err = %v", err)
	}
}

func TestFactoryFrameOptions(t *testing.T) {
	off := false
	tests := []struct {
		name string
		cfg  config.ProviderBackendConfig
		want FrameOptions
	}{
		{"defaults", config.ProviderBackendConfig{}, DefaultFrameOptions()},
		{"explicit bounds", config.ProviderBackendConfig{MinFPS: 0.5, MaxFPS: 2, SceneThreshold: 0.4}, FrameOptions{MinFPS: 0.5, MaxFPS: 2, SceneDetection: true, SceneThreshold: 0.4, MaxFrames: DefaultMaxFrames, MaxDimension: DefaultMaxDimension}},
		{"scene off", config.ProviderBackendConfig{MaxFPS: 0.5, SceneDetection: &off}, FrameOptions{MinFPS: 0.1, MaxFPS: 0.5, MaxFrames: DefaultMaxFrames, MaxDimension: DefaultMaxDimension}},
		{"one bound keeps the other's default", config.ProviderBackendConfig{MaxFPS: 2}, FrameOptions{MinFPS: 0.1, MaxFPS: 2, SceneDetection: true, MaxFrames: DefaultMaxFrames, MaxDimension: DefaultMaxDimension}},
		{"cap and size", config.ProviderBackendConfig{MaxFrames: 50, MaxDimension: 640}, FrameOptions{MinFPS: 0.1, MaxFPS: 1, SceneDetection: true, MaxFrames: 50, MaxDimension: 640}},
	}
	for _, tt := range tests {
		if got := NewFactory(config.ProvidersConfig{Frames: tt.cfg}, nil).FrameOptions(); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestFactoryFrames(t *testing.T) {
	if got := NewFactory(config.ProvidersConfig{Frames: config.ProviderBackendConfig{Backend: "stub"}}, nil).Frames().Name(); got != "stub" {
		t.Errorf("stub: %q", got)
	}
	// auto depends on the host's tools: see TestFrames_AutoResolvesByTool.
}

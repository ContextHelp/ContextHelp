package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"hop.top/kit/go/storage/secret/memory"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
)

// noKeys clears every hosted API key from the environment.
func noKeys(t *testing.T) {
	t.Helper()
	for _, k := range providerKeyNames {
		t.Setenv(k, "")
	}
}

func extractFramesErr(p FrameProvider) error {
	return p.ExtractFrames(context.Background(), FileSource("/x.mp4"), DefaultFrameOptions(), func(Frame) error { return nil })
}

// The three allowlists are one: roleBackends is config's, ollama is no
// transcription backend, and frames and summarization are listed.
func TestRoleBackendsIsConfigAllowlist(t *testing.T) {
	if reflect.ValueOf(roleBackends).UnsafePointer() != reflect.ValueOf(config.ProviderBackends).UnsafePointer() {
		t.Fatal("roleBackends is a copy of config.ProviderBackends, not the same map")
	}
	for _, b := range roleBackends["transcription"] {
		if b == backendOllama {
			t.Errorf("transcription lists %q", b)
		}
	}
	want := map[string][]string{
		"frames":        {"auto", "ffmpeg", "stub"},
		"summarization": {"auto", "ollama", "openai", "anthropic", "stub"},
	}
	for role, w := range want {
		if !reflect.DeepEqual(roleBackends[role], w) {
			t.Errorf("%s backends = %v, want %v", role, roleBackends[role], w)
		}
	}
}

// auto without ffmpeg leaves frames unavailable (a stub), not an error;
// with ffmpeg and ffprobe it is the ffmpeg backend.
func TestFrames_AutoResolvesByTool(t *testing.T) {
	dir := noTools(t)
	f := NewFactory(config.ProvidersConfig{}, nil)
	if _, ok := f.Frames().(*StubFrameProvider); !ok || IsFailed(f.Frames()) {
		t.Errorf("auto without ffmpeg = %T, want the stub and not failed", f.Frames())
	}
	if err := f.Validate(); err != nil {
		t.Errorf("auto must never be a config error: %v", err)
	}
	fakeTool(t, dir, "ffmpeg")
	fakeTool(t, dir, "ffprobe")
	if got := f.Frames().Name(); got != backendFFmpeg {
		t.Errorf("auto with ffmpeg = %q, want ffmpeg", got)
	}
}

// A named ffmpeg that is missing is a failed provider carrying the install
// hint, and Validate reports it under providers.frames.backend.
func TestFrames_NamedFFmpegMissingIsFailedWithHint(t *testing.T) {
	noTools(t)
	f := NewFactory(config.ProvidersConfig{Frames: backend(backendFFmpeg)}, nil)
	if !IsFailed(f.Frames()) {
		t.Fatalf("Frames() = %T, want a failed provider", f.Frames())
	}
	var toolErr *ToolNotInstalledError
	if err := extractFramesErr(f.Frames()); !errors.As(err, &toolErr) {
		t.Errorf("ExtractFrames err = %v, want *ToolNotInstalledError", err)
	}
	err := f.Validate()
	if err == nil || !strings.Contains(err.Error(), "providers.frames.backend") || !strings.Contains(err.Error(), "ffmpeg") {
		t.Errorf("Validate() = %v, want an error naming providers.frames.backend and ffmpeg", err)
	}
}

// failedFrames and failedSummarizer are the shared failed type, so
// IsFailed and Unavailable see them.
func TestFramesAndSummarizerFailuresShareFailedType(t *testing.T) {
	f := NewFactory(config.ProvidersConfig{
		Frames:        backend("magic"),
		Summarization: backend("gpt-sorcery"),
	}, nil)
	for name, p := range map[string]any{"frames": f.Frames(), "summarization": f.Summarization()} {
		if !IsFailed(p) {
			t.Errorf("%s: IsFailed = false for %T", name, p)
			continue
		}
		if err := p.(interface{ Unavailable() error }).Unavailable(); err == nil || !strings.Contains(err.Error(), "unknown backend") {
			t.Errorf("%s: Unavailable() = %v", name, err)
		}
	}
	bad := NewFactory(config.ProvidersConfig{Summarization: config.ProviderBackendConfig{PromptTemplate: "{{.Text"}}, nil)
	if !IsFailed(bad.Summarization()) {
		t.Error("a bad prompt template is not a failed summarizer")
	}
}

// One wording for an unknown backend, from config, for every role.
func TestValidate_UnknownBackendWordingIsShared(t *testing.T) {
	err := NewFactory(config.ProvidersConfig{
		Frames:        backend("magic"),
		Summarization: backend("gpt-sorcery"),
		Video:         backend("ffmpg"),
	}, nil).Validate()
	if err == nil {
		t.Fatal("Validate() = nil")
	}
	for _, want := range []string{
		`providers.frames.backend: unknown backend "magic" for frames; valid values: auto, ffmpeg, stub`,
		`providers.summarization.backend: unknown backend "gpt-sorcery" for summarization; valid values: auto, ollama, openai, anthropic, stub`,
		`providers.video.backend: unknown backend "ffmpg" for video; valid values: auto, ffmpeg, stub`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %q, lacks %q", err, want)
		}
	}
	// Each problem once: an unknown name is not also reported from the
	// role's getter.
	if n := strings.Count(err.Error(), `"magic"`); n != 1 {
		t.Errorf("frames name reported %d times: %v", n, err)
	}
}

// A summarization prompt-template error surfaces through Validate, even
// on auto.
func TestValidate_SummarizationTemplateError(t *testing.T) {
	for _, b := range []string{"", "auto", "ollama"} {
		c := config.ProviderBackendConfig{Backend: b, Model: "m", PromptTemplate: "{{.Text"}
		err := NewFactory(config.ProvidersConfig{Summarization: c}, nil).Validate()
		if err == nil || !strings.Contains(err.Error(), "providers.summarization.prompt_template") {
			t.Errorf("backend %q: Validate() = %v", b, err)
		}
	}
	if err := NewFactory(config.ProvidersConfig{Summarization: config.ProviderBackendConfig{PromptTemplate: "{{.Text}}"}}, nil).Validate(); err != nil {
		t.Errorf("good template: %v", err)
	}
}

// A named ollama backend with no model is a config error naming
// providers.<role>.model; auto with no model is not.
func TestValidate_NamedOllamaNeedsModel(t *testing.T) {
	noKeys(t)
	set := func(role string, c config.ProviderBackendConfig) config.ProvidersConfig {
		var p config.ProvidersConfig
		switch role {
		case "vision":
			p.Vision = c
		case "llm":
			p.LLM = c
		case "summarization":
			p.Summarization = c
		}
		return p
	}
	for _, role := range []string{"vision", "llm", "summarization"} {
		err := NewFactory(set(role, backend(backendOllama)), nil).Validate()
		if err == nil || !strings.Contains(err.Error(), "providers."+role+".model") {
			t.Errorf("%s: named ollama with no model: Validate() = %v", role, err)
		}
		if err := NewFactory(set(role, backendModel(backendOllama, "m:1b")), nil).Validate(); err != nil {
			t.Errorf("%s: named ollama with a model: %v", role, err)
		}
		if err := NewFactory(set(role, backend(backendAuto)), nil).Validate(); err != nil {
			t.Errorf("%s: auto with no model must not error: %v", role, err)
		}
		if err := NewFactory(set(role, config.ProviderBackendConfig{}), nil).Validate(); err != nil {
			t.Errorf("%s: unset must not error: %v", role, err)
		}
	}
}

// A named hosted backend whose key does not resolve is a config error;
// the key resolves from the secrets store.
func TestValidate_NamedHostedNeedsKey(t *testing.T) {
	noKeys(t)
	cases := []struct {
		role string
		cfg  config.ProvidersConfig
		key  string
	}{
		{"llm", config.ProvidersConfig{LLM: backend(backendOpenAI)}, keyOpenAI},
		{"llm", config.ProvidersConfig{LLM: backend(backendAnthropic)}, keyAnthropic},
		{"vision", config.ProvidersConfig{Vision: backend(backendGemini)}, keyGemini},
		{"vision", config.ProvidersConfig{Vision: backend(backendOpenRouter)}, keyOpenRouter},
		{"summarization", config.ProvidersConfig{Summarization: backend(backendAnthropic)}, keyAnthropic},
	}
	for _, tc := range cases {
		err := NewFactory(tc.cfg, memory.New()).Validate()
		if err == nil || !strings.Contains(err.Error(), "providers."+tc.role+".backend") || !strings.Contains(err.Error(), tc.key) {
			t.Errorf("%s/%s: Validate() = %v", tc.role, tc.key, err)
		}
		store := memory.New()
		if err := store.Set(context.Background(), tc.key, []byte("k")); err != nil {
			t.Fatal(err)
		}
		if err := NewFactory(tc.cfg, store).Validate(); err != nil {
			t.Errorf("%s/%s with the key in the store: %v", tc.role, tc.key, err)
		}
	}
	// auto never errors on a missing key.
	if err := NewFactory(config.ProvidersConfig{LLM: backend(backendAuto), Vision: backend(backendAuto), Summarization: backend(backendAuto)}, memory.New()).Validate(); err != nil {
		t.Errorf("auto: %v", err)
	}
}

// The resolution snapshot covers every role, frames and summarization
// included.
func TestResolveCoversFramesAndSummarization(t *testing.T) {
	noTools(t)
	r := NewFactory(config.ProvidersConfig{Frames: backend(backendStub), Summarization: backend(backendStub)}, nil).Resolve()
	if r.Frames == nil || r.Summarization == nil {
		t.Fatalf("Resolve() left frames or summarization nil: %+v", r)
	}
	if r.Frames.Name() != backendStub || r.Summarization.Name() != backendStub {
		t.Errorf("snapshot = %s / %s, want the configured stubs", r.Frames.Name(), r.Summarization.Name())
	}
}

func TestInstallHintFramesIsFFmpeg(t *testing.T) {
	if got := InstallHint("frames"); !strings.Contains(got, "ffmpeg") {
		t.Errorf("InstallHint(frames) = %q, want the ffmpeg hint", got)
	}
	if InstallHint("frames") != InstallHint("video") {
		t.Errorf("frames and video share ffmpeg: %q vs %q", InstallHint("frames"), InstallHint("video"))
	}
}

// One vision probe per vision config: a derived factory that changes
// another role shares the answer, one that changes the vision config
// probes again.
func TestVisionProbeIsSharedAcrossUnrelatedConfigChanges(t *testing.T) {
	o := newFakeOllama(t, "m:1b")
	cfg := config.ProvidersConfig{Vision: config.ProviderBackendConfig{Endpoint: o.srv.URL, Model: "m:1b"}}
	f := NewFactory(cfg, nil)
	f.Resolve()
	f.Resolve()
	if got := o.tagsRequests(); got != 1 {
		t.Fatalf("one factory probed %d times, want 1", got)
	}

	other := cfg
	other.OCR = backend(backendStub)
	g := f.WithConfig(other)
	g.Resolve()
	if got := o.tagsRequests(); got != 1 {
		t.Errorf("a derived factory with the same vision config probed again: %d requests", got)
	}

	changed := cfg
	changed.Vision.Model = "m:1b"
	changed.Vision.Endpoint = o.srv.URL + "/"
	f.WithConfig(changed).Resolve()
	if got := o.tagsRequests(); got != 2 {
		t.Errorf("a changed vision config must probe again: %d requests", got)
	}
}

// The probe sends its request through the factory's injected client and
// runs under the factory's context.
func TestVisionProbeUsesFactoryClientAndContext(t *testing.T) {
	o := newFakeOllama(t, "m:1b")
	cfg := config.ProvidersConfig{Vision: config.ProviderBackendConfig{Endpoint: o.srv.URL, Model: "m:1b"}}

	var sawTags bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sawTags = sawTags || strings.HasSuffix(r.URL.Path, "/api/tags")
		return http.DefaultTransport.RoundTrip(r)
	})}
	if got := NewFactory(cfg, nil).WithHTTPClient(client).Vision().Name(); got != "ollama" || !sawTags {
		t.Errorf("injected client: provider %q, saw /api/tags = %v", got, sawTags)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := o.tagsRequests()
	if _, stub := NewFactory(cfg, nil).WithContext(ctx).Vision().(*vision.StubProvider); !stub {
		t.Error("a cancelled factory context must fail the probe")
	}
	if got := o.tagsRequests(); got != before {
		t.Errorf("the probe ran past its context: %d requests, want %d", got, before)
	}
}

// A response larger than the limit is not read whole: the probe fails.
func TestVisionProbeBoundsTheResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"m:1b"}],"pad":"`))
		chunk := strings.Repeat("x", 1<<20)
		for range 5 {
			_, _ = w.Write([]byte(chunk))
		}
		_, _ = w.Write([]byte(`"}`))
	}))
	t.Cleanup(srv.Close)
	cfg := config.ProvidersConfig{Vision: config.ProviderBackendConfig{Endpoint: srv.URL, Model: "m:1b"}}
	if _, stub := NewFactory(cfg, nil).Vision().(*vision.StubProvider); !stub {
		t.Error("an oversized /api/tags answer must not select vision")
	}
}

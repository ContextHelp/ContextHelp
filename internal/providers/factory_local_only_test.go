package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/storage/secret"
	"hop.top/kit/go/storage/secret/memory"
)

// Backends that send data off the machine. No role may resolve to one
// unless config names it.
var cloudBackendNames = map[string]bool{
	"openai": true, "anthropic": true, "gemini": true, "openrouter": true,
}

// closedPort is an endpoint nothing listens on, so a local probe fails
// without touching a real service.
const closedPort = "http://127.0.0.1:1"

// allKeysFactory returns a factory over a config with no named backend
// ("auto" everywhere), with every hosted API key set in both the secrets
// store and the environment, and a fake hosted API behind the HTTP client.
func allKeysFactory(t *testing.T, a *fakeAPI, cfg config.ProvidersConfig) *Factory {
	t.Helper()
	store := memory.New()
	for _, k := range providerKeyNames {
		t.Setenv(k, envKey)
		require.NoError(t, store.Set(context.Background(), k, []byte(storeKey)))
	}
	return NewFactory(cfg, store).WithHTTPClient(a.client())
}

// Privacy gate, one case per role: with every cloud key set and no named
// backend, a role resolves to a local backend or to unavailable (the
// stub), and no request reaches a hosted API.
func TestAutoNeverResolvesToCloud_EveryRole(t *testing.T) {
	a := newFakeAPI(t)
	cfg := config.ProvidersConfig{}
	cfg.LLM.Endpoint = closedPort
	cfg.Vision.Endpoint = closedPort
	f := allKeysFactory(t, a, cfg)

	roles := map[string]func() string{
		"video":         func() string { return f.Video().Name() },
		"document":      func() string { return f.Document().Name() },
		"ocr":           func() string { return f.OCR().Name() },
		"transcription": func() string { return f.Transcription().Name() },
		"diarization":   func() string { return f.Diarization().Name() },
		"vision":        func() string { return f.Vision().Name() },
		"llm":           func() string { return f.LLM().Name() },
	}
	for role, name := range roles {
		t.Run(role, func(t *testing.T) {
			got := name()
			assert.False(t, cloudBackendNames[got], "%s auto resolved to cloud backend %q", role, got)
		})
	}
	assert.Empty(t, a.received(), "no request may reach a hosted API")
}

func TestLLMAutoIsLocalOllamaDespiteKeys(t *testing.T) {
	a := newFakeAPI(t)
	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "auto"
	cfg.LLM.Endpoint = closedPort
	cfg.LLM.Model = "local-model"
	f := allKeysFactory(t, a, cfg)

	assert.Equal(t, "ollama", f.LLM().Name())
	_, err := f.LLM().Generate(context.Background(), "hi")
	require.Error(t, err, "closed local port: the call fails locally")
	assert.Empty(t, a.received())
}

func TestLLMEmptyBackendIsLocalOllamaDespiteKeys(t *testing.T) {
	f := allKeysFactory(t, newFakeAPI(t), config.ProvidersConfig{})
	assert.Equal(t, "ollama", f.LLM().Name())
}

// A fake Ollama that records each request and lists the installed models
// on /api/tags.
type fakeOllama struct {
	srv       *httptest.Server
	mu        sync.Mutex
	models    []string
	tagsCalls int
}

func newFakeOllama(t *testing.T, installed ...string) *fakeOllama {
	t.Helper()
	o := &fakeOllama{}
	type tag struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	}
	tags := make([]tag, 0, len(installed))
	for _, m := range installed {
		tags = append(tags, tag{m, m})
	}
	o.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			o.mu.Lock()
			o.tagsCalls++
			o.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"models": tags})
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		o.mu.Lock()
		o.models = append(o.models, req.Model)
		o.mu.Unlock()
		_, _ = w.Write([]byte(`{"response":"a cat. Labels: cat","done":true}`))
	}))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *fakeOllama) tagsRequests() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.tagsCalls
}

func (o *fakeOllama) requestedModels() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.models...)
}

func TestVisionAutoUsesLocalOllamaWithConfiguredModel(t *testing.T) {
	a := newFakeAPI(t)
	o := newFakeOllama(t, "qwen2.5vl:7b")
	cfg := config.ProvidersConfig{}
	cfg.Vision.Endpoint = o.srv.URL
	cfg.Vision.Model = "qwen2.5vl:7b"
	f := allKeysFactory(t, a, cfg)

	p := f.Vision()
	require.Equal(t, "ollama", p.Name())
	_, err := p.Analyze(context.Background(), []byte{0x89, 'P', 'N', 'G'}, "image/png")
	require.NoError(t, err)
	assert.Equal(t, []string{"qwen2.5vl:7b"}, o.requestedModels())
	assert.Empty(t, a.received())
}

// Local Ollama is down: auto is unavailable (stub), never a cloud
// backend, whatever keys are set.
func TestVisionAutoUnavailableWhenOllamaDownDespiteKeys(t *testing.T) {
	a := newFakeAPI(t)
	cfg := config.ProvidersConfig{}
	cfg.Vision.Endpoint = closedPort
	cfg.Vision.Model = "qwen2.5vl:7b"
	f := allKeysFactory(t, a, cfg)

	_, isStub := f.Vision().(*vision.StubProvider)
	assert.True(t, isStub, "got %q", f.Vision().Name())
	assert.Empty(t, a.received())
}

// Ollama answers but the configured vision model is not installed: the
// role is unavailable (a registered image pipeline would fail every job
// with "model not found"). ":latest" matches the bare name either way.
func TestVisionAutoRequiresInstalledModel(t *testing.T) {
	cases := []struct {
		name      string
		installed []string
		model     string
		want      string
	}{
		{"absent", []string{"nomic-embed-text:latest", "llama3:latest"}, "qwen2.5vl:7b", "stub"},
		{"empty list", nil, "qwen2.5vl:7b", "stub"},
		{"different tag", []string{"qwen2.5vl:3b"}, "qwen2.5vl:7b", "stub"},
		{"exact", []string{"qwen2.5vl:7b"}, "qwen2.5vl:7b", "ollama"},
		{"configured bare, installed latest", []string{"llava:latest"}, "llava", "ollama"},
		{"configured latest, installed bare", []string{"llava"}, "llava:latest", "ollama"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := newFakeOllama(t, tc.installed...)
			cfg := config.ProvidersConfig{}
			cfg.Vision.Endpoint = o.srv.URL
			cfg.Vision.Model = tc.model
			f := allKeysFactory(t, newFakeAPI(t), cfg)
			assert.Equal(t, tc.want, f.Vision().Name())
		})
	}
}

// The probe runs once per factory, so every caller (capability check,
// step construction) sees the same answer.
func TestVisionAutoProbesOncePerFactory(t *testing.T) {
	o := newFakeOllama(t, "qwen2.5vl:7b")
	cfg := config.ProvidersConfig{}
	cfg.Vision.Endpoint = o.srv.URL
	cfg.Vision.Model = "qwen2.5vl:7b"
	f := NewFactory(cfg, memory.New())
	for i := 0; i < 3; i++ {
		require.Equal(t, "ollama", f.Vision().Name())
	}
	assert.Equal(t, 1, o.tagsRequests())

	// A derived factory has its own config, so it probes for itself.
	cfg2 := cfg
	cfg2.Vision.Model = "other"
	assert.Equal(t, "stub", f.WithConfig(cfg2).Vision().Name())
	assert.Equal(t, 2, o.tagsRequests())
}

// A trailing slash on the endpoint does not double up in the request path.
func TestVisionOllamaEndpointTrailingSlash(t *testing.T) {
	o := newFakeOllama(t)
	cfg := config.ProvidersConfig{}
	cfg.Vision.Backend = "ollama"
	cfg.Vision.Endpoint = o.srv.URL + "/"
	cfg.Vision.Model = "m"
	_, err := NewFactory(cfg, memory.New()).Vision().Analyze(
		context.Background(), []byte{0x89, 'P', 'N', 'G'}, "image/png")
	require.NoError(t, err)
	assert.Equal(t, []string{"m"}, o.requestedModels())
}

// recordingStore records every secrets lookup.
type recordingStore struct {
	secret.Store
	mu   sync.Mutex
	gets []string
}

func (r *recordingStore) Get(ctx context.Context, name string) (*secret.Secret, error) {
	r.mu.Lock()
	r.gets = append(r.gets, name)
	r.mu.Unlock()
	return r.Store.Get(ctx, name)
}

// auto never consults the secrets store: no key lookup decides what runs.
func TestAutoNeverReadsSecrets(t *testing.T) {
	o := newFakeOllama(t, "qwen2.5vl:7b")
	rec := &recordingStore{Store: memory.New()}
	cfg := config.ProvidersConfig{}
	cfg.LLM.Endpoint = closedPort
	cfg.Vision.Endpoint = o.srv.URL
	cfg.Vision.Model = "qwen2.5vl:7b"
	f := NewFactory(cfg, rec)
	_ = f.Video()
	_ = f.Document()
	_ = f.OCR()
	_ = f.Transcription()
	_ = f.Diarization()
	_ = f.Vision()
	_ = f.LLM()
	assert.Empty(t, rec.gets)
}

// No model configured: there is nothing local to run, so auto is
// unavailable rather than guessing a model.
func TestVisionAutoUnavailableWithoutModel(t *testing.T) {
	o := newFakeOllama(t)
	cfg := config.ProvidersConfig{}
	cfg.Vision.Endpoint = o.srv.URL
	f := allKeysFactory(t, newFakeAPI(t), cfg)

	_, isStub := f.Vision().(*vision.StubProvider)
	assert.True(t, isStub, "got %q", f.Vision().Name())
}

// A named ollama backend with no model is a configuration error at call
// time, with no request sent and no silent default model.
func TestOllamaWithoutModelIsConfigurationError(t *testing.T) {
	o := newFakeOllama(t)

	t.Run("llm", func(t *testing.T) {
		cfg := config.ProvidersConfig{}
		cfg.LLM.Backend = "ollama"
		cfg.LLM.Endpoint = o.srv.URL
		_, err := NewFactory(cfg, memory.New()).LLM().Generate(context.Background(), "hi")
		require.ErrorIs(t, err, ErrModelNotConfigured)
		assert.Contains(t, err.Error(), "providers.llm.model")
	})
	t.Run("llm auto", func(t *testing.T) {
		cfg := config.ProvidersConfig{}
		cfg.LLM.Endpoint = o.srv.URL
		_, err := NewFactory(cfg, memory.New()).LLM().Generate(context.Background(), "hi")
		require.ErrorIs(t, err, ErrModelNotConfigured)
	})
	t.Run("vision", func(t *testing.T) {
		cfg := config.ProvidersConfig{}
		cfg.Vision.Backend = "ollama"
		cfg.Vision.Endpoint = o.srv.URL
		_, err := NewFactory(cfg, memory.New()).Vision().Analyze(
			context.Background(), []byte{0x89, 'P', 'N', 'G'}, "image/png")
		require.ErrorIs(t, err, ErrModelNotConfigured)
		assert.Contains(t, err.Error(), "providers.vision.model")
	})
	assert.Empty(t, o.requestedModels(), "no request without a model")
}

// The built-in local default model is not a hosted model name: a named
// cloud backend left on the default model uses its own default.
func TestHostedBackendIgnoresBuiltInLocalModel(t *testing.T) {
	var (
		mu     sync.Mutex
		models []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		models = append(models, req.Model)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok Labels: a"}}]}`))
	}))
	t.Cleanup(srv.Close)
	a := &fakeAPI{srv: srv}

	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "openai"
	cfg.LLM.Model = config.DefaultLLMModel
	cfg.Vision.Backend = "openai"
	cfg.Vision.Model = config.DefaultVisionModel
	f := keyFactory(t, a, "llm", "openai", map[string]string{keyOpenAI: storeKey}, nil).WithConfig(cfg)

	_, err := f.LLM().Generate(context.Background(), "hi")
	require.NoError(t, err)
	_, err = f.Vision().Analyze(context.Background(), []byte{0x89, 'P', 'N', 'G'}, "image/png")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"gpt-4o-mini", "gpt-4o"}, models)
}

// A model the user set explicitly reaches a named cloud backend.
func TestHostedBackendKeepsExplicitModel(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		got = req.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)
	a := &fakeAPI{srv: srv}

	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "openai"
	cfg.LLM.Model = "gpt-4.1"
	f := keyFactory(t, a, "llm", "openai", map[string]string{keyOpenAI: storeKey}, nil).WithConfig(cfg)
	_, err := f.LLM().Generate(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "gpt-4.1", got)
}

func TestSameOllamaModelTagAfterLastSlash(t *testing.T) {
	assert.True(t, sameOllamaModel("localhost:5000/llava", "localhost:5000/llava:latest"))
	assert.True(t, sameOllamaModel("registry.example:5000/team/llava:latest", "registry.example:5000/team/llava"))
	assert.False(t, sameOllamaModel("localhost:5000/llava", "localhost:5000/llava:7b"))
	assert.False(t, sameOllamaModel("localhost:5000/llava", "localhost"))
	assert.True(t, sameOllamaModel("llava", "llava:latest"))
}

// An /api/tags body past the bound is not read to the end: the role is
// unavailable instead of buffering an unbounded response.
func TestVisionAutoBoundsTagsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"` + strings.Repeat("x", 5<<20) + `"},{"name":"qwen2.5vl:7b"}]}`))
	}))
	t.Cleanup(srv.Close)
	cfg := config.ProvidersConfig{}
	cfg.Vision.Endpoint = srv.URL
	cfg.Vision.Model = "qwen2.5vl:7b"
	assert.Equal(t, "stub", NewFactory(cfg, memory.New()).Vision().Name())
}

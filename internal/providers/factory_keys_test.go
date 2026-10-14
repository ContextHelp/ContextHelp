package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/storage/secret/memory"
)

// Hosted provider key names, as the factory looks them up.
var providerKeyNames = []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"}

const (
	storeKey = "test-key-from-store"
	envKey   = "test-key-from-env"
)

// fakeAPI answers every hosted provider's request shape and records the
// API key each request carried (Authorization bearer, x-api-key, or the
// Gemini key query parameter).
type fakeAPI struct {
	srv  *httptest.Server
	mu   sync.Mutex
	keys []string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	a := &fakeAPI{}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" {
			key = r.Header.Get("x-api-key")
		}
		if key == "" {
			key = r.URL.Query().Get("key")
		}
		a.mu.Lock()
		a.keys = append(a.keys, key)
		a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"content":"ok Labels: a"}}],
			"content":[{"text":"ok"}],
			"candidates":[{"content":{"parts":[{"text":"ok Labels: a"}]}}]
		}`))
	}))
	t.Cleanup(a.srv.Close)
	return a
}

// client returns an HTTP client that sends every request, whatever its
// host, to the fake server, so no request can leave the machine.
func (a *fakeAPI) client() *http.Client {
	target, _ := url.Parse(a.srv.URL)
	base := a.srv.Client().Transport
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host, r.Host = target.Scheme, target.Host, ""
		return base.RoundTrip(r)
	})}
}

func (a *fakeAPI) received() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.keys...)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// hostedTarget is one hosted client the factory can build.
type hostedTarget struct {
	kind    string // "llm" or "vision"
	backend string
	keyName string
	auto    bool // auto mode picks this backend when only its key is set
}

var hostedTargets = []hostedTarget{
	{"llm", "anthropic", "ANTHROPIC_API_KEY", true},
	{"llm", "openai", "OPENAI_API_KEY", true},
	{"vision", "openai", "OPENAI_API_KEY", true},
	{"vision", "anthropic", "ANTHROPIC_API_KEY", true},
	{"vision", "gemini", "GEMINI_API_KEY", true},
	{"vision", "openrouter", "OPENROUTER_API_KEY", true},
}

// keyFactory builds a factory over a memory secrets store holding
// storeKeys, with the process env holding envKeys only. Every endpoint
// that could reach a local service points at a closed port.
func keyFactory(t *testing.T, a *fakeAPI, kind, backend string, storeKeys, envKeys map[string]string) *Factory {
	t.Helper()
	for _, k := range providerKeyNames {
		t.Setenv(k, envKeys[k])
	}
	store := memory.New()
	for k, v := range storeKeys {
		require.NoError(t, store.Set(context.Background(), k, []byte(v)))
	}
	var cfg config.ProvidersConfig
	cfg.LLM.Endpoint = "http://127.0.0.1:1"
	cfg.Vision.Endpoint = "http://127.0.0.1:1"
	switch kind {
	case "llm":
		cfg.LLM.Backend = backend
	case "vision":
		cfg.Vision.Backend = backend
	}
	return NewFactory(cfg, store).WithHTTPClient(a.client())
}

// call builds the target's provider and sends one request through it.
func call(t *testing.T, f *Factory, kind string) (string, error) {
	t.Helper()
	switch kind {
	case "llm":
		p := f.LLM()
		_, err := p.Generate(context.Background(), "hi")
		return p.Name(), err
	default:
		p := f.Vision()
		_, err := p.Analyze(context.Background(), []byte{0x89, 'P', 'N', 'G'}, "image/png")
		return p.Name(), err
	}
}

func TestFactoryHostedClientsCarryResolvedKey(t *testing.T) {
	scenarios := []struct {
		name      string
		storeKeys func(k string) map[string]string
		envKeys   func(k string) map[string]string
		want      string
	}{
		{"store only", func(k string) map[string]string { return map[string]string{k: storeKey} }, func(string) map[string]string { return nil }, storeKey},
		{"env only", func(string) map[string]string { return nil }, func(k string) map[string]string { return map[string]string{k: envKey} }, envKey},
		{"store wins over env", func(k string) map[string]string { return map[string]string{k: storeKey} }, func(k string) map[string]string { return map[string]string{k: envKey} }, storeKey},
	}
	for _, tg := range hostedTargets {
		for _, sc := range scenarios {
			for _, mode := range []string{tg.backend, "auto"} {
				if mode == "auto" && !tg.auto {
					continue
				}
				t.Run(tg.kind+"/"+tg.backend+"/"+mode+"/"+sc.name, func(t *testing.T) {
					a := newFakeAPI(t)
					f := keyFactory(t, a, tg.kind, mode, sc.storeKeys(tg.keyName), sc.envKeys(tg.keyName))
					name, err := call(t, f, tg.kind)
					require.NoError(t, err)
					assert.Equal(t, tg.backend, name)
					assert.Equal(t, []string{sc.want}, a.received())
				})
			}
		}
	}
}

func TestFactoryHostedClientsWithoutKey(t *testing.T) {
	for _, tg := range hostedTargets {
		t.Run(tg.kind+"/"+tg.backend+"/explicit", func(t *testing.T) {
			a := newFakeAPI(t)
			f := keyFactory(t, a, tg.kind, tg.backend, nil, nil)
			_, err := call(t, f, tg.kind)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tg.keyName)
			assert.Empty(t, a.received(), "no request without a key")
		})
	}
	t.Run("llm/auto falls back to ollama", func(t *testing.T) {
		f := keyFactory(t, newFakeAPI(t), "llm", "auto", nil, nil)
		assert.Equal(t, "ollama", f.LLM().Name())
	})
	t.Run("vision/auto falls back to stub", func(t *testing.T) {
		f := keyFactory(t, newFakeAPI(t), "vision", "auto", nil, nil)
		_, isStub := f.Vision().(*vision.StubProvider)
		assert.True(t, isStub)
	})
}

// A factory derived for a per-pipeline override resolves keys through the
// same secrets store.
func TestFactoryWithConfigKeepsSecretsStore(t *testing.T) {
	a := newFakeAPI(t)
	base := keyFactory(t, a, "llm", "stub", map[string]string{"OPENAI_API_KEY": storeKey}, nil)
	var cfg config.ProvidersConfig
	cfg.LLM.Backend = "openai"
	derived := base.WithConfig(cfg)
	name, err := call(t, derived, "llm")
	require.NoError(t, err)
	assert.Equal(t, "openai", name)
	assert.Equal(t, []string{storeKey}, a.received())
}

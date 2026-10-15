package builtins_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/storage/secret/memory"
)

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host, req.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(req)
}

// A per-pipeline provider override resolves its API key through the base
// factory's secrets store, not the environment alone.
func TestPipelineProviderOverrideUsesSecretsStore(t *testing.T) {
	for _, k := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"} {
		t.Setenv(k, "")
	}
	const storeKey = "test-key-from-store"

	var (
		mu   sync.Mutex
		keys []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"alpha, beta"}}]}`))
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	client := &http.Client{Transport: rewriteTransport{target, srv.Client().Transport}}

	store := memory.New()
	require.NoError(t, store.Set(context.Background(), "OPENAI_API_KEY", []byte(storeKey)))
	baseCfg := config.ProvidersConfig{LLM: config.ProviderBackendConfig{Backend: "stub"}}
	base := providers.NewFactory(baseCfg, store).WithHTTPClient(client)

	reg, err := builtins.ConfiguredRegistryWithPipelineOverrides(builtins.BuildOpts{Factory: base}, baseCfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"text.short": {Providers: map[string]config.ProviderBackendConfig{"llm": {Backend: "openai"}}},
		},
	})
	require.NoError(t, err)
	pipe, err := reg.Get("text.short")
	require.NoError(t, err)

	ran := false
	for _, s := range pipe.Steps {
		if s.Name() != "tagger" {
			continue
		}
		_, err := s.Run(context.Background(), &storage.KnowledgeObject{
			ID:         "override-key",
			RawContent: strings.Repeat("secrets store keys reach the override pipeline provider. ", 4),
		})
		require.NoError(t, err)
		ran = true
	}
	require.True(t, ran, "text.short has a tagger step")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{storeKey}, keys)
}

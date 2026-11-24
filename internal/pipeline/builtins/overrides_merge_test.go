package builtins_test

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
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/storage/secret/memory"
)

// A per-pipeline provider override changes only the fields it sets: an
// endpoint-only llm override keeps the base backend and model, so the
// tagger's request reaches the override endpoint for the base model.
func TestPipelineProviderOverrideInheritsUnsetFields(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"response":"alpha, beta","done":true}`))
	}))
	t.Cleanup(srv.Close)

	var baseCfg config.ProvidersConfig
	baseCfg.LLM = config.ProviderBackendConfig{Backend: "ollama", Model: "base-model", Endpoint: "http://127.0.0.1:1"}
	base := providers.NewFactory(baseCfg, memory.New())

	reg, err := builtins.ConfiguredRegistryWithPipelineOverrides(builtins.BuildOpts{Factory: base}, baseCfg, config.PipelinesConfig{
		Overrides: map[string]config.PipelineOverride{
			"text.short": {Providers: map[string]config.ProviderBackendConfig{"llm": {Endpoint: srv.URL}}},
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
			ID:         "override-merge",
			RawContent: strings.Repeat("an override sets only the fields it names. ", 5),
		})
		require.NoError(t, err)
		ran = true
	}
	require.True(t, ran, "text.short has a tagger step")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"base-model"}, models)
}

// A miscased top-level backend name stops the registry build with the
// key and the valid values, rather than resolving to a stub.
func TestConfiguredRegistryRejectsUnknownBackendName(t *testing.T) {
	var cfg config.ProvidersConfig
	cfg.LLM.Backend = "Anthropic"
	_, err := builtins.ConfiguredRegistryWithPipelineOverrides(
		builtins.BuildOpts{Factory: providers.NewFactory(cfg, memory.New())}, cfg, config.PipelinesConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "providers.llm.backend")
	assert.Contains(t, err.Error(), "anthropic")
}

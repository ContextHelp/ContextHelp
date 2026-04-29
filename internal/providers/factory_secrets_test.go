package providers_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/storage/secret/memory"
)

func newMockStore(kv map[string]string) *memory.Store {
	s := memory.New()
	for k, v := range kv {
		_ = s.Set(context.Background(), k, []byte(v))
	}
	return s
}

func TestNewFactoryWithResolver(t *testing.T) {
	store := newMockStore(map[string]string{"ANTHROPIC_API_KEY": "sk-test-value"})
	cfg := config.ProvidersConfig{}
	f := providers.NewFactory(cfg, store)
	require.NotNil(t, f)
}

func TestNewFactoryNilResolverDefaultsToEnv(t *testing.T) {
	cfg := config.ProvidersConfig{}
	f := providers.NewFactory(cfg, nil)
	require.NotNil(t, f)
}

func TestFactoryLLMAutoUsesResolverKey(t *testing.T) {
	store := newMockStore(map[string]string{"ANTHROPIC_API_KEY": "sk-test-value"})
	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "auto"
	f := providers.NewFactory(cfg, store)
	llm := f.LLM()
	assert.NotNil(t, llm)
}

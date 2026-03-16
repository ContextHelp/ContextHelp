package providers_test

import (
	"fmt"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockResolver struct {
	keys map[string]string
}

func (m *mockResolver) Get(key string) (string, error) {
	if v, ok := m.keys[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found: %s", key)
}

func (m *mockResolver) Set(key, value string) error { return nil }

func TestNewFactoryWithResolver(t *testing.T) {
	resolver := &mockResolver{keys: map[string]string{
		"ANTHROPIC_API_KEY": "sk-test-value",
	}}
	cfg := config.ProvidersConfig{}
	f := providers.NewFactory(cfg, resolver)
	require.NotNil(t, f)
}

func TestNewFactoryNilResolverDefaultsToEnv(t *testing.T) {
	cfg := config.ProvidersConfig{}
	f := providers.NewFactory(cfg, nil)
	require.NotNil(t, f)
}

func TestFactoryLLMAutoUsesResolverKey(t *testing.T) {
	resolver := &mockResolver{keys: map[string]string{
		"ANTHROPIC_API_KEY": "sk-test-value",
	}}
	cfg := config.ProvidersConfig{}
	cfg.LLM.Backend = "auto"
	f := providers.NewFactory(cfg, resolver)
	llm := f.LLM()
	assert.NotNil(t, llm)
}

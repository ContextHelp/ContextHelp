package embeddings_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
)

var _ embeddings.ProviderResolver = embeddings.NewProviderResolver(&embeddings.Resolver{})

func snowflakeModel() registry.Model {
	return registry.Model{
		ModelID:    remoteModelID,
		Provider:   "ollama",
		Dimension:  1024,
		ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://m3:11434"}`,
	}
}

// ForModel embeds with the model's own backend and model, through a
// transport override (a tunnel port given as a flag). Replays the
// remote-ollama cassette recorded against a real Ollama.
func TestProviderResolver_ForModelUsesConfigJSONWithTransportOverride(t *testing.T) {
	client, calls := providertest.OllamaClient(t, "testdata/cassettes/remote-ollama")
	pr := embeddings.NewProviderResolver(&embeddings.Resolver{
		Config:     config.EmbeddingProviderConfig{Backend: "ollama", Model: "nomic-embed-text"},
		Flags:      embeddings.Overrides{Endpoint: "http://127.0.0.1:11555"},
		LookupEnv:  env(nil),
		HTTPClient: client,
	})

	p, err := pr.ForModel(context.Background(), snowflakeModel())
	if err != nil {
		t.Fatalf("ForModel: %v", err)
	}
	if p.Dimensions() != 1024 {
		t.Errorf("Dimensions() = %d, want the registered 1024", p.Dimensions())
	}
	vec, err := p.Embed(context.Background(), "remote embedding through a tunnel")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 1024 {
		t.Fatalf("dimension = %d, want 1024", len(vec))
	}
	if urls := calls.URLs(); len(urls) != 1 || urls[0] != "http://127.0.0.1:11555/api/embeddings" {
		t.Fatalf("provider called %v, want the tunnel endpoint", urls)
	}
}

func TestProviderResolver_ForModelRefusesIdentityOverride(t *testing.T) {
	for name, r := range map[string]*embeddings.Resolver{
		"flag model":  {Flags: embeddings.Overrides{Model: "nomic-embed-text"}, LookupEnv: env(nil)},
		"env backend": {LookupEnv: env(map[string]string{embeddings.EnvProvider: "stub"})},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := embeddings.NewProviderResolver(r).ForModel(context.Background(), snowflakeModel()); err == nil {
				t.Fatal("ForModel accepted an override of the registered identity")
			}
		})
	}
}

// ForRegistration resolves with full per-field overrides and returns the
// config_json to persist: backend, model, endpoint and the api_key_env
// NAME, never a key value and never the dimension (measured separately).
func TestProviderResolver_ForRegistration(t *testing.T) {
	pr := embeddings.NewProviderResolver(&embeddings.Resolver{
		Config: config.EmbeddingProviderConfig{Backend: "ollama", Model: "cfg-model", Dimension: 768},
		Flags:  embeddings.Overrides{Model: "snowflake-arctic-embed2"},
		LookupEnv: env(map[string]string{
			embeddings.EnvEndpoint:  "http://127.0.0.1:11555",
			embeddings.EnvAPIKeyEnv: "REG_KEY",
			"REG_KEY":               "sk-never-stored",
		}),
	})
	p, raw, err := pr.ForRegistration(context.Background())
	if err != nil {
		t.Fatalf("ForRegistration: %v", err)
	}
	if p == nil || p.Name() != "ollama-embed" {
		t.Fatalf("provider = %v", p)
	}
	if strings.Contains(string(raw), "sk-never-stored") {
		t.Fatalf("config_json holds the key value: %s", raw)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("config_json %s: %v", raw, err)
	}
	want := map[string]any{
		"backend":     "ollama",
		"model":       "snowflake-arctic-embed2",
		"endpoint":    "http://127.0.0.1:11555",
		"api_key_env": "REG_KEY",
	}
	if len(got) != len(want) {
		t.Fatalf("config_json = %s, want exactly %v", raw, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("config_json[%s] = %v, want %v", k, got[k], v)
		}
	}
}

// What ForRegistration stores, ForModel reads back as the same identity.
func TestProviderResolver_RegistrationRoundTrips(t *testing.T) {
	pr := embeddings.NewProviderResolver(&embeddings.Resolver{
		Flags:     embeddings.Overrides{Model: "snowflake-arctic-embed2", Endpoint: "http://m3:11434"},
		LookupEnv: env(nil),
	})
	_, raw, err := pr.ForRegistration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	later := embeddings.NewProviderResolver(&embeddings.Resolver{LookupEnv: env(nil)})
	p, err := later.ForModel(context.Background(), registry.Model{
		ModelID: remoteModelID, Provider: "ollama", Dimension: 1024, ConfigJSON: string(raw),
	})
	if err != nil {
		t.Fatalf("ForModel on the stored config_json: %v", err)
	}
	if p.Name() != "ollama-embed" {
		t.Fatalf("provider = %s", p.Name())
	}
}

func TestProviderResolver_ForRegistrationUnsupportedBackend(t *testing.T) {
	pr := embeddings.NewProviderResolver(&embeddings.Resolver{Flags: embeddings.Overrides{Backend: "openai"}, LookupEnv: env(nil)})
	if _, _, err := pr.ForRegistration(context.Background()); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err = %v", err)
	}
}

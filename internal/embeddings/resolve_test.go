package embeddings_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	kitconfig "hop.top/kit/go/core/config"
)

// env returns a LookupEnv backed by m, so tests never read the process env.
func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// cOverrides parses -c key=value tokens exactly the way kit's -c global does.
func cOverrides(t *testing.T, pairs ...string) map[string]any {
	t.Helper()
	_, o, err := kitconfig.ParseConfigArgs(pairs)
	if err != nil {
		t.Fatalf("ParseConfigArgs(%v): %v", pairs, err)
	}
	return o
}

func resolve(t *testing.T, r *embeddings.Resolver, req embeddings.Request) embeddings.Resolved {
	t.Helper()
	got, err := r.Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return got
}

func assertField(t *testing.T, got embeddings.Resolved, f embeddings.Field, value string, layer embeddings.Layer) {
	t.Helper()
	if v := got.Value(f); v != value {
		t.Errorf("%s = %q, want %q", f, v, value)
	}
	if l := got.Source(f); l != layer {
		t.Errorf("%s source = %q, want %q", f, l, layer)
	}
}

func TestResolve_Defaults(t *testing.T) {
	got := resolve(t, &embeddings.Resolver{LookupEnv: env(nil)}, embeddings.Request{})
	assertField(t, got, embeddings.FieldBackend, embeddings.DefaultBackend, embeddings.LayerDefault)
	assertField(t, got, embeddings.FieldModel, embeddings.DefaultModel, embeddings.LayerDefault)
	assertField(t, got, embeddings.FieldEndpoint, embeddings.DefaultEndpoint, embeddings.LayerDefault)
	assertField(t, got, embeddings.FieldAPIKeyEnv, "", embeddings.LayerDefault)
	assertField(t, got, embeddings.FieldDimension, "0", embeddings.LayerDefault)
}

func TestResolve_ConfigFileBeatsDefaults(t *testing.T) {
	r := &embeddings.Resolver{
		Config: config.EmbeddingProviderConfig{
			Backend: "ollama", Model: "cfg-model", Endpoint: "http://cfg:11434",
			APIKeyEnv: "CFG_KEY", Dimension: 768,
		},
		LookupEnv: env(nil),
	}
	got := resolve(t, r, embeddings.Request{})
	assertField(t, got, embeddings.FieldModel, "cfg-model", embeddings.LayerConfig)
	assertField(t, got, embeddings.FieldEndpoint, "http://cfg:11434", embeddings.LayerConfig)
	assertField(t, got, embeddings.FieldAPIKeyEnv, "CFG_KEY", embeddings.LayerConfig)
	assertField(t, got, embeddings.FieldDimension, "768", embeddings.LayerConfig)
}

func TestResolve_ConfigOverrideBeatsConfigFile(t *testing.T) {
	r := &embeddings.Resolver{
		Config:          config.EmbeddingProviderConfig{Model: "cfg-model"},
		ConfigOverrides: cOverrides(t, "providers.embedding.model=c-model", "providers.embedding.dimension=1024"),
		LookupEnv:       env(nil),
	}
	got := resolve(t, r, embeddings.Request{})
	assertField(t, got, embeddings.FieldModel, "c-model", embeddings.LayerConfigOverride)
	assertField(t, got, embeddings.FieldDimension, "1024", embeddings.LayerConfigOverride)
}

func TestResolve_EnvBeatsConfigFile(t *testing.T) {
	r := &embeddings.Resolver{
		Config:    config.EmbeddingProviderConfig{Model: "cfg-model", Endpoint: "http://cfg:11434"},
		LookupEnv: env(map[string]string{embeddings.EnvModel: "env-model"}),
	}
	got := resolve(t, r, embeddings.Request{})
	assertField(t, got, embeddings.FieldModel, "env-model", embeddings.LayerEnv)
	assertField(t, got, embeddings.FieldEndpoint, "http://cfg:11434", embeddings.LayerConfig)
}

func TestResolve_EnvBeatsConfigOverride(t *testing.T) {
	r := &embeddings.Resolver{
		ConfigOverrides: cOverrides(t, "providers.embedding.endpoint=http://c:11434"),
		LookupEnv:       env(map[string]string{embeddings.EnvEndpoint: "http://env:11434"}),
	}
	got := resolve(t, r, embeddings.Request{})
	assertField(t, got, embeddings.FieldEndpoint, "http://env:11434", embeddings.LayerEnv)
}

func TestResolve_FlagBeatsEnv(t *testing.T) {
	r := &embeddings.Resolver{
		LookupEnv: env(map[string]string{
			embeddings.EnvProvider: "stub",
			embeddings.EnvModel:    "env-model",
			embeddings.EnvEndpoint: "http://env:11434",
		}),
	}
	got := resolve(t, r, embeddings.Request{Overrides: embeddings.Overrides{
		Backend: "ollama", Model: "flag-model", Endpoint: "http://flag:11434",
	}})
	assertField(t, got, embeddings.FieldBackend, "ollama", embeddings.LayerFlag)
	assertField(t, got, embeddings.FieldModel, "flag-model", embeddings.LayerFlag)
	assertField(t, got, embeddings.FieldEndpoint, "http://flag:11434", embeddings.LayerFlag)
}

// A flag that overrides one field must not reset the others: precedence is
// per field, never whole-struct.
func TestResolve_PrecedenceIsPerField(t *testing.T) {
	r := &embeddings.Resolver{
		Config:          config.EmbeddingProviderConfig{Backend: "ollama", Model: "cfg-model", APIKeyEnv: "CFG_KEY"},
		ConfigOverrides: cOverrides(t, "providers.embedding.dimension=1024"),
		LookupEnv:       env(map[string]string{embeddings.EnvModel: "env-model"}),
	}
	got := resolve(t, r, embeddings.Request{Overrides: embeddings.Overrides{Endpoint: "http://127.0.0.1:11555"}})
	assertField(t, got, embeddings.FieldBackend, "ollama", embeddings.LayerConfig)
	assertField(t, got, embeddings.FieldModel, "env-model", embeddings.LayerEnv)
	assertField(t, got, embeddings.FieldEndpoint, "http://127.0.0.1:11555", embeddings.LayerFlag)
	assertField(t, got, embeddings.FieldAPIKeyEnv, "CFG_KEY", embeddings.LayerConfig)
	assertField(t, got, embeddings.FieldDimension, "1024", embeddings.LayerConfigOverride)
}

func TestResolve_EmptyValuesFallThrough(t *testing.T) {
	r := &embeddings.Resolver{
		Config:    config.EmbeddingProviderConfig{Model: "cfg-model"},
		LookupEnv: env(map[string]string{embeddings.EnvModel: ""}),
	}
	got := resolve(t, r, embeddings.Request{Overrides: embeddings.Overrides{Model: ""}})
	assertField(t, got, embeddings.FieldModel, "cfg-model", embeddings.LayerConfig)
}

func TestResolve_APIKeyEnvIsANameAndNeverResolvedToItsValue(t *testing.T) {
	r := &embeddings.Resolver{
		LookupEnv: env(map[string]string{
			embeddings.EnvAPIKeyEnv: "MY_EMBED_KEY",
			"MY_EMBED_KEY":          "sk-super-secret",
		}),
	}
	got := resolve(t, r, embeddings.Request{})
	assertField(t, got, embeddings.FieldAPIKeyEnv, "MY_EMBED_KEY", embeddings.LayerEnv)
	for _, e := range got.Explain() {
		if strings.Contains(e.Value, "sk-super-secret") {
			t.Fatalf("Explain leaked the key value: %+v", e)
		}
	}
}

func TestResolve_InvalidInputs(t *testing.T) {
	cases := []struct {
		name string
		r    *embeddings.Resolver
		req  embeddings.Request
		want string
	}{
		{
			name: "endpoint without scheme",
			r:    &embeddings.Resolver{LookupEnv: env(map[string]string{embeddings.EnvEndpoint: "127.0.0.1:11555"})},
			want: embeddings.EnvEndpoint,
		},
		{
			name: "non-numeric -c dimension",
			r:    &embeddings.Resolver{LookupEnv: env(nil), ConfigOverrides: cOverrides(t, "providers.embedding.dimension=big")},
			want: "providers.embedding.dimension",
		},
		{
			name: "negative config dimension",
			r:    &embeddings.Resolver{LookupEnv: env(nil), Config: config.EmbeddingProviderConfig{Dimension: -1}},
			want: "dimension",
		},
		{
			name: "model id without a registry",
			r:    &embeddings.Resolver{LookupEnv: env(nil)},
			req:  embeddings.Request{ModelID: "x@2026-09-26"},
			want: "registry",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.r.Resolve(context.Background(), tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want mention of %q", err, tc.want)
			}
		})
	}
}

// newRegistry stands up a real sqlite registry.
func newRegistry(t *testing.T) *registry.Store {
	t.Helper()
	d, err := sqlite.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	d.SetVectorDimension(sqlite.DefaultVectorDimension)
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("driver.Init: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return registry.New(d.DB())
}

const remoteModelID = "ollama-snowflake-arctic-embed2@2026-09-26"

func registerRemote(t *testing.T, reg *registry.Store) {
	t.Helper()
	err := reg.Register(context.Background(), registry.Model{
		ModelID:    remoteModelID,
		Provider:   "ollama",
		Dimension:  1024,
		ConfigJSON: `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://m3:11434","api_key_env":"M3_KEY"}`,
	}, false)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
}

// A request targeting a registered model_id takes that model's own provider
// settings over the config file, even when config names another model.
func TestResolve_RegistryEntryAppliesToTargetedModel(t *testing.T) {
	reg := newRegistry(t)
	registerRemote(t, reg)
	r := &embeddings.Resolver{
		Config:    config.EmbeddingProviderConfig{Backend: "stub", Model: "cfg-model", Endpoint: "http://cfg:11434", Dimension: 768},
		Registry:  reg,
		LookupEnv: env(nil),
	}
	got := resolve(t, r, embeddings.Request{ModelID: remoteModelID})
	if got.ModelID != remoteModelID {
		t.Errorf("ModelID = %q", got.ModelID)
	}
	assertField(t, got, embeddings.FieldBackend, "ollama", embeddings.LayerRegistry)
	assertField(t, got, embeddings.FieldModel, "snowflake-arctic-embed2", embeddings.LayerRegistry)
	assertField(t, got, embeddings.FieldEndpoint, "http://m3:11434", embeddings.LayerRegistry)
	assertField(t, got, embeddings.FieldAPIKeyEnv, "M3_KEY", embeddings.LayerRegistry)
	assertField(t, got, embeddings.FieldDimension, "1024", embeddings.LayerRegistry)
	for _, f := range []embeddings.Field{embeddings.FieldBackend, embeddings.FieldModel, embeddings.FieldDimension} {
		if !got.Fixed(f) {
			t.Errorf("%s not reported as fixed by the registry", f)
		}
	}
	for _, f := range []embeddings.Field{embeddings.FieldEndpoint, embeddings.FieldAPIKeyEnv} {
		if got.Fixed(f) {
			t.Errorf("%s reported as fixed; transport stays overridable", f)
		}
	}
}

// Flags, env and -c still override a registered model's transport:
// endpoint and api_key_env.
func TestResolve_TransportOverridesBeatRegistryEntry(t *testing.T) {
	reg := newRegistry(t)
	registerRemote(t, reg)
	r := &embeddings.Resolver{
		ConfigOverrides: cOverrides(t, "providers.embedding.api_key_env=C_KEY"),
		Registry:        reg,
		LookupEnv:       env(map[string]string{embeddings.EnvEndpoint: "http://env:11434"}),
	}
	got := resolve(t, r, embeddings.Request{
		ModelID:   remoteModelID,
		Overrides: embeddings.Overrides{Endpoint: "http://127.0.0.1:11555"},
	})
	assertField(t, got, embeddings.FieldEndpoint, "http://127.0.0.1:11555", embeddings.LayerFlag)
	assertField(t, got, embeddings.FieldAPIKeyEnv, "C_KEY", embeddings.LayerConfigOverride)
	assertField(t, got, embeddings.FieldBackend, "ollama", embeddings.LayerRegistry)
	assertField(t, got, embeddings.FieldModel, "snowflake-arctic-embed2", embeddings.LayerRegistry)
	assertField(t, got, embeddings.FieldDimension, "1024", embeddings.LayerRegistry)

	r.LookupEnv = env(map[string]string{embeddings.EnvEndpoint: "http://env:11434"})
	got = resolve(t, r, embeddings.Request{ModelID: remoteModelID})
	assertField(t, got, embeddings.FieldEndpoint, "http://env:11434", embeddings.LayerEnv)
}

// A registered model's backend, model and dimension are its vector-space
// identity: a runtime override that would change them fails instead of
// writing or querying foreign vectors under that model_id.
func TestResolve_RegisteredIdentityCannotBeOverridden(t *testing.T) {
	cases := []struct {
		name      string
		flags     embeddings.Overrides
		env       map[string]string
		overrides []string
		want      string
	}{
		{name: "flag backend", flags: embeddings.Overrides{Backend: "stub"}, want: "--embedding-provider"},
		{name: "flag model", flags: embeddings.Overrides{Model: "nomic-embed-text"}, want: "--embedding-model"},
		{name: "env backend", env: map[string]string{embeddings.EnvProvider: "stub"}, want: embeddings.EnvProvider},
		{name: "env model", env: map[string]string{embeddings.EnvModel: "nomic-embed-text"}, want: embeddings.EnvModel},
		{name: "-c backend", overrides: []string{"providers.embedding.backend=stub"}, want: "providers.embedding.backend"},
		{name: "-c model", overrides: []string{"providers.embedding.model=nomic-embed-text"}, want: "providers.embedding.model"},
		{name: "-c dimension", overrides: []string{"providers.embedding.dimension=768"}, want: "providers.embedding.dimension"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := newRegistry(t)
			registerRemote(t, reg)
			r := &embeddings.Resolver{Registry: reg, LookupEnv: env(tc.env)}
			if len(tc.overrides) > 0 {
				r.ConfigOverrides = cOverrides(t, tc.overrides...)
			}
			_, err := r.Resolve(context.Background(), embeddings.Request{ModelID: remoteModelID, Overrides: tc.flags})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), remoteModelID) {
				t.Fatalf("err = %v, want a refusal naming %q and the model", err, tc.want)
			}
		})
	}
}

// Restating the registered value is not a change.
func TestResolve_RegisteredIdentityRestatedIsAllowed(t *testing.T) {
	reg := newRegistry(t)
	registerRemote(t, reg)
	r := &embeddings.Resolver{Registry: reg, LookupEnv: env(map[string]string{embeddings.EnvProvider: "ollama"})}
	got := resolve(t, r, embeddings.Request{ModelID: remoteModelID, Overrides: embeddings.Overrides{Model: "snowflake-arctic-embed2"}})
	assertField(t, got, embeddings.FieldModel, "snowflake-arctic-embed2", embeddings.LayerRegistry)
	assertField(t, got, embeddings.FieldBackend, "ollama", embeddings.LayerRegistry)
}

// Backend and model come only from config_json: a registration without them
// cannot be resolved (the provider column and config file do not fill in).
func TestResolve_RegisteredModelNeedsIdentityInConfigJSON(t *testing.T) {
	for name, cj := range map[string]string{
		"no backend": `{"model":"snowflake-arctic-embed2"}`,
		"no model":   `{"backend":"ollama"}`,
	} {
		t.Run(name, func(t *testing.T) {
			reg := newRegistry(t)
			if err := reg.Register(context.Background(), registry.Model{
				ModelID: "partial@2026-09-26", Provider: "ollama", Dimension: 1024, ConfigJSON: cj,
			}, false); err != nil {
				t.Fatal(err)
			}
			r := &embeddings.Resolver{
				Registry: reg, LookupEnv: env(nil),
				Config: config.EmbeddingProviderConfig{Backend: "ollama", Model: "cfg-model"},
			}
			_, err := r.Resolve(context.Background(), embeddings.Request{ModelID: "partial@2026-09-26"})
			if err == nil || !strings.Contains(err.Error(), "config_json") {
				t.Fatalf("err = %v, want a config_json identity error", err)
			}
		})
	}
}

func TestResolve_NothingFixedWithoutModelID(t *testing.T) {
	got := resolve(t, &embeddings.Resolver{LookupEnv: env(nil)}, embeddings.Request{})
	for _, f := range embeddings.Fields {
		if got.Fixed(f) {
			t.Errorf("%s fixed without a targeted model", f)
		}
	}
}

func TestResolve_RegistryUnusedWithoutModelID(t *testing.T) {
	reg := newRegistry(t)
	registerRemote(t, reg)
	r := &embeddings.Resolver{Registry: reg, LookupEnv: env(nil)}
	got := resolve(t, r, embeddings.Request{})
	assertField(t, got, embeddings.FieldModel, embeddings.DefaultModel, embeddings.LayerDefault)
}

func TestResolve_UnknownModelID(t *testing.T) {
	r := &embeddings.Resolver{Registry: newRegistry(t), LookupEnv: env(nil)}
	_, err := r.Resolve(context.Background(), embeddings.Request{ModelID: "nope@2026-01-01"})
	if !errors.Is(err, registry.ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
}

func TestResolved_ProviderBackends(t *testing.T) {
	stub := resolve(t, &embeddings.Resolver{LookupEnv: env(map[string]string{embeddings.EnvProvider: "stub"})}, embeddings.Request{})
	p, err := stub.Provider()
	if err != nil || p.Name() != "stub" {
		t.Fatalf("stub backend: provider=%v err=%v", p, err)
	}

	legacy := resolve(t, &embeddings.Resolver{LookupEnv: env(map[string]string{embeddings.EnvProvider: "legacy-blob"})}, embeddings.Request{})
	if _, err := legacy.Provider(); err == nil || !strings.Contains(err.Error(), "legacy-blob") {
		t.Fatalf("unsupported backend: err = %v", err)
	}
}

// One run pointed at a remote Ollama (here via an SSH tunnel on local port
// 11555) through env alone: no config edit. The cassette was recorded with
// the tunnel end served by a real Ollama; re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run TestResolve_RemoteOllama ./internal/embeddings/
func TestResolve_RemoteOllamaThroughTunnelForOneRun(t *testing.T) {
	client, calls := providertest.OllamaClient(t, "testdata/cassettes/remote-ollama")
	r := &embeddings.Resolver{
		Config: config.EmbeddingProviderConfig{Backend: "ollama", Model: "nomic-embed-text", Endpoint: "http://localhost:11434"},
		LookupEnv: env(map[string]string{
			embeddings.EnvEndpoint: "http://127.0.0.1:11555",
			embeddings.EnvModel:    "snowflake-arctic-embed2",
		}),
		HTTPClient: client,
	}
	got := resolve(t, r, embeddings.Request{})
	assertField(t, got, embeddings.FieldEndpoint, "http://127.0.0.1:11555", embeddings.LayerEnv)

	p, err := got.Provider()
	if err != nil {
		t.Fatalf("Provider: %v", err)
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

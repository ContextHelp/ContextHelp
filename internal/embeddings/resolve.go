package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
)

// Field names one resolved provider setting. The values double as the
// providers.embedding config keys.
type Field string

// Provider settings, in display order (see Fields).
const (
	FieldBackend   Field = "backend"
	FieldModel     Field = "model"
	FieldEndpoint  Field = "endpoint"
	FieldAPIKeyEnv Field = "api_key_env"
	FieldDimension Field = "dimension"
)

// Fields lists every resolved setting in display order.
var Fields = []Field{FieldBackend, FieldModel, FieldEndpoint, FieldAPIKeyEnv, FieldDimension}

// Layer names where a resolved value came from.
type Layer string

// Layers, highest precedence first.
const (
	LayerFlag           Layer = "flag"
	LayerEnv            Layer = "env"
	LayerConfigOverride Layer = "config-override"
	LayerRegistry       Layer = "registry"
	LayerConfig         Layer = "config"
	LayerDefault        Layer = "default"
)

// Environment variables read by the env layer.
const (
	EnvProvider  = "CTXT_EMBEDDING_PROVIDER"
	EnvModel     = "CTXT_EMBEDDING_MODEL"
	EnvEndpoint  = "CTXT_EMBEDDING_ENDPOINT"
	EnvAPIKeyEnv = "CTXT_EMBEDDING_API_KEY_ENV"
)

// Built-in defaults, the lowest layer.
const (
	DefaultBackend  = "ollama"
	DefaultModel    = "nomic-embed-text"
	DefaultEndpoint = "http://localhost:11434"
)

// Backends Provider can build.
const (
	BackendOllama = "ollama"
	BackendStub   = "stub"
)

// configKeyPrefix is the dotted config path of the provider block.
const configKeyPrefix = "providers.embedding."

// Overrides holds per-invocation flag values. Empty strings are unset.
type Overrides struct {
	Backend  string
	Model    string
	Endpoint string
}

// Request names what to resolve.
type Request struct {
	// ModelID, when set, targets a registered embedding model: its registry
	// entry becomes a layer between -c and the config file.
	ModelID string
	// Overrides are the flag layer.
	Overrides Overrides
}

// ModelLookup reads one registry entry. *registry.Store satisfies it.
type ModelLookup interface {
	Get(ctx context.Context, modelID string) (*registry.Model, error)
}

// Resolver holds the inputs that do not vary per request.
type Resolver struct {
	// Config is providers.embedding as loaded from config files. It may
	// already include -c overrides; ConfigOverrides tells them apart.
	Config config.EmbeddingProviderConfig
	// ConfigOverrides is the raw -c key=value map (kit ConfigArgs output).
	ConfigOverrides map[string]any
	// LookupEnv reads the env layer; nil means os.LookupEnv.
	LookupEnv func(string) (string, bool)
	// Registry resolves Request.ModelID; required only when ModelID is set.
	Registry ModelLookup
	// HTTPClient, when set, is used by providers that make HTTP calls.
	HTTPClient *http.Client
}

// Resolved is the effective embedding provider configuration.
type Resolved struct {
	// ModelID is the registry model_id the request targeted, if any.
	ModelID   string
	Backend   string
	Model     string
	Endpoint  string
	APIKeyEnv string
	Dimension int

	sources    map[Field]Layer
	httpClient *http.Client
}

// Explanation is one resolved setting and the layer that supplied it.
type Explanation struct {
	Field Field  `json:"field"`
	Value string `json:"value"`
	Layer Layer  `json:"source"`
}

// settings is one layer's partial view: empty / zero fields are unset.
type settings struct {
	backend, model, endpoint, apiKeyEnv string
	dimension                           int
}

type layerSettings struct {
	layer Layer
	s     settings
	// origin names the concrete source for error messages (a flag name,
	// an env var, a config key).
	origin map[Field]string
}

// Resolve applies the layers per field and validates the result.
func (r *Resolver) Resolve(ctx context.Context, req Request) (Resolved, error) {
	layers, err := r.layers(ctx, req)
	if err != nil {
		return Resolved{}, err
	}

	out := Resolved{ModelID: req.ModelID, sources: map[Field]Layer{}, httpClient: r.HTTPClient}
	origin := map[Field]string{}
	pick := func(f Field, get func(settings) (string, bool), set func(string)) {
		for _, l := range layers {
			if v, ok := get(l.s); ok {
				set(v)
				out.sources[f] = l.layer
				origin[f] = l.origin[f]
				return
			}
		}
		// Nothing set it anywhere: the built-in default is "unset".
		out.sources[f] = LayerDefault
		origin[f] = "default"
	}
	str := func(sel func(settings) string) func(settings) (string, bool) {
		return func(s settings) (string, bool) { v := sel(s); return v, v != "" }
	}
	pick(FieldBackend, str(func(s settings) string { return s.backend }), func(v string) { out.Backend = v })
	pick(FieldModel, str(func(s settings) string { return s.model }), func(v string) { out.Model = v })
	pick(FieldEndpoint, str(func(s settings) string { return s.endpoint }), func(v string) { out.Endpoint = v })
	pick(FieldAPIKeyEnv, str(func(s settings) string { return s.apiKeyEnv }), func(v string) { out.APIKeyEnv = v })
	pick(FieldDimension, func(s settings) (string, bool) {
		return strconv.Itoa(s.dimension), s.dimension != 0
	}, func(v string) { out.Dimension, _ = strconv.Atoi(v) })

	if err := validateEndpoint(out.Endpoint); err != nil {
		return Resolved{}, fmt.Errorf("embedding endpoint %q (from %s): %w", out.Endpoint, origin[FieldEndpoint], err)
	}
	if out.Dimension < 0 {
		return Resolved{}, fmt.Errorf("embedding dimension %d (from %s) must not be negative", out.Dimension, origin[FieldDimension])
	}
	return out, nil
}

// layers returns every layer, highest precedence first.
func (r *Resolver) layers(ctx context.Context, req Request) ([]layerSettings, error) {
	lookup := r.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	getenv := func(k string) string { v, _ := lookup(k); return v }

	flag := layerSettings{
		layer: LayerFlag,
		s:     settings{backend: req.Overrides.Backend, model: req.Overrides.Model, endpoint: req.Overrides.Endpoint},
		origin: map[Field]string{
			FieldBackend: "--" + FlagProvider, FieldModel: "--" + FlagModel, FieldEndpoint: "--" + FlagEndpoint,
		},
	}
	envLayer := layerSettings{
		layer: LayerEnv,
		s: settings{
			backend: getenv(EnvProvider), model: getenv(EnvModel),
			endpoint: getenv(EnvEndpoint), apiKeyEnv: getenv(EnvAPIKeyEnv),
		},
		origin: map[Field]string{
			FieldBackend: EnvProvider, FieldModel: EnvModel, FieldEndpoint: EnvEndpoint, FieldAPIKeyEnv: EnvAPIKeyEnv,
		},
	}
	override, err := overrideLayer(r.ConfigOverrides)
	if err != nil {
		return nil, err
	}

	out := []layerSettings{flag, envLayer, override}
	if req.ModelID != "" {
		reg, err := r.registryLayer(ctx, req.ModelID)
		if err != nil {
			return nil, err
		}
		out = append(out, reg)
	}

	// The config layer holds only what -c did not set: a field -c set is
	// already decided one layer up, so the merged value adds nothing.
	c := r.Config
	cfgOrigin := map[Field]string{}
	for _, f := range Fields {
		cfgOrigin[f] = "config " + configKeyPrefix + string(f)
	}
	out = append(
		out,
		layerSettings{
			layer: LayerConfig,
			s: settings{
				backend: c.Backend, model: c.Model, endpoint: c.Endpoint,
				apiKeyEnv: c.APIKeyEnv, dimension: c.Dimension,
			},
			origin: cfgOrigin,
		},
		layerSettings{
			layer:  LayerDefault,
			s:      settings{backend: DefaultBackend, model: DefaultModel, endpoint: DefaultEndpoint},
			origin: map[Field]string{FieldBackend: "default", FieldModel: "default", FieldEndpoint: "default", FieldAPIKeyEnv: "default", FieldDimension: "default"},
		},
	)
	return out, nil
}

// overrideLayer extracts providers.embedding.* from kit's nested -c map.
func overrideLayer(overrides map[string]any) (layerSettings, error) {
	l := layerSettings{layer: LayerConfigOverride, origin: map[Field]string{}}
	for _, f := range Fields {
		l.origin[f] = "-c " + configKeyPrefix + string(f)
	}
	block, _ := overrides["providers"].(map[string]any)
	emb, _ := block["embedding"].(map[string]any)
	if emb == nil {
		return l, nil
	}
	str := func(f Field) string {
		v, ok := emb[string(f)]
		if !ok || v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
	l.s = settings{
		backend: str(FieldBackend), model: str(FieldModel),
		endpoint: str(FieldEndpoint), apiKeyEnv: str(FieldAPIKeyEnv),
	}
	if raw, ok := emb[string(FieldDimension)]; ok && raw != nil {
		n, err := strconv.Atoi(fmt.Sprint(raw))
		if err != nil {
			return l, fmt.Errorf("-c %s%s=%v: not an integer", configKeyPrefix, FieldDimension, raw)
		}
		l.s.dimension = n
	}
	return l, nil
}

// registryModelConfig is the subset of a registry entry's config_json the
// resolver reads.
type registryModelConfig struct {
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	APIKeyEnv string `json:"api_key_env"`
}

func (r *Resolver) registryLayer(ctx context.Context, modelID string) (layerSettings, error) {
	if r.Registry == nil {
		return layerSettings{}, fmt.Errorf("embedding model %s: no model registry available to resolve it", modelID)
	}
	m, err := r.Registry.Get(ctx, modelID)
	if err != nil {
		return layerSettings{}, fmt.Errorf("embedding model %s: %w", modelID, err)
	}
	var mc registryModelConfig
	if raw := strings.TrimSpace(m.ConfigJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &mc); err != nil {
			return layerSettings{}, fmt.Errorf("embedding model %s: config_json: %w", modelID, err)
		}
	}
	origin := map[Field]string{}
	for _, f := range Fields {
		origin[f] = "registry " + modelID
	}
	return layerSettings{
		layer: LayerRegistry,
		s: settings{
			backend: m.Provider, model: mc.Model, endpoint: mc.Endpoint,
			apiKeyEnv: mc.APIKeyEnv, dimension: m.Dimension,
		},
		origin: origin,
	}, nil
}

func validateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("want an http(s) URL such as %s", DefaultEndpoint)
	}
	return nil
}

// Source reports the layer that supplied f.
func (r Resolved) Source(f Field) Layer { return r.sources[f] }

// Value renders f's resolved value as a string.
func (r Resolved) Value(f Field) string {
	switch f {
	case FieldBackend:
		return r.Backend
	case FieldModel:
		return r.Model
	case FieldEndpoint:
		return r.Endpoint
	case FieldAPIKeyEnv:
		return r.APIKeyEnv
	case FieldDimension:
		return strconv.Itoa(r.Dimension)
	}
	return ""
}

// Explain lists every setting with its value and source, in display order.
// api_key_env is reported by name only.
func (r Resolved) Explain() []Explanation {
	out := make([]Explanation, 0, len(Fields))
	for _, f := range Fields {
		out = append(out, Explanation{Field: f, Value: r.Value(f), Layer: r.Source(f)})
	}
	return out
}

// Provider builds the embedding provider for the resolved settings.
func (r Resolved) Provider() (providers.EmbeddingProvider, error) {
	switch r.Backend {
	case BackendOllama:
		return providers.NewOllamaEmbeddingProvider(
			r.Endpoint, r.Model,
			providers.WithOllamaEmbeddingHTTPClient(r.httpClient),
			providers.WithOllamaEmbeddingDimension(r.Dimension),
		), nil
	case BackendStub:
		return providers.NewStubEmbeddingProvider(), nil
	default:
		return nil, fmt.Errorf("embedding backend %q (from %s) is not supported; supported: %s, %s",
			r.Backend, r.Source(FieldBackend), BackendOllama, BackendStub)
	}
}

// Package stack assembles the dpkms request-serving stack over an open
// storage driver: job queue, pipeline registry, search engine, event
// bus, policy engine, service, watcher manager, security emitter,
// inbound entitlement gate and HTTP router.
//
// dpkms serve builds its stack here, and so does the in-process test
// server (internal/dpkmstest), so a command test talks to the same API
// an operator's daemon serves. Process concerns stay with the caller:
// storage init, listeners, the /ws/bus hub, the worker pool, gRPC and
// signal handling.
package stack

import (
	"fmt"
	"io"

	"github.com/go-chi/chi/v5"

	kitbus "hop.top/kit/go/runtime/bus"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	embregistry "github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/policy"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// Inputs is what the caller resolves before the stack is built.
type Inputs struct {
	// Config is the loaded dpkms config: providers, pipelines,
	// duplicates, blob storage, secrets, security alerts and quotas.
	Config *config.Config
	// Driver is the initialized storage driver the stack serves.
	Driver storage.StorageDriver
	// Access is the effective access class (config.Access*), as
	// ResolveInboundAuth returns it.
	Access string
	// Auth is the inbound authentication provider. It guards the route
	// table on non-private instances only.
	Auth authn.Provider
	// PolicyBus is the bus the policy engine subscribes to; dpkms serve
	// passes its /ws/bus hub.
	PolicyBus kitbus.Bus
	// Embeddings resolves each model's embedding provider for ingest.
	Embeddings *embeddings.Resolver
	// Browser is the optional browser automation client.
	Browser *browser.Client
	// StepsPath is the external steps directory.
	StepsPath string
	// ConfigPath is the config file the event subscriber reports.
	ConfigPath string
	// Probes injects runtime signals into /healthz.
	Probes httpserver.HealthzProbes
	// DevCORS enables CORS for the Vite dev server.
	DevCORS bool
	// Warnings receives non-fatal assembly warnings. nil discards them.
	Warnings io.Writer
}

// Stack is the assembled request-serving stack.
type Stack struct {
	Queue     *jobs.Queue
	Pipelines pipeline.Registry
	Engine    *search.Engine
	// Bus is the in-process event bus the service publishes on.
	Bus     events.Bus
	Service *service.Service
	Watcher *watcher.Manager
	// RouteAuth is the provider guarding the route table; nil on
	// private instances.
	RouteAuth    authn.Provider
	Security     *security.Emitter
	Entitlements *registry.InboundGate
	Router       chi.Router

	policy *policy.Bootstrap
}

// Close releases what Build acquired beyond the driver: it unwires the
// policy engine from its bus. Idempotent.
func (s *Stack) Close() {
	if s != nil {
		s.policy.Close()
	}
}

// ResolveInboundAuth folds the --public flag into the loaded config,
// enforces the access-class rules, and constructs the configured
// authentication provider. Returns the effective access class and the
// provider (nil when no auth is configured, which is only legal for
// private instances). Any misconfiguration (unknown class, explicit
// private + --public, non-private without credentials, broken provider
// config) is an error, so dpkms serve refuses before a port is bound.
func ResolveInboundAuth(c *config.Config, publicFlag bool) (string, authn.Provider, error) {
	folded := *c
	folded.Server.Public = folded.Server.Public || publicFlag
	if err := folded.ValidateAccess(); err != nil {
		return "", nil, err
	}
	access := folded.Server.EffectiveAccess()

	provider, err := authn.FromConfig(c.Server.Auth)
	if err != nil {
		return "", nil, err
	}
	if access != config.AccessPrivate && provider == nil {
		// Unreachable while ValidateAccess covers credential presence;
		// kept as a hard stop so a validation regression can never
		// expose an unauthenticated non-private listener.
		return "", nil, fmt.Errorf("config: %s instance requires inbound authentication (server.auth)", access)
	}
	return access, provider, nil
}

// EmbeddingBuildOpts wires the embedding write path into the pipeline
// registry: the populate set from the driver's model registry, each
// model's provider through r, and the driver's per-model vector index.
// Without a readable registry, ingest writes no vectors (reported on
// warn). The duplicates config rides along for dedup, which runs on
// those vectors.
func EmbeddingBuildOpts(driver storage.StorageDriver, r *embeddings.Resolver, dups config.DuplicatesConfig, warn io.Writer) builtins.BuildOpts {
	opts := builtins.BuildOpts{
		Resolver:   embeddings.NewProviderResolver(r),
		Embeddings: driver.Embeddings(),
		Audit:      driver.AuditLog(),
		Duplicates: dups,
	}
	reg, err := embregistry.ForDriver(driver)
	if err != nil {
		if warn != nil {
			fmt.Fprintf(warn, "Warning: embedding model registry unavailable; ingest writes no vectors: %v\n", err)
		}
		return opts
	}
	opts.Models = reg
	return opts
}

// Build assembles the stack over in.Driver.
func Build(in Inputs) (*Stack, error) {
	cfg := in.Config
	if cfg == nil {
		return nil, fmt.Errorf("stack: config is required")
	}
	if in.Driver == nil {
		return nil, fmt.Errorf("stack: storage driver is required")
	}
	if in.PolicyBus == nil {
		return nil, fmt.Errorf("stack: policy bus is required")
	}
	resolver := in.Embeddings
	if resolver == nil {
		resolver = &embeddings.Resolver{Config: cfg.Providers.Embedding}
	}
	access := in.Access
	if access == "" {
		access = config.AccessPrivate
	}

	s := &Stack{Queue: jobs.NewQueue(in.Driver.Jobs())}

	// Pipeline registry with configured providers and per-pipeline
	// overrides, validated at enqueue time.
	secretsStore, err := secrets.New(cfg.Secrets)
	if err != nil {
		return nil, fmt.Errorf("init secrets: %w", err)
	}
	buildOpts := EmbeddingBuildOpts(in.Driver, resolver, cfg.Duplicates, in.Warnings)
	buildOpts.Factory = providers.NewFactory(cfg.Providers, secretsStore)
	buildOpts.BlobStore = in.Driver.Blobs()
	buildOpts.BlobThreshold = cfg.Storage.Blob.Threshold
	buildOpts.BrowserClient = in.Browser
	s.Pipelines = builtins.ConfiguredRegistryWithPipelineOverrides(buildOpts, cfg.Providers, cfg.Pipelines)
	pipes := s.Pipelines
	s.Queue.SetPipelineValidator(func(name string) error {
		_, err := pipes.Get(name)
		return err
	})

	s.Engine = search.NewEngine(in.Driver)

	bus := events.NewLocalBus()
	events.SetupSubscriber(bus, cfg, in.ConfigPath)
	s.Bus = bus

	// kit/runtime/policy on the caller's bus. Misconfig (bad YAML,
	// unknown topic, broken CEL) fails loud so the daemon never serves
	// traffic against an unenforced ruleset. Non-private instances
	// refuse the env principal fallback: a remote caller with no
	// authenticated principal resolves as anonymous, never as the
	// daemon operator's $USER/$KIT_POLICY_ROLE. policy.Init already
	// prefixes its errors with "policy:".
	var polOpts []policy.Option
	if access != config.AccessPrivate {
		polOpts = append(polOpts, policy.WithoutEnvPrincipal())
	}
	pol, err := policy.Init(in.PolicyBus, polOpts...)
	if err != nil {
		return nil, err
	}
	s.policy = pol

	s.Service = service.NewWithOptions(
		in.Driver, s.Queue, s.Pipelines, s.Engine, in.StepsPath, bus,
		[]service.Option{service.WithPolicyPublisher(pol.Publisher())},
		*cfg,
	)
	s.Watcher = watcher.NewManager(in.Driver.Watches(), s.Service)

	// Non-private instances authenticate the whole /api/v1 route table
	// (MCP mount included) through the provider-agnostic middleware.
	if access != config.AccessPrivate {
		s.RouteAuth = in.Auth
	}
	// Auth failures and ACL denials feed the audit log
	// (event_class=security) plus optional webhook/SMTP alerting. Wired
	// on every instance: policy denials matter on private ones too.
	s.Security = security.New(security.ConfigFromAlerts(cfg.Security.Alerts), in.Driver.AuditLog())

	// Inbound entitlement/metering gate for the entity-serving surface.
	// Only authenticated instances construct one: grants are
	// entitlement rows keyed by principal ID, quotas come from
	// server.quotas. nil on private instances = no gating.
	if s.RouteAuth != nil {
		s.Entitlements = registry.NewInboundGate(in.Driver.Entitlements(), in.Driver.Metering())
		for _, q := range cfg.Server.Quotas {
			s.Entitlements.SetQuota(q.Principal, storage.MeteringEventType(q.Event), storage.QuotaConfig{
				Limit:  q.Limit,
				WarnAt: q.WarnAt,
			})
		}
	}

	s.Router = httpserver.NewRouterWithConfig(s.Service, httpserver.RouterConfig{
		DevCORS:      in.DevCORS,
		Watcher:      s.Watcher,
		Probes:       in.Probes,
		Auth:         s.RouteAuth,
		Security:     s.Security,
		Entitlements: s.Entitlements,
		// On non-private instances the federation push route is gated
		// per federation.token, never by a principal token alone.
		RequireFederationCredential: access != config.AccessPrivate,
		RedactHealthz:               access == config.AccessPublic,
	})
	return s, nil
}

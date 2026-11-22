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
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	kitbus "hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/storage/secret"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/bosaccess"
	"github.com/ideacrafterslabs/ctxt/internal/bosfeed"
	"github.com/ideacrafterslabs/ctxt/internal/bosingest"
	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	embregistry "github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/github"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/policy"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
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
	// Auth replaces the authentication chain Build assembles from
	// server.auth and the driver's principal registry (tests). nil
	// builds the configured chain. Ignored on private instances.
	Auth *authn.Chain
	// PolicyBus is the bus the policy engine subscribes to; dpkms serve
	// passes its /ws/bus hub.
	PolicyBus kitbus.Bus
	// Embeddings resolves each model's embedding provider for ingest.
	Embeddings *embeddings.Resolver
	// Browser is the optional browser automation client.
	Browser *browser.Client
	// StepsPath is the external steps directory.
	StepsPath string
	// StepRunners replaces the runners installed steps run through
	// (tests); nil means steps.DefaultRunners.
	StepRunners *steps.Runners
	// ConfigPath is the config file the event subscriber reports.
	ConfigPath string
	// Probes injects runtime signals into /healthz.
	Probes httpserver.HealthzProbes
	// DevCORS enables CORS for the Vite dev server.
	DevCORS bool
	// HTTPPort is the port the HTTP listener actually bound; the Host
	// allowlist (see HostAllowlist) names it. 0 means no listener
	// serves Router by name (tests driving Router in-process) and
	// disables the Host check.
	HTTPPort int
	// Warnings receives non-fatal assembly warnings. nil discards them.
	Warnings io.Writer
	// Notes receives startup notes (web UI sign-in availability and
	// session sweeps). nil discards them.
	Notes io.Writer
	// StrictSharing makes the visibility backstop refuse a response it
	// would otherwise strip (ADR-080 §8): dpkms serve --strict-sharing,
	// and every test instance.
	StrictSharing bool
	// Authority is the access state an external authority provisions. nil
	// builds one over the instance database when sharing.authority.enabled
	// is set, and none otherwise.
	Authority *bosaccess.Peer
	// AuthorityContent fetches the content of the authority's records for
	// ingest; nil reads them through the authority's served read commands
	// (bosingest.RESTReader) with the feed credential.
	AuthorityContent bosingest.ContentReader
}

// authorityStateKey is where the access state of the authority is kept in
// the instance key-value store.
const authorityStateKey = "bosaccess/state"

// authorityCursorKey is where the feed cursor is kept.
const authorityCursorKey = "bosaccess/feed-cursor"

// Stack is the assembled request-serving stack.
type Stack struct {
	Queue     *jobs.Queue
	Pipelines pipeline.Registry
	Engine    *search.Engine
	// Providers builds the configured providers (LLM, OCR, ...) the
	// pipelines use; task jobs that run steps outside a pipeline use it too.
	Providers *providers.Factory
	// Bus is the in-process event bus the service publishes on.
	Bus     kitbus.Bus
	Service *service.Service
	Watcher *watcher.Manager
	// RouteAuth is the authentication chain guarding the route table
	// and gRPC; nil on private instances.
	RouteAuth *authn.Chain
	// Keys mints and verifies the registry API keys RouteAuth accepts.
	// A private instance authenticates nothing but still manages keys
	// over the API, so it holds one too.
	Keys *authn.Keys
	// UISessions is the web UI sign-in session manager; nil when the
	// instance offers no browser sign-in (private instances, a
	// server.auth.methods without session). Only the HTTP chain runs
	// the session method: gRPC carries no cookies.
	UISessions *authn.Sessions
	// Tickets mints and redeems invite and sign-in links; nil when no
	// ceremony may mint a session (no UISessions, or sign_in: []).
	Tickets      *authn.Tickets
	Security     *security.Emitter
	Entitlements *registry.InboundGate
	Router       chi.Router
	// StrictSharing is Inputs.StrictSharing, for the gRPC server the
	// caller builds beside the router.
	StrictSharing bool
	// Audit is the read audit writer (ADR-080 §9) the router records
	// into; the gRPC server the caller builds takes it too. Shutdown
	// flushes it.
	Audit *sharing.AuditWriter
	// Approvals is the shared approval mechanism (ADR-083 §9) with its
	// kinds registered; its queue is served on /api/v1/approvals.
	// Stewards is the grant half of steward.grant (revocation, state).
	Approvals *approval.Service
	Stewards  *steward.Service
	// Authority is the access state of an external authority; nil when the
	// instance takes none.
	Authority *bosaccess.Peer
	stopFeed  context.CancelFunc

	policy *policy.Bootstrap
}

// closeDrainTimeout bounds Close's wait for queued security audit rows.
const closeDrainTimeout = 5 * time.Second

// Shutdown releases what Build acquired beyond the driver, and must run
// before the driver closes: it flushes the read audit writer, stops the
// security emitter and drains its queued audit rows within ctx, then
// unwires the policy engine from its bus. Idempotent; a drain cut short
// by ctx is returned as an error.
func (s *Stack) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	err := s.Audit.Close(ctx)
	if s.Security != nil {
		if serr := s.Security.Close(ctx); serr != nil && err == nil {
			err = serr
		}
	}
	s.policy.Close()
	if s.stopFeed != nil {
		s.stopFeed()
	}
	return err
}

// startAuthorityFeed reads the authority's change feed in the background:
// each event updates what records stand under and, with a content reader,
// ingests the record. The cursor lives in the instance key-value store.
func (s *Stack) startAuthorityFeed(in Inputs, fc config.AuthorityFeedConfig, authScope string, secretsStore secret.Store) {
	if s.Authority == nil || fc.URL == "" || authScope == "" {
		return
	}
	token := ""
	if got, err := secretsStore.Get(context.Background(), "bos/feed-token"); err == nil {
		token = string(got.Value)
	}
	r := &bosfeed.Reader{
		Source: &bosfeed.Client{BaseURL: fc.URL, Peer: fc.Peer, Scope: scopeID(authScope), Token: token},
		Basis:  s.Authority,
		Cursor: sharing.KVCursor{KV: in.Driver.Auth().KV(), Key: authorityCursorKey},
		Limit:  500,
	}
	content := in.AuthorityContent
	if content == nil {
		content = &bosingest.RESTReader{BaseURL: fc.URL, Token: token}
	}
	r.Sinks = []bosfeed.Sink{&bosingest.Sink{Reader: content, Store: in.Driver.Objects(), Runner: ingest.NewRunner(in.Driver.Objects())}}
	interval := fc.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.stopFeed = cancel
	go r.Run(ctx, interval, func(err error) { slog.Warn("authority feed", "err", err) })
}

// Close is Shutdown bounded by a fixed drain timeout, for deferred and
// test cleanup use. Idempotent.
func (s *Stack) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), closeDrainTimeout)
	defer cancel()
	_ = s.Shutdown(ctx)
}

// ResolveInboundAuth folds the --public flag into the loaded config and
// enforces the access-class and server.auth rules, returning the
// effective access class. Any misconfiguration (unknown class, explicit
// private + --public, broken server.auth, a link-minting instance with
// no public_url) is an error, so dpkms serve refuses before a port is
// bound. Whether the instance holds a credential is checked by Build
// against the registry (authn.RequireOwnerCredential).
func ResolveInboundAuth(c *config.Config, publicFlag bool) (string, error) {
	folded := *c
	folded.Server.Public = folded.Server.Public || publicFlag
	if err := folded.ValidateAccess(); err != nil {
		return "", err
	}
	return folded.Server.EffectiveAccess(), nil
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

// QuerySemantic is the query path's reach into the default embedding
// model's index: the driver's model registry, read per query, and each
// model's provider through r, so POST /find embeds its query on this
// host. Without a readable registry there is no default model and
// vector and hybrid finds answer full-text only (reported on warn).
func QuerySemantic(driver storage.StorageDriver, r *embeddings.Resolver, warn io.Writer) retrieval.SemanticSource {
	src := retrieval.SemanticSource{Resolver: embeddings.NewProviderResolver(r)}
	reg, err := embregistry.ForDriver(driver)
	if err != nil {
		if warn != nil {
			fmt.Fprintf(warn, "Warning: embedding model registry unavailable; find answers full-text only: %v\n", err)
		}
		return src
	}
	src.Models = reg
	return src
}

// HostAllowlist returns the Host header check for an HTTP server bound
// to port, or nil when none applies. Private instances always check
// Host, against the loopback names for the bound port plus extra
// (server.allowed_hosts): a hostile page that rebinds its DNS name to
// 127.0.0.1 is otherwise same-origin with an unauthenticated API.
// Protected and public instances bind every interface and are reached
// by names dpkms cannot know (LAN addresses, proxies), so they check
// Host only when extra is set; the loopback names stay allowed then so
// local clients keep working.
func HostAllowlist(access string, port int, extra []string) (*httpserver.HostAllowlist, error) {
	if access != config.AccessPrivate && len(extra) == 0 {
		return nil, nil
	}
	p := strconv.Itoa(port)
	entries := append([]string{
		net.JoinHostPort("127.0.0.1", p),
		net.JoinHostPort("localhost", p),
	}, extra...)
	hosts, err := httpserver.NewHostAllowlist(entries)
	if err != nil {
		return nil, fmt.Errorf("config: server.%w", err)
	}
	return hosts, nil
}

// uiSessionRetention keeps ended sessions listable this long before
// Build prunes them.
const uiSessionRetention = 30 * 24 * time.Hour

// newUISessions builds the web UI session manager of a non-private
// instance over the driver's session store and principal registry, or
// returns nil (private instances need no sign-in). At start it prunes
// sessions that ended more than uiSessionRetention ago. base carries
// the steward elevation's settings (ADR-083 §3: the idle timeout and
// the hook its starts and ends are reported to); the session bounds
// come from bounds.
func newUISessions(ctx context.Context, out io.Writer, driver storage.StorageDriver, keys *authn.Keys, access string, bounds config.UISessionConfig, base authn.SessionOptions) (*authn.Sessions, error) {
	if access == config.AccessPrivate {
		return nil, nil
	}
	base.IdleTTL, base.MaxTTL = bounds.IdleTTL, bounds.MaxTTL
	sessions, err := authn.NewSessions(driver.UISessions(), driver.Auth(), keys, base)
	if err != nil {
		return nil, fmt.Errorf("web UI sessions: %w", err)
	}
	if _, err := driver.UISessions().Prune(ctx, time.Now().Add(-uiSessionRetention)); err != nil {
		return nil, fmt.Errorf("web UI sessions: prune: %w", err)
	}
	fmt.Fprintf(out, "Web UI sign-in: ctxt ui open (sessions end after %s idle, %s at most)\n",
		sessions.IdleTTL(), sessions.MaxTTL())
	return sessions, nil
}

// buildAuth assembles the authentication of a non-private instance
// (ADR-079): registry keys and config-declared keys behind the api_key
// method, browser sessions behind the session method, chained in the
// configured order with carried methods first. It refuses an instance
// with no active owner credential, naming the command that creates one.
// A browser session's steward elevation ends after
// steward.elevation_idle, and its starts and ends are reported to sec.
func buildAuth(ctx context.Context, notes io.Writer, driver storage.StorageDriver, cfg *config.Config, access string, sec *security.Emitter) (*authn.Chain, *authn.Keys, *authn.Sessions, error) {
	keys := authn.NewKeysOver(driver.Auth(), nil)
	cfgKeys, err := authn.ConfigKeysFrom(cfg.Server.Auth.Keys)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("config: %w", err)
	}
	if err := authn.RequireOwnerCredential(ctx, keys, cfgKeys, time.Now()); err != nil {
		return nil, nil, nil, fmt.Errorf("config: %s instance: %w", access, err)
	}
	methods := cfg.Server.Auth.EffectiveMethods()
	available := []authn.Method{authn.NewAPIKeyMethod(keys, cfgKeys)}
	var sessions *authn.Sessions
	if slices.Contains(methods, authn.MethodSession) {
		elevation := authn.SessionOptions{
			ElevationIdle: cfg.Steward.EffectiveElevationIdle(),
			OnElevation:   steward.ElevationEvents(sec),
		}
		if sessions, err = newUISessions(ctx, notes, driver, keys, access, cfg.Server.UI.Session, elevation); err != nil {
			return nil, nil, nil, err
		}
		available = append(available, sessions.Method())
	}
	chain, err := authn.ChainFor(methods, available...)
	if err != nil {
		return nil, nil, nil, err
	}
	return chain, keys, sessions, nil
}

// openAuthority returns the injected authority peer, or opens one over the
// driver's KV when the config enables it; nil when neither applies.
func openAuthority(in Inputs, cfg *config.Config) (*bosaccess.Peer, error) {
	if in.Authority != nil || !cfg.Sharing.Authority.Enabled {
		return in.Authority, nil
	}
	return bosaccess.Open(bosaccess.DeclaredProfile(in.Probes.Version),
		sharing.KVState{KV: in.Driver.Auth().KV(), Key: authorityStateKey}, nil)
}

// applyAuthorityRoutes points the router's access decisions at the authority
// peer; a no-op without one.
func applyAuthorityRoutes(rcfg *httpserver.RouterConfig, peer *bosaccess.Peer, ac config.AuthorityConfig) {
	if peer == nil {
		return
	}
	rcfg.Access = peer
	rcfg.AccessPrincipal = ac.Principal
	rcfg.AccessScope = ac.Scope
}

// newEntitlements builds the inbound gate with the configured quotas.
func newEntitlements(driver storage.StorageDriver, cfg *config.Config) *registry.InboundGate {
	g := registry.NewInboundGate(driver.Entitlements(), driver.Metering())
	for _, q := range cfg.Server.Quotas {
		g.SetQuota(q.Principal, storage.MeteringEventType(q.Event), storage.QuotaConfig{
			Limit:  q.Limit,
			WarnAt: q.WarnAt,
		})
	}
	return g
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

	var hosts *httpserver.HostAllowlist
	if in.HTTPPort != 0 {
		var err error
		if hosts, err = HostAllowlist(access, in.HTTPPort, cfg.Server.AllowedHosts); err != nil {
			return nil, err
		}
	}

	s := &Stack{Queue: jobs.NewQueue(in.Driver.Jobs()), StrictSharing: in.StrictSharing}

	// Pipeline registry with configured providers and per-pipeline
	// overrides, validated at enqueue time.
	secretsStore, err := secrets.New(cfg.Secrets)
	if err != nil {
		return nil, fmt.Errorf("init secrets: %w", err)
	}
	buildOpts := EmbeddingBuildOpts(in.Driver, resolver, cfg.Duplicates, in.Warnings)
	buildOpts.Factory = providers.NewFactory(cfg.Providers, secretsStore)
	s.Providers = buildOpts.Factory
	buildOpts.BlobStore = in.Driver.Blobs()
	buildOpts.BlobThreshold = cfg.Storage.Blob.Threshold
	buildOpts.BrowserClient = in.Browser
	buildOpts.FetchGuard = config.FetchGuard(cfg)
	buildOpts.GitHub = github.NewClientFromConfig(cfg.GitHub, secrets.Getter(secretsStore))
	// ADR-080 §3, §7: on protected and public instances every pipeline
	// runs content_classes with the configured class rules and the
	// embedding step writes the masked variant; a private instance
	// shares nothing and does neither.
	buildOpts.Access = access
	buildOpts.ClassRules = builtins.ClassRulesFromConfig(cfg.Sharing)
	// Installed steps pipelines.overrides may insert (ADR-058).
	runners := steps.DefaultRunners()
	if in.StepRunners != nil {
		runners = *in.StepRunners
	}
	catalog, err := steps.LoadCatalog(context.Background(), in.Driver, runners)
	if err != nil {
		return nil, err
	}
	buildOpts.External = catalog
	s.Pipelines, err = builtins.ConfiguredRegistryWithPipelineOverrides(buildOpts, cfg.Providers, cfg.Pipelines)
	if err != nil {
		return nil, err
	}
	pipes := s.Pipelines
	s.Queue.SetPipelineValidator(func(name string) error {
		_, err := pipes.Get(name)
		return err
	})

	s.Engine = search.NewEngine(in.Driver)

	bus := kitbus.New(kitbus.WithInvalidTopicReporter(func(err error) {
		slog.Warn("event bus: topic outside kit grammar", "err", err)
	}))
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

	if s.Authority, err = openAuthority(in, cfg); err != nil {
		return nil, err
	}
	svcOpts := []service.Option{
		service.WithPolicyPublisher(pol.Publisher()),
		service.WithReservedStepNames(builtins.IsStepName),
		// A server has no login keychain: registry tokens come from
		// secrets.backend under registry/<name>.
		service.WithRegistryTokens(registry.SecretTokens(func() (secret.Store, error) { return secretsStore, nil })),
	}
	if s.Authority != nil {
		svcOpts = append(svcOpts, service.WithAuthorityAccess(s.Authority))
	}
	s.Service = service.NewWithOptions(
		in.Driver, s.Queue, s.Pipelines, s.Engine, in.StepsPath, bus, svcOpts, *cfg,
	)
	s.Watcher = watcher.NewManager(in.Driver.Watches(), watcher.StoreRecords(in.Driver.Watches()), serviceIngester{s.Service})

	// Non-private instances authenticate the whole /api/v1 route table
	// (MCP mount included) through the method chain: the configured one
	// over the driver's principal registry, or the one a test injects.
	notes := in.Notes
	if notes == nil {
		notes = io.Discard
	}
	// Auth failures and ACL denials feed the audit log
	// (event_class=security) plus optional webhook/SMTP alerting. Wired
	// on every instance: policy denials matter on private ones too. It
	// comes first: the session manager reports steward elevations to it.
	s.Security = security.New(security.ConfigFromAlerts(cfg.Security.Alerts), in.Driver.AuditLog())
	if err := s.wireAuth(in, access, notes); err != nil {
		s.Close()
		return nil, err
	}

	// Inbound entitlement/metering gate for the entity-serving surface.
	// Only authenticated instances construct one: grants are
	// entitlement rows keyed by principal ID, quotas come from
	// server.quotas. nil on private instances = no gating.
	if s.RouteAuth != nil {
		s.Entitlements = newEntitlements(in.Driver, cfg)
	}

	// The read audit of shared viewers (ADR-080 §9): buffered, flushed
	// off the request path.
	s.Audit = sharing.NewAuditWriter(in.Driver.Sharing().AccessLog(), sharing.AuditOptions{})

	if err := s.wireApprovals(in, cfg); err != nil {
		s.Close()
		return nil, err
	}

	rcfg := httpserver.RouterConfig{
		Semantic:     QuerySemantic(in.Driver, resolver, in.Warnings),
		DevCORS:      in.DevCORS,
		Watcher:      s.Watcher,
		Probes:       in.Probes,
		Auth:         s.RouteAuth,
		Sessions:     s.UISessions,
		Security:     s.Security,
		Entitlements: s.Entitlements,
		// The principal registry routes and the link ceremonies
		// (ADR-079 §5): links are built on server.public_url.
		Registry:  in.Driver.Auth(),
		Keys:      s.Keys,
		Sharing:   in.Driver.Sharing(),
		Tickets:   s.Tickets,
		SignIn:    cfg.Server.Auth.EffectiveSignIn(),
		PublicURL: cfg.Server.PublicURL,
		// On non-private instances the federation push route takes a
		// peer principal's key, never any authenticated principal.
		RequirePeerPrincipal: access != config.AccessPrivate,
		RedactHealthz:        access == config.AccessPublic,
		Hosts:                hosts,
		StrictSharing:        in.StrictSharing,
		Audit:                s.Audit,
		AuditLog:             in.Driver.AuditLog(),
		Approvals:            s.Approvals,
		Stewards:             s.Stewards,
	}
	applyAuthorityRoutes(&rcfg, s.Authority, cfg.Sharing.Authority)
	s.Router = httpserver.NewRouterWithConfig(s.Service, rcfg)
	s.startAuthorityFeed(in, cfg.Sharing.Authority.Feed, cfg.Sharing.Authority.Scope, secretsStore)
	return s, nil
}

// scopeID is the part of a scope address after the last slash.
func scopeID(addr string) string { return addr[strings.LastIndex(addr, "/")+1:] }

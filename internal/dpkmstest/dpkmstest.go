// Package dpkmstest runs the dpkms API in process for tests: the router
// and service dpkms serve builds (internal/server/stack), behind an
// httptest server on 127.0.0.1, over the caller's storage driver.
//
// Tests seed through the driver and exercise commands or clients over
// HTTP against the same API an operator's daemon serves:
//
//	driver := storageutil.NewTestDriver(t)
//	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())
//	// srv.URL, srv.Token(dpkmstest.RoleReader)
//
// The package that calls Start must run its tests through
// internal/testguard (a TestMain calling testguard.Main); Start fails
// the test otherwise.
//
// The instance is hermetic: every provider backend is the stub and the
// embedding resolver sees only a stub provider on a closed port, so
// nothing reaches a local model server. WithEmbeddingHTTPClient lets a
// registered model's own provider run over a recorded transport instead.
// No worker pool runs: enqueued jobs stay pending. Import it from test
// files only.
package dpkmstest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	kitbus "hop.top/kit/go/runtime/bus"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/server/stack"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// Roles the test keys grant, one principal each: the seeded owner
// (admin), a device (writer) and a person (reader). They are plain role
// strings on the principal; what each may do is the server's
// authorization decision.
const (
	RoleAdmin  = "admin"
	RoleWriter = "writer"
	RoleReader = "reader"
)

// Roles lists every role WithKeys issues a key for.
var Roles = []string{RoleAdmin, RoleWriter, RoleReader}

// Principal IDs behind the WithKeys roles: the admin key is the owner's.
const (
	PrincipalAdmin  = storage.OwnerPrincipal
	PrincipalWriter = "test-writer"
	PrincipalReader = "test-reader"
)

// Server is a running in-process dpkms instance.
type Server struct {
	// URL is the base URL, http://127.0.0.1:<ephemeral port>. For an
	// Unreachable instance nothing listens there.
	URL string
	// Driver is the storage driver the instance serves.
	Driver storage.StorageDriver
	// Config is the effective dpkms config the stack was built from.
	Config *config.Config
	// Stack is the assembled stack; nil for an Unreachable instance.
	Stack *stack.Stack

	tokens map[string]string
}

// Token returns the API key for role, or "" when the instance issues
// none for it (private or Unreachable instances, custom methods).
func (s *Server) Token(role string) string { return s.tokens[role] }

// Principal returns the registry id behind role's key.
func (s *Server) Principal(role string) string {
	switch role {
	case RoleAdmin:
		return PrincipalAdmin
	case RoleWriter:
		return PrincipalWriter
	case RoleReader:
		return PrincipalReader
	}
	return ""
}

// Request sends method path (relative to URL) with role's API key,
// when it has one, and returns the response. The caller closes the body.
func (s *Server) Request(t testing.TB, method, path, role string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, s.URL+path, body)
	if err != nil {
		t.Fatalf("dpkmstest: %v", err)
	}
	if tok := s.Token(role); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dpkmstest: %s %s: %v", method, path, err)
	}
	return resp
}

type options struct {
	keys        bool
	methods     []authn.Method
	unreachable bool
	embedClient *http.Client
	upgrade     *upgrade.Manager
}

// Option configures Start.
type Option func(*options)

// WithKeys makes the instance protected (server.access: protected)
// behind key-backed principals: the seeded owner (admin) and two
// principals created in the driver's registry, PrincipalWriter (a
// device, writer) and PrincipalReader (a person, reader), each with one
// API key. Server.Token returns each key.
func WithKeys() Option {
	return func(o *options) { o.keys = true }
}

// WithAuthMethods makes the instance protected behind a chain of
// methods, in place of the configured one. It is the seam for tests
// that need another authenticator. Server.Token returns "" for every
// role; the test owns the credentials. The owner still gets a key in
// the registry, since a protected instance refuses to start without
// one.
func WithAuthMethods(methods ...authn.Method) Option {
	return func(o *options) { o.methods = methods }
}

// WithEmbeddingHTTPClient carries the instance's embedding requests over c,
// typically a providertest cassette client. A registered model then
// resolves to its own provider (the env layer no longer pins the stub
// backend); its endpoint stays pinned to a closed port, so only c can
// answer.
func WithEmbeddingHTTPClient(c *http.Client) Option {
	return func(o *options) { o.embedClient = c }
}

// WithUpgrade reports m's state as the instance's upgrade state: on
// /healthz and in the upgrade header on /api/v1 responses. Tests drive m
// (Start, Tick, Fail, Complete) to put the instance mid-upgrade.
func WithUpgrade(m *upgrade.Manager) Option {
	return func(o *options) { o.upgrade = m }
}

// Unreachable returns a Server whose URL points at a closed port: no
// stack is built and every request fails with connection refused. For
// tests of a client that cannot reach its instance.
func Unreachable() Option {
	return func(o *options) { o.unreachable = true }
}

// Start builds the dpkms stack over driver and serves it until the test
// ends.
func Start(t testing.TB, driver storage.StorageDriver, opts ...Option) *Server {
	t.Helper()
	if testguard.Active == nil {
		t.Fatal("dpkmstest: the test package must run through testguard: add " +
			"func TestMain(m *testing.M) { os.Exit(testguard.Main(m, nil, \"ctxt\")) }")
	}
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.keys && o.methods != nil {
		t.Fatal("dpkmstest: WithKeys and WithAuthMethods are exclusive")
	}
	if o.unreachable {
		return &Server{URL: testguard.ClosedServerURL, Driver: driver}
	}

	dir := t.TempDir()
	srv := &Server{Driver: driver, tokens: map[string]string{}}
	protected := o.keys || o.methods != nil

	// The listener comes first so the config can name the instance's
	// own URL as server.public_url, the base of its sign-in links.
	hs := httptest.NewUnstartedServer(nil)
	t.Cleanup(hs.Close)
	srv.URL = "http://" + hs.Listener.Addr().String()

	if protected {
		tokens := mintKeys(t, driver)
		if o.keys {
			srv.tokens = tokens
		}
	}
	cfgPath := writeConfig(t, dir, protected, srv.URL)
	cfg, err := config.Load("dpkms", cfgPath)
	if err != nil {
		t.Fatalf("dpkmstest: load config: %v", err)
	}
	srv.Config = cfg

	access, err := stack.ResolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("dpkmstest: %v", err)
	}
	var chain *authn.Chain
	if o.methods != nil {
		chain = authn.NewChain(o.methods...)
	}

	probes := httpserver.HealthzProbes{Version: "dpkmstest", Started: time.Now()}
	if m := o.upgrade; m != nil {
		probes.Upgrade = func(context.Context) *httpserver.UpgradeSnapshot {
			return httpserver.NewUpgradeSnapshot(m.Snapshot())
		}
	}

	bus := kitbus.New()
	t.Cleanup(func() { _ = bus.Close(context.Background()) })
	st, err := stack.Build(stack.Inputs{
		Config:     cfg,
		Driver:     driver,
		Access:     access,
		Auth:       chain,
		PolicyBus:  bus,
		Embeddings: hermeticEmbeddings(cfg, o.embedClient),
		StepsPath:  filepath.Join(dir, "steps"),
		ConfigPath: cfgPath,
		Probes:     probes,
		Warnings:   io.Discard,
		// A test instance refuses a response the backstop would strip:
		// a leak is a failure, never a quietly cleaned body.
		StrictSharing: true,
	})
	if err != nil {
		t.Fatalf("dpkmstest: build stack: %v", err)
	}
	t.Cleanup(st.Close)
	srv.Stack = st
	// As dpkms serve does: every daemon store carries the default registry.
	if err := st.Service.EnsureDefaultRegistry(sharing.WithViewer(context.Background(), sharing.Unrestricted)); err != nil {
		t.Fatalf("dpkmstest: seed default registry: %v", err)
	}

	hs.Config.Handler = st.Router
	hs.Start()
	return srv
}

// EverythingPolicy is the share policy WithKeys grants the writer and
// reader principals (sharingtest.EverythingPolicy): every object, every
// capability. A non-owner principal sees nothing without a grant
// (ADR-080 §2), and tests of the API's shape as a reader or writer want
// the data, not the sharing rules; tests of the sharing rules build
// their own principals.
const EverythingPolicy = sharingtest.EverythingPolicy

// mintKeys creates the writer and reader principals in driver's
// registry (the owner is seeded), grants them EverythingPolicy and
// mints one key per role.
func mintKeys(t testing.TB, driver storage.StorageDriver) map[string]string {
	t.Helper()
	ctx := context.Background()
	for _, p := range []*storage.Principal{
		{ID: PrincipalWriter, Kind: storage.PrincipalKindDevice, DisplayName: "Test writer", Roles: []string{RoleWriter}},
		{ID: PrincipalReader, Kind: storage.PrincipalKindPerson, DisplayName: "Test reader", Roles: []string{RoleReader}},
	} {
		if err := driver.Auth().Principals().Create(ctx, p); err != nil {
			t.Fatalf("dpkmstest: create principal %s: %v", p.ID, err)
		}
	}
	sharingtest.GrantEverything(t, driver, PrincipalWriter, PrincipalReader)
	keys := authn.NewKeysOver(driver.Auth(), nil)
	tokens := map[string]string{}
	for role, principal := range map[string]string{RoleAdmin: PrincipalAdmin, RoleWriter: PrincipalWriter, RoleReader: PrincipalReader} {
		secret, _, err := keys.Mint(ctx, authn.MintKey{PrincipalID: principal, Name: "dpkmstest " + role})
		if err != nil {
			t.Fatalf("dpkmstest: mint %s key: %v", role, err)
		}
		tokens[role] = secret
	}
	return tokens
}

// providerKeys are the providers.* sections pinned to their stub
// backend. "auto" would probe a local Ollama and pick up API keys from
// the environment while the pipeline registry is built.
var providerKeys = []string{"video", "document", "ocr", "transcription", "vision", "diarization", "llm"}

// writeConfig writes the instance's dpkms config and returns its path.
// A protected instance names publicURL as its server.public_url.
func writeConfig(t testing.TB, dir string, protected bool, publicURL string) string {
	t.Helper()
	providers := map[string]any{}
	for _, k := range providerKeys {
		providers[k] = map[string]any{"backend": "stub"}
	}
	server := map[string]any{"access": config.AccessPrivate}
	if protected {
		server = map[string]any{"access": config.AccessProtected, "public_url": publicURL}
	}
	body, err := yaml.Marshal(map[string]any{
		"server":    server,
		"providers": providers,
	})
	if err != nil {
		t.Fatalf("dpkmstest: marshal config: %v", err)
	}
	path := filepath.Join(dir, "dpkms.yaml")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("dpkmstest: write config: %v", err)
	}
	return path
}

// hermeticEmbeddings resolves the embedding provider from the config
// alone, with the env layer pinned to a closed-port endpoint. Without
// client it also pins the stub backend: the registry layer (a model's
// recorded provider) sits below env, so no ingest or query embedding can
// reach a real model server. With client, a registered model keeps its
// own backend and every request goes through client.
func hermeticEmbeddings(cfg *config.Config, client *http.Client) *embeddings.Resolver {
	env := map[string]string{embeddings.EnvEndpoint: testguard.ClosedServerURL}
	if client == nil {
		env[embeddings.EnvProvider] = embeddings.BackendStub
	}
	return &embeddings.Resolver{
		Config:     cfg.Providers.Embedding,
		LookupEnv:  func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		HTTPClient: client,
	}
}

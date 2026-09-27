// Package dpkmstest runs the dpkms API in process for tests: the router
// and service dpkms serve builds (internal/server/stack), behind an
// httptest server on 127.0.0.1, over the caller's storage driver.
//
// Tests seed through the driver and exercise commands or clients over
// HTTP against the same API an operator's daemon serves:
//
//	driver := storageutil.NewTestDriver(t)
//	srv := dpkmstest.Start(t, driver, dpkmstest.WithStaticTokens())
//	// srv.URL, srv.Token(dpkmstest.RoleReader)
//
// The package that calls Start must run its tests through
// internal/testguard (a TestMain calling testguard.Main); Start fails
// the test otherwise.
//
// The instance is hermetic: every provider backend is the stub and the
// embedding resolver sees only a stub provider on a closed port, so
// nothing reaches a local model server. No worker pool runs: enqueued
// jobs stay pending. Import it from test files only.
package dpkmstest

import (
	"context"
	"crypto/rand"
	"fmt"
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
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// Roles the static test tokens grant. They are plain role strings on the
// principal; what each may do is the server's authorization decision.
const (
	RoleAdmin  = "admin"
	RoleWriter = "writer"
	RoleReader = "reader"
)

// Roles lists every role WithStaticTokens issues a token for.
var Roles = []string{RoleAdmin, RoleWriter, RoleReader}

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

// Token returns the bearer token for role, or "" when the instance
// issues none for it (private or Unreachable instances, custom
// providers).
func (s *Server) Token(role string) string { return s.tokens[role] }

// Request sends method path (relative to URL) with role's bearer token,
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
	tokens      bool
	provider    authn.Provider
	unreachable bool
}

// Option configures Start.
type Option func(*options)

// WithStaticTokens makes the instance protected (server.access:
// protected) behind the static token provider, with one principal per
// role in Roles ("test-admin", "test-writer", "test-reader"). Server.Token
// returns each token.
func WithStaticTokens() Option {
	return func(o *options) { o.tokens = true }
}

// WithAuthProvider makes the instance protected behind p. It is the
// seam for tests that need a provider other than the static table.
// Server.Token returns "" for every role; the test owns the credentials.
func WithAuthProvider(p authn.Provider) Option {
	return func(o *options) { o.provider = p }
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
	if o.tokens && o.provider != nil {
		t.Fatal("dpkmstest: WithStaticTokens and WithAuthProvider are exclusive")
	}
	if o.unreachable {
		return &Server{URL: testguard.ClosedServerURL, Driver: driver}
	}

	dir := t.TempDir()
	srv := &Server{Driver: driver, tokens: map[string]string{}}
	if o.tokens {
		for _, role := range Roles {
			srv.tokens[role] = "dpkmstest-" + role + "-" + randomHex(t)
		}
	}
	cfgPath := writeConfig(t, dir, o.provider != nil, srv.tokens)
	cfg, err := config.Load("dpkms", cfgPath)
	if err != nil {
		t.Fatalf("dpkmstest: load config: %v", err)
	}
	srv.Config = cfg

	access, provider, err := stack.ResolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("dpkmstest: %v", err)
	}
	if o.provider != nil {
		provider = o.provider
	}

	bus := kitbus.New()
	t.Cleanup(func() { _ = bus.Close(context.Background()) })
	st, err := stack.Build(stack.Inputs{
		Config:     cfg,
		Driver:     driver,
		Access:     access,
		Auth:       provider,
		PolicyBus:  bus,
		Embeddings: hermeticEmbeddings(cfg),
		StepsPath:  filepath.Join(dir, "steps"),
		ConfigPath: cfgPath,
		Probes:     httpserver.HealthzProbes{Version: "dpkmstest", Started: time.Now()},
		Warnings:   io.Discard,
	})
	if err != nil {
		t.Fatalf("dpkmstest: build stack: %v", err)
	}
	t.Cleanup(st.Close)
	srv.Stack = st

	hs := httptest.NewServer(st.Router)
	t.Cleanup(hs.Close)
	srv.URL = hs.URL
	return srv
}

// providerKeys are the providers.* sections pinned to their stub
// backend. "auto" would probe a local Ollama and pick up API keys from
// the environment while the pipeline registry is built.
var providerKeys = []string{"video", "document", "ocr", "transcription", "vision", "diarization", "llm"}

// writeConfig writes the instance's dpkms config and returns its path.
// With tokens (or an injected provider) the instance is protected.
func writeConfig(t testing.TB, dir string, customAuth bool, tokens map[string]string) string {
	t.Helper()
	providers := map[string]any{}
	for _, k := range providerKeys {
		providers[k] = map[string]any{"backend": "stub"}
	}
	server := map[string]any{"access": config.AccessPrivate}
	if len(tokens) > 0 {
		entries := make([]map[string]any, 0, len(tokens))
		for _, role := range Roles {
			entries = append(entries, map[string]any{
				"token": tokens[role], "principal": "test-" + role, "roles": []string{role},
			})
		}
		server = map[string]any{
			"access": config.AccessProtected,
			"auth":   map[string]any{"provider": authn.ProviderStatic, "static": map[string]any{"tokens": entries}},
		}
	} else if customAuth {
		// ResolveInboundAuth needs credentials to accept a protected
		// class; the injected provider replaces this placeholder.
		server = map[string]any{
			"access": config.AccessProtected,
			"auth": map[string]any{"provider": authn.ProviderStatic, "static": map[string]any{"tokens": []map[string]any{
				{"token": "dpkmstest-unused-" + randomHex(t), "principal": "dpkmstest-unused"},
			}}},
		}
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
// alone, with the env layer pinned to a stub backend on a closed port:
// the registry layer (a model's recorded provider) sits below env, so no
// ingest or query embedding can reach a real model server.
func hermeticEmbeddings(cfg *config.Config) *embeddings.Resolver {
	env := map[string]string{
		embeddings.EnvProvider: embeddings.BackendStub,
		embeddings.EnvEndpoint: testguard.ClosedServerURL,
	}
	return &embeddings.Resolver{
		Config:    cfg.Providers.Embedding,
		LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok },
	}
}

func randomHex(t testing.TB) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("dpkmstest: random token: %v", err)
	}
	return fmt.Sprintf("%x", b)
}

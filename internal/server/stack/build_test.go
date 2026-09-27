package stack

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	kitbus "hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil, "dpkms"))
}

// build assembles a stack over a fresh driver for cfg, resolving access
// and auth the way dpkms serve does. Every provider backend is pinned to
// its stub: "auto" would probe a local Ollama while building the
// pipeline registry.
func build(t *testing.T, cfg *config.Config) *Stack {
	t.Helper()
	stubProviders(cfg)
	access, provider, err := ResolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("ResolveInboundAuth: %v", err)
	}
	bus := kitbus.New()
	st, err := Build(Inputs{
		Config:    cfg,
		Driver:    storageutil.NewTestDriver(t),
		Access:    access,
		Auth:      provider,
		PolicyBus: bus,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

// stubProviders pins every provider backend of cfg to its stub and
// returns cfg.
func stubProviders(cfg *config.Config) *config.Config {
	for _, b := range []*config.ProviderBackendConfig{
		&cfg.Providers.Video, &cfg.Providers.Document, &cfg.Providers.OCR, &cfg.Providers.Transcription,
		&cfg.Providers.Vision, &cfg.Providers.Diarization, &cfg.Providers.LLM,
	} {
		b.Backend = "stub"
	}
	return cfg
}

func status(t *testing.T, st *Stack, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/objects", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	st.Router.ServeHTTP(rec, req)
	return rec.Code
}

// TestBuildProtectedGuardsRouteTable: a protected stack wires its provider
// into the router, the security emitter and the entitlement gate, so the
// API refuses a caller without a valid token.
func TestBuildProtectedGuardsRouteTable(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Access = config.AccessProtected
	cfg.Server.Auth = staticAuthCfg()
	st := build(t, cfg)

	if st.RouteAuth == nil || st.Entitlements == nil || st.Security == nil {
		t.Fatalf("protected stack: RouteAuth=%v Entitlements=%v Security=%v; want all wired",
			st.RouteAuth, st.Entitlements, st.Security)
	}
	if got := status(t, st, ""); got != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", got)
	}
	if got := status(t, st, "wrong"); got != http.StatusUnauthorized {
		t.Errorf("wrong token: status %d, want 401", got)
	}
	if got := status(t, st, "tok-1"); got != http.StatusOK {
		t.Errorf("valid token: status %d, want 200", got)
	}
}

// TestBuildPrivateLeavesRouteTableOpen: a private stack serves without a
// token and constructs no entitlement gate, even with auth configured.
func TestBuildPrivateLeavesRouteTableOpen(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Auth = staticAuthCfg()
	st := build(t, cfg)

	if st.RouteAuth != nil || st.Entitlements != nil {
		t.Fatalf("private stack: RouteAuth=%v Entitlements=%v; want nil", st.RouteAuth, st.Entitlements)
	}
	if got := status(t, st, ""); got != http.StatusOK {
		t.Errorf("no token: status %d, want 200", got)
	}
}

func TestBuildRequiresInputs(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	for name, in := range map[string]Inputs{
		"config":     {Driver: driver, PolicyBus: kitbus.New()},
		"driver":     {Config: &config.Config{}, PolicyBus: kitbus.New()},
		"policy bus": {Config: &config.Config{}, Driver: driver},
	} {
		if _, err := Build(in); err == nil {
			t.Errorf("Build without %s: want error", name)
		}
	}
}

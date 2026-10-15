package stack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	kitbus "hop.top/kit/go/runtime/bus"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil, "dpkms"))
}

// build assembles a stack over driver for cfg, resolving access and
// auth the way dpkms serve does. Every provider backend is pinned to
// its stub: "auto" would probe a local Ollama while building the
// pipeline registry.
func build(t *testing.T, cfg *config.Config, driver storage.StorageDriver) *Stack {
	t.Helper()
	st, err := tryBuild(t, cfg, driver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return st
}

// tryBuild is build that returns Build's error.
func tryBuild(t *testing.T, cfg *config.Config, driver storage.StorageDriver) (*Stack, error) {
	t.Helper()
	stubProviders(cfg)
	access, err := ResolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("ResolveInboundAuth: %v", err)
	}
	bus := kitbus.New()
	st, err := Build(Inputs{
		Config:    cfg,
		Driver:    driver,
		Access:    access,
		PolicyBus: bus,
	})
	if err != nil {
		return nil, err
	}
	t.Cleanup(st.Close)
	return st, nil
}

// ownerKey mints an API key for the owner in driver's registry.
func ownerKey(t *testing.T, driver storage.StorageDriver) string {
	t.Helper()
	secret, _, err := authn.NewKeysOver(driver.Auth(), nil).Mint(context.Background(), authn.MintKey{PrincipalID: storage.OwnerPrincipal})
	if err != nil {
		t.Fatalf("mint owner key: %v", err)
	}
	return secret
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

// TestBuildProtectedGuardsRouteTable: a protected stack wires its chain
// into the router, the security emitter and the entitlement gate, so
// the API refuses a caller without a valid key and admits the owner's.
func TestBuildProtectedGuardsRouteTable(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	key := ownerKey(t, driver)
	st := build(t, protectedCfg(), driver)

	if st.RouteAuth == nil || st.Keys == nil || st.UISessions == nil || st.Entitlements == nil || st.Security == nil {
		t.Fatalf("protected stack: RouteAuth=%v Keys=%v UISessions=%v Entitlements=%v Security=%v; want all wired",
			st.RouteAuth, st.Keys, st.UISessions, st.Entitlements, st.Security)
	}
	if got := st.RouteAuth.Methods(); len(got) != 2 || got[0] != authn.MethodAPIKey || got[1] != authn.MethodSession {
		t.Errorf("chain = %v, want [api_key session]", got)
	}
	if got := status(t, st, ""); got != http.StatusUnauthorized {
		t.Errorf("no key: status %d, want 401", got)
	}
	if got := status(t, st, "wrong"); got != http.StatusUnauthorized {
		t.Errorf("wrong key: status %d, want 401", got)
	}
	if got := status(t, st, key); got != http.StatusOK {
		t.Errorf("owner key: status %d, want 200", got)
	}
}

// TestBuildProtectedRefusesWithoutOwnerCredential: a protected stack
// over a registry whose owner holds no live key refuses to build and
// names the command that creates one (ADR-079 §4); a revoked or expired
// key is no credential; a server.auth.keys entry for the owner is.
func TestBuildProtectedRefusesWithoutOwnerCredential(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	_, err := tryBuild(t, protectedCfg(), driver)
	if err == nil || !strings.Contains(err.Error(), "dpkms auth key create --principal owner") {
		t.Fatalf("Build without an owner key: %v; want a refusal naming the command", err)
	}

	keys := authn.NewKeysOver(driver.Auth(), nil)
	_, issued, err := keys.Mint(context.Background(), authn.MintKey{PrincipalID: storage.OwnerPrincipal})
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.Revoke(context.Background(), issued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tryBuild(t, protectedCfg(), driver); err == nil {
		t.Fatal("Build with only a revoked owner key: want a refusal")
	}
	if _, _, err := keys.Mint(context.Background(), authn.MintKey{PrincipalID: storage.OwnerPrincipal, ExpiresAt: time.Now().Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := tryBuild(t, protectedCfg(), driver); err == nil {
		t.Fatal("Build with only an expired owner key: want a refusal")
	}

	cfg := protectedCfg()
	const cfgKey = "config-owner-key-0123456789abcdef0123456789"
	cfg.Server.Auth.Keys = []config.AuthKeyConfig{{Hash: "sha256:" + authn.HashSecret(cfgKey), Principal: storage.OwnerPrincipal}}
	st := build(t, cfg, driver)
	if got := status(t, st, cfgKey); got != http.StatusOK {
		t.Errorf("config-declared owner key: status %d, want 200", got)
	}
	if got := status(t, st, "wrong"); got != http.StatusUnauthorized {
		t.Errorf("wrong key: status %d, want 401", got)
	}
}

// TestBuildMethodsFollowConfig: a methods list without session builds
// no session manager and a chain of api_key alone.
func TestBuildMethodsFollowConfig(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ownerKey(t, driver)
	cfg := protectedCfg()
	cfg.Server.Auth.Methods = []string{authn.MethodAPIKey}
	st := build(t, cfg, driver)
	if st.UISessions != nil {
		t.Error("session manager built without the session method")
	}
	if got := st.RouteAuth.Methods(); len(got) != 1 || got[0] != authn.MethodAPIKey {
		t.Errorf("chain = %v, want [api_key]", got)
	}
}

// TestBuildPrivateLeavesRouteTableOpen: a private stack serves without a
// key and constructs no chain, registry keys or entitlement gate.
func TestBuildPrivateLeavesRouteTableOpen(t *testing.T) {
	st := build(t, &config.Config{}, storageutil.NewTestDriver(t))

	if st.RouteAuth != nil || st.Keys != nil || st.UISessions != nil || st.Entitlements != nil {
		t.Fatalf("private stack: RouteAuth=%v Keys=%v UISessions=%v Entitlements=%v; want nil",
			st.RouteAuth, st.Keys, st.UISessions, st.Entitlements)
	}
	if got := status(t, st, ""); got != http.StatusOK {
		t.Errorf("no key: status %d, want 200", got)
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

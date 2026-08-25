package cmd

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func staticAuthCfg() config.AuthConfig {
	return config.AuthConfig{
		Provider: "static",
		Static: config.StaticAuthConfig{
			Tokens: []config.StaticTokenConfig{{Token: "tok-1", Principal: "ops"}},
		},
	}
}

func TestResolveInboundAuthPrivateDefault(t *testing.T) {
	cfg := &config.Config{}
	access, provider, err := resolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("resolveInboundAuth: %v", err)
	}
	if access != config.AccessPrivate {
		t.Errorf("access = %q, want private", access)
	}
	if provider != nil {
		t.Errorf("expected no provider on private instance, got %T", provider)
	}
}

func TestResolveInboundAuthProtectedWithoutCredentialsRefuses(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Access = config.AccessProtected
	if _, _, err := resolveInboundAuth(cfg, false); err == nil {
		t.Fatal("expected refusal for protected instance without credentials")
	}
}

func TestResolveInboundAuthPublicFlagWithoutCredentialsRefuses(t *testing.T) {
	cfg := &config.Config{}
	if _, _, err := resolveInboundAuth(cfg, true); err == nil {
		t.Fatal("expected refusal for --public without credentials")
	}
}

func TestResolveInboundAuthExplicitPrivatePlusPublicFlagRefuses(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Access = config.AccessPrivate
	cfg.Server.Auth = staticAuthCfg()
	if _, _, err := resolveInboundAuth(cfg, true); err == nil {
		t.Fatal("expected hard error for access: private + --public")
	}
}

func TestResolveInboundAuthProtectedWithCredentials(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Access = config.AccessProtected
	cfg.Server.Auth = staticAuthCfg()
	access, provider, err := resolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("resolveInboundAuth: %v", err)
	}
	if access != config.AccessProtected {
		t.Errorf("access = %q, want protected", access)
	}
	if provider == nil {
		t.Fatal("expected provider for protected instance")
	}
}

func TestResolveInboundAuthPublicShorthandWithCredentials(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Auth = staticAuthCfg()
	access, provider, err := resolveInboundAuth(cfg, true)
	if err != nil {
		t.Fatalf("resolveInboundAuth: %v", err)
	}
	if access != config.AccessPublic {
		t.Errorf("access = %q, want public", access)
	}
	if provider == nil {
		t.Fatal("expected provider for public instance")
	}
}

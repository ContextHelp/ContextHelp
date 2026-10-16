package stack

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// protectedCfg is a protected instance's config with the one key a
// link-minting instance must set.
func protectedCfg() *config.Config {
	cfg := &config.Config{}
	cfg.Server.Access = config.AccessProtected
	cfg.Server.PublicURL = "https://dpkms.example.net"
	return cfg
}

func TestResolveInboundAuthPrivateDefault(t *testing.T) {
	cfg := &config.Config{}
	access, err := ResolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("resolveInboundAuth: %v", err)
	}
	if access != config.AccessPrivate {
		t.Errorf("access = %q, want private", access)
	}
}

// A protected instance mints links by default, so it needs
// server.public_url; the credential itself is the registry's business.
func TestResolveInboundAuthProtectedWithoutPublicURLRefuses(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Access = config.AccessProtected
	if _, err := ResolveInboundAuth(cfg, false); err == nil {
		t.Fatal("expected refusal for protected instance without public_url")
	}
	if _, err := ResolveInboundAuth(&config.Config{}, true); err == nil {
		t.Fatal("expected refusal for --public without public_url")
	}
	cfg.Server.Auth.SignIn = []string{}
	if _, err := ResolveInboundAuth(cfg, false); err != nil {
		t.Fatalf("protected minting no links: %v", err)
	}
}

func TestResolveInboundAuthExplicitPrivatePlusPublicFlagRefuses(t *testing.T) {
	cfg := protectedCfg()
	cfg.Server.Access = config.AccessPrivate
	if _, err := ResolveInboundAuth(cfg, true); err == nil {
		t.Fatal("expected hard error for access: private + --public")
	}
}

func TestResolveInboundAuthBrokenAuthRefuses(t *testing.T) {
	cfg := protectedCfg()
	cfg.Server.Auth.Keys = []config.AuthKeyConfig{{Hash: "ctxt_plain_key", Principal: "owner"}}
	if _, err := ResolveInboundAuth(cfg, false); err == nil {
		t.Fatal("expected refusal for a plaintext key in server.auth.keys")
	}
}

func TestResolveInboundAuthProtectedAndPublic(t *testing.T) {
	access, err := ResolveInboundAuth(protectedCfg(), false)
	if err != nil {
		t.Fatalf("resolveInboundAuth: %v", err)
	}
	if access != config.AccessProtected {
		t.Errorf("access = %q, want protected", access)
	}
	cfg := protectedCfg()
	cfg.Server.Access = ""
	access, err = ResolveInboundAuth(cfg, true)
	if err != nil {
		t.Fatalf("resolveInboundAuth: %v", err)
	}
	if access != config.AccessPublic {
		t.Errorf("access = %q, want public", access)
	}
}

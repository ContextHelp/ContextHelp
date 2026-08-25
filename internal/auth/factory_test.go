package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestFromConfigEmptyProviderIsNil(t *testing.T) {
	p, err := FromConfig(config.AuthConfig{})
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if p != nil {
		t.Fatalf("expected nil provider for empty config, got %T", p)
	}
}

func TestFromConfigStatic(t *testing.T) {
	p, err := FromConfig(config.AuthConfig{
		Provider: "static",
		Static: config.StaticAuthConfig{
			Tokens: []config.StaticTokenConfig{
				{Token: "tok-1", Principal: "ops", Roles: []string{"admin"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider")
	}
	if p.Name() != ProviderStatic {
		t.Errorf("Name = %q, want %q", p.Name(), ProviderStatic)
	}
	princ, err := p.Authenticate(context.Background(), Credential{Token: "tok-1"})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if princ.ID != "ops" {
		t.Errorf("principal = %q, want ops", princ.ID)
	}
}

func TestFromConfigStaticWithoutTokens(t *testing.T) {
	_, err := FromConfig(config.AuthConfig{Provider: "static"})
	if err == nil {
		t.Fatal("expected error for static provider with no tokens")
	}
}

func TestFromConfigReservedProviders(t *testing.T) {
	for _, name := range []string{ProviderOIDC, ProviderMTLS} {
		_, err := FromConfig(config.AuthConfig{Provider: name})
		if err == nil {
			t.Fatalf("provider %q: expected not-implemented error", name)
		}
		if !strings.Contains(err.Error(), "not yet implemented") {
			t.Errorf("provider %q: err = %v, want not-yet-implemented", name, err)
		}
	}
}

func TestFromConfigUnknownProvider(t *testing.T) {
	_, err := FromConfig(config.AuthConfig{Provider: "kerberos"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

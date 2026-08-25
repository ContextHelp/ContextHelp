package policy

import (
	"context"
	"testing"

	kitpolicy "hop.top/kit/go/runtime/policy"
)

// Private instances keep kit's default resolution: ctx principal, then
// $KIT_POLICY_ROLE/$USER env fallback.
func TestResolverEnvFallbackAllowedOnPrivate(t *testing.T) {
	t.Setenv("KIT_POLICY_ROLE", "admin")
	t.Setenv("USER", "localuser")

	p := ctxtPrincipalResolver(false)(context.Background())
	if p.Role != "admin" {
		t.Errorf("role = %q, want admin (env fallback)", p.Role)
	}
	if p.ID != "localuser" {
		t.Errorf("id = %q, want localuser (env fallback)", p.ID)
	}
}

// Non-private instances refuse the env fallback: with no context
// principal the resolver returns an anonymous principal — a remote
// caller must never inherit the daemon operator's $USER or
// $KIT_POLICY_ROLE identity.
func TestResolverRefusesEnvFallbackOnNonPrivate(t *testing.T) {
	t.Setenv("KIT_POLICY_ROLE", "admin")
	t.Setenv("USER", "localuser")

	p := ctxtPrincipalResolver(true)(context.Background())
	if p.ID != "" || p.Role != "" {
		t.Fatalf("env fallback must be refused, got %+v", p)
	}
	if p.Source != "none" {
		t.Errorf("source = %q, want none", p.Source)
	}
}

// The authenticated context principal always wins, env refusal or not.
func TestResolverContextPrincipalAlwaysWins(t *testing.T) {
	t.Setenv("KIT_POLICY_ROLE", "admin")
	ctx := context.WithValue(context.Background(), kitpolicy.ContextPrincipalKey,
		kitpolicy.Principal{ID: "ops", Role: "reader", Source: "static"})

	for _, refuseEnv := range []bool{false, true} {
		p := ctxtPrincipalResolver(refuseEnv)(ctx)
		if p.ID != "ops" || p.Role != "reader" {
			t.Errorf("refuseEnv=%v: principal = %+v, want ctx principal ops/reader", refuseEnv, p)
		}
	}
}

package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Fake tokens long enough to pass the static provider's strength check.
const (
	tok1     = "tok-1-0000000000000000000000000000"
	tokAdmin = "tok-admin-0000000000000000000000000"
	tokRead  = "tok-read-00000000000000000000000000"
)

func TestNewStaticRejectsEmptyToken(t *testing.T) {
	_, err := NewStatic([]StaticToken{{Token: "", Principal: "ci", Roles: []string{RoleReader}}})
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestNewStaticRejectsEmptyPrincipal(t *testing.T) {
	_, err := NewStatic([]StaticToken{{Token: tok1, Principal: "", Roles: []string{RoleReader}}})
	if err == nil {
		t.Fatal("expected error for empty principal")
	}
}

func TestNewStaticRejectsDuplicateToken(t *testing.T) {
	_, err := NewStatic([]StaticToken{
		{Token: tok1, Principal: "a", Roles: []string{RoleReader}},
		{Token: tok1, Principal: "b", Roles: []string{RoleReader}},
	})
	if err == nil {
		t.Fatal("expected error for duplicate token")
	}
}

func TestStaticAuthenticateValid(t *testing.T) {
	p, err := NewStatic([]StaticToken{
		{Token: tokAdmin, Principal: "ops", Roles: []string{"admin"}},
		{Token: tokRead, Principal: "reader", Roles: []string{RoleReader}},
	})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}

	princ, err := p.Authenticate(context.Background(), Credential{Scheme: SchemeBearer, Token: tokAdmin})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if princ.ID != "ops" {
		t.Errorf("principal ID = %q, want %q", princ.ID, "ops")
	}
	if princ.Provider != ProviderStatic {
		t.Errorf("principal Provider = %q, want %q", princ.Provider, ProviderStatic)
	}
	if !princ.HasRole("admin") {
		t.Error("expected admin role")
	}
	if princ.HasRole("root") {
		t.Error("unexpected root role")
	}
}

func TestStaticAuthenticateMissingCredential(t *testing.T) {
	p, err := NewStatic([]StaticToken{{Token: tok1, Principal: "a", Roles: []string{RoleReader}}})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	_, err = p.Authenticate(context.Background(), Credential{})
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
}

func TestStaticAuthenticateInvalidToken(t *testing.T) {
	p, err := NewStatic([]StaticToken{{Token: tok1, Principal: "a", Roles: []string{RoleReader}}})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	_, err = p.Authenticate(context.Background(), Credential{Scheme: SchemeBearer, Token: "wrong"})
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("err = %v, want ErrInvalidCredential", err)
	}
}

func TestStaticAuthenticateCopiesRoles(t *testing.T) {
	roles := []string{"admin"}
	p, err := NewStatic([]StaticToken{{Token: tok1, Principal: "a", Roles: roles}})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	princ, err := p.Authenticate(context.Background(), Credential{Token: tok1})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	princ.Roles[0] = "mutated"

	again, err := p.Authenticate(context.Background(), Credential{Token: tok1})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if again.Roles[0] != "admin" {
		t.Errorf("provider state mutated through returned principal: %q", again.Roles[0])
	}

	princ.Scopes[0] = "mutated"
	again, err = p.Authenticate(context.Background(), Credential{Token: tok1})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if again.Scopes[0] != AllScopes[0] {
		t.Errorf("provider scopes mutated through returned principal: %q", again.Scopes[0])
	}
}

func TestPrincipalContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if _, ok := FromContext(ctx); ok {
		t.Fatal("empty context should carry no principal")
	}
	want := &Principal{ID: "ops", Provider: ProviderStatic}
	ctx = WithPrincipal(ctx, want)
	got, ok := FromContext(ctx)
	if !ok || got.ID != "ops" {
		t.Fatalf("FromContext = %+v, %v; want principal ops", got, ok)
	}
}

// The example config's placeholder and short tokens are public or
// guessable: serve must refuse them, naming the entry to fix.
func TestNewStaticRejectsWeakTokens(t *testing.T) {
	for name, tok := range map[string]string{
		"example placeholder":     "REPLACE_ME_admin_token",
		"placeholder, lower case": "replace_me_" + strings.Repeat("0", 40),
		"short":                   "tok-1",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewStatic([]StaticToken{
				{Token: tok1, Principal: "a", Roles: []string{RoleReader}},
				{Token: tok, Principal: "b", Roles: []string{RoleReader}},
			})
			if err == nil {
				t.Fatalf("NewStatic accepted %q", tok)
			}
			for _, want := range []string{"[1]", "openssl rand -hex 32"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

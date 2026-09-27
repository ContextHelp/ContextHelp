package auth

import (
	"context"
	"errors"
	"testing"
)

func TestNewStaticRejectsEmptyToken(t *testing.T) {
	_, err := NewStatic([]StaticToken{{Token: "", Principal: "ci", Roles: []string{RoleReader}}})
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestNewStaticRejectsEmptyPrincipal(t *testing.T) {
	_, err := NewStatic([]StaticToken{{Token: "tok-1", Principal: "", Roles: []string{RoleReader}}})
	if err == nil {
		t.Fatal("expected error for empty principal")
	}
}

func TestNewStaticRejectsDuplicateToken(t *testing.T) {
	_, err := NewStatic([]StaticToken{
		{Token: "tok-1", Principal: "a", Roles: []string{RoleReader}},
		{Token: "tok-1", Principal: "b", Roles: []string{RoleReader}},
	})
	if err == nil {
		t.Fatal("expected error for duplicate token")
	}
}

func TestStaticAuthenticateValid(t *testing.T) {
	p, err := NewStatic([]StaticToken{
		{Token: "tok-admin", Principal: "ops", Roles: []string{"admin"}},
		{Token: "tok-read", Principal: "reader", Roles: []string{RoleReader}},
	})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}

	princ, err := p.Authenticate(context.Background(), Credential{Scheme: SchemeBearer, Token: "tok-admin"})
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
	p, err := NewStatic([]StaticToken{{Token: "tok-1", Principal: "a", Roles: []string{RoleReader}}})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	_, err = p.Authenticate(context.Background(), Credential{})
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
}

func TestStaticAuthenticateInvalidToken(t *testing.T) {
	p, err := NewStatic([]StaticToken{{Token: "tok-1", Principal: "a", Roles: []string{RoleReader}}})
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
	p, err := NewStatic([]StaticToken{{Token: "tok-1", Principal: "a", Roles: roles}})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	princ, err := p.Authenticate(context.Background(), Credential{Token: "tok-1"})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	princ.Roles[0] = "mutated"

	again, err := p.Authenticate(context.Background(), Credential{Token: "tok-1"})
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if again.Roles[0] != "admin" {
		t.Errorf("provider state mutated through returned principal: %q", again.Roles[0])
	}

	princ.Scopes[0] = "mutated"
	again, err = p.Authenticate(context.Background(), Credential{Token: "tok-1"})
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

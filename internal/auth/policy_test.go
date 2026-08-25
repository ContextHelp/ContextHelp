package auth

import (
	"context"
	"testing"

	kitpolicy "hop.top/kit/go/runtime/policy"
)

func TestAttachExposesPrincipalToPolicyEngine(t *testing.T) {
	ctx := Attach(context.Background(), &Principal{
		ID:       "ops",
		Provider: ProviderStatic,
		Roles:    []string{"admin", "reader"},
	})

	// Auth-side view.
	got, ok := FromContext(ctx)
	if !ok || got.ID != "ops" {
		t.Fatalf("FromContext = %+v, %v", got, ok)
	}

	// Policy-side view: kit's default resolver must see the same actor.
	kp := kitpolicy.DefaultPrincipalResolver(ctx)
	if kp.ID != "ops" {
		t.Errorf("policy principal ID = %q, want ops", kp.ID)
	}
	if kp.Role != "admin" {
		t.Errorf("policy principal Role = %q, want admin (primary role)", kp.Role)
	}
	if kp.Source != ProviderStatic {
		t.Errorf("policy principal Source = %q, want %q", kp.Source, ProviderStatic)
	}
}

func TestAttachNilPrincipalIsNoop(t *testing.T) {
	ctx := Attach(context.Background(), nil)
	if _, ok := FromContext(ctx); ok {
		t.Fatal("nil principal must not be attached")
	}
}

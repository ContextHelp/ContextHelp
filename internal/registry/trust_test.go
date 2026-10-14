package registry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func makeEntity(slug string) *storage.Entity {
	return &storage.Entity{Slug: slug, Namespace: "ai", Title: slug}
}

func TestTrustGate_Trusted_AllowsWrite(t *testing.T) {
	g := registry.NewTrustGate()
	e := makeEntity("ai.bert")

	rec, err := g.CheckWrite(context.Background(), e, "https://trusted.example.com",
		config.RegistryTrustLevelTrusted)

	if err != nil {
		t.Fatalf("trusted registry should allow write, got: %v", err)
	}
	if rec != nil {
		t.Fatal("trusted write should return nil record")
	}
}

func TestTrustGate_Untrusted_QueuesApproval(t *testing.T) {
	g := registry.NewTrustGate()
	e := makeEntity("ai.bert")

	rec, err := g.CheckWrite(context.Background(), e, "https://untrusted.example.com",
		config.RegistryTrustLevelUntrusted)

	if err == nil {
		t.Fatal("untrusted registry should return error")
	}
	if !errors.Is(err, registry.ErrEntityRequiresApproval) {
		t.Fatalf("expected ErrEntityRequiresApproval, got: %v", err)
	}
	if rec == nil {
		t.Fatal("expected approval record, got nil")
	}
	if rec.Entity.Slug != "ai.bert" {
		t.Errorf("wrong slug: got %q", rec.Entity.Slug)
	}
}

func TestTrustGate_Sandboxed_BlocksSilently(t *testing.T) {
	g := registry.NewTrustGate()
	e := makeEntity("ai.bert")

	rec, err := g.CheckWrite(context.Background(), e, "https://sandboxed.example.com",
		config.RegistryTrustLevelSandboxed)

	if err == nil {
		t.Fatal("sandboxed registry should return error")
	}
	if !errors.Is(err, registry.ErrEntityRequiresApproval) {
		t.Fatalf("expected ErrEntityRequiresApproval, got: %v", err)
	}
	if rec != nil {
		t.Fatal("sandboxed should not produce an approval record")
	}
}

func TestTrustGate_DefaultLevel_TreatsAsUntrusted(t *testing.T) {
	g := registry.NewTrustGate()
	e := makeEntity("ai.bert")

	// Empty TrustLevel — EffectiveTrustLevel should return "untrusted".
	cfg := config.RegistryConfig{URL: "https://unknown.example.com"}
	level := cfg.EffectiveTrustLevel()
	if level != config.RegistryTrustLevelUntrusted {
		t.Errorf("default level: got %q, want %q", level, config.RegistryTrustLevelUntrusted)
	}

	_, err := g.CheckWrite(context.Background(), e, cfg.URL, level)
	if !errors.Is(err, registry.ErrEntityRequiresApproval) {
		t.Fatalf("default-trust should queue for approval, got: %v", err)
	}
}

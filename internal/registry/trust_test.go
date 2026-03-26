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
	if len(g.PendingApprovals()) != 0 {
		t.Fatal("no pending approvals expected for trusted write")
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
	pending := g.PendingApprovals()
	if len(pending) != 1 {
		t.Fatalf("pending count: got %d, want 1", len(pending))
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
	// Nothing should be queued — sandboxed is a hard block.
	if len(g.PendingApprovals()) != 0 {
		t.Fatal("sandboxed should not queue for approval")
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

func TestTrustGate_Approve_RemovesFromPending(t *testing.T) {
	g := registry.NewTrustGate()
	e := makeEntity("ai.bert")
	url := "https://untrusted.example.com"

	g.CheckWrite(context.Background(), e, url, config.RegistryTrustLevelUntrusted) //nolint:errcheck

	approved, err := g.Approve(url, "ai.bert")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Entity.Slug != "ai.bert" {
		t.Errorf("wrong approved slug: %q", approved.Entity.Slug)
	}
	if len(g.PendingApprovals()) != 0 {
		t.Fatal("pending list should be empty after approval")
	}
}

func TestTrustGate_Approve_UnknownSlug_ReturnsError(t *testing.T) {
	g := registry.NewTrustGate()
	_, err := g.Approve("https://example.com", "nonexistent.slug")
	if err == nil {
		t.Fatal("expected error for unknown approval slug")
	}
}

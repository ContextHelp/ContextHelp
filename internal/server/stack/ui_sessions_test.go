package stack

import (
	"bytes"
	"context"
	"testing"
	"time"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestNewUISessions_PrivateHasNone(t *testing.T) {
	store := storageutil.NewTestDriver(t).UISessions()
	var out bytes.Buffer
	s, err := newUISessions(context.Background(), &out, store, nil, config.AccessPrivate, config.UISessionConfig{})
	if err != nil || s != nil {
		t.Fatalf("private instance: sessions = %v, err = %v; want none", s, err)
	}
}

func TestNewUISessions_ProtectedSweepsRemovedTokens(t *testing.T) {
	store := storageutil.NewTestDriver(t).UISessions()
	ctx := context.Background()
	now := time.Now().UTC()
	for id, hash := range map[string]string{"uis_gone": authn.HashSecret("tok-removed"), "uis_kept": authn.HashSecret("tok-1")} {
		if err := store.Create(ctx, &storage.UISession{
			ID: id, SecretHash: "s-" + id, PrincipalID: "ops", TokenHash: hash, Scope: authn.ScopeUI,
			CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(2 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	provider, err := authn.FromConfig(staticAuthCfg())
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s, err := newUISessions(ctx, &out, store, provider, config.AccessProtected,
		config.UISessionConfig{IdleTTL: time.Hour, MaxTTL: 24 * time.Hour})
	if err != nil || s == nil {
		t.Fatalf("protected instance: sessions = %v, err = %v", s, err)
	}
	if s.IdleTTL() != time.Hour || s.MaxTTL() != 24*time.Hour {
		t.Errorf("bounds = %s/%s, want the configured 1h/24h", s.IdleTTL(), s.MaxTTL())
	}
	gone, err := store.Get(ctx, "uis_gone")
	if err != nil {
		t.Fatal(err)
	}
	if gone.RevokeReason != authn.RevokeReasonTokenRemoved {
		t.Errorf("session of a removed token: revoke reason %q, want %q", gone.RevokeReason, authn.RevokeReasonTokenRemoved)
	}
	kept, err := store.Get(ctx, "uis_kept")
	if err != nil {
		t.Fatal(err)
	}
	if kept.RevokedAt != nil {
		t.Error("session of a configured token was revoked")
	}
	for _, want := range []string{"revoked 1 session(s)", "ctxt ui open"} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("serve output lacks %q:\n%s", want, out.String())
		}
	}
}

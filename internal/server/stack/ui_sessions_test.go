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
	driver := storageutil.NewTestDriver(t)
	var out bytes.Buffer
	s, err := newUISessions(context.Background(), &out, driver, nil, config.AccessPrivate, config.UISessionConfig{})
	if err != nil || s != nil {
		t.Fatalf("private instance: sessions = %v, err = %v; want none", s, err)
	}
}

// A protected instance gets a session manager with the configured
// bounds; sessions that ended long ago are pruned at start, live ones
// kept.
func TestNewUISessions_ProtectedPrunesEndedSessions(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	ctx := context.Background()
	idn := &storage.Identity{PrincipalID: storage.OwnerPrincipal, Method: storage.IdentityMethodAPIKey, Subject: "k1"}
	if err := driver.Auth().Identities().Create(ctx, idn); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for id, created := range map[string]time.Time{"uis_old": now.Add(-60 * 24 * time.Hour), "uis_live": now} {
		if err := driver.UISessions().Create(ctx, &storage.UISession{
			ID: id, SecretHash: "s-" + id, PrincipalID: storage.OwnerPrincipal, IdentityID: idn.ID, Scope: authn.SessionKindUI,
			CreatedAt: created, LastSeenAt: created, IdleExpiresAt: created.Add(time.Hour), ExpiresAt: created.Add(2 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	s, err := newUISessions(ctx, &out, driver, authn.NewKeysOver(driver.Auth(), nil), config.AccessProtected,
		config.UISessionConfig{IdleTTL: time.Hour, MaxTTL: 24 * time.Hour})
	if err != nil || s == nil {
		t.Fatalf("protected instance: sessions = %v, err = %v", s, err)
	}
	if s.IdleTTL() != time.Hour || s.MaxTTL() != 24*time.Hour {
		t.Errorf("bounds = %s/%s, want the configured 1h/24h", s.IdleTTL(), s.MaxTTL())
	}
	if _, err := driver.UISessions().Get(ctx, "uis_old"); err == nil {
		t.Error("a session that ended 60 days ago survived the start-up prune")
	}
	if _, err := driver.UISessions().Get(ctx, "uis_live"); err != nil {
		t.Errorf("a live session was pruned: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("ctxt ui open")) {
		t.Errorf("serve output lacks the sign-in note:\n%s", out.String())
	}
}

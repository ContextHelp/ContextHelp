package storagetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// uiT0 is the conformance clock origin, on a millisecond boundary: the
// SQLite driver stores Unix milliseconds.
var uiT0 = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func uiSession(id, principal, tokenHash string, created time.Time, idle, maxAge time.Duration) *storage.UISession {
	return &storage.UISession{
		ID:            id,
		SecretHash:    "secret-" + id,
		PrincipalID:   principal,
		TokenHash:     tokenHash,
		Scope:         "ui",
		UserAgent:     "conformance/1.0",
		RemoteAddr:    "192.0.2.1",
		CreatedAt:     created,
		LastSeenAt:    created,
		IdleExpiresAt: created.Add(idle),
		ExpiresAt:     created.Add(maxAge),
	}
}

// UISessionConformance pins the UISessionStore contract every driver
// implements: single-use login codes, active-session lookup under idle
// and absolute expiry, touch, revocation (one session and every session
// of a minting token), listing and pruning.
func UISessionConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	st := drv.UISessions()
	if st == nil {
		t.Fatal("UISessions() returned nil")
	}

	for _, c := range []struct {
		name string
		fn   func(*testing.T, storage.UISessionStore)
	}{
		{"login code is single use", uiConfLoginCodeIsSingleUse},
		{"expired login code is refused and gone", uiConfExpiredLoginCodeIsRefusedAndGone},
		{"unknown login code", uiConfUnknownLoginCode},
		{"concurrent consumes: exactly one wins", uiConfConcurrentConsumesExactlyOneWins},
		{"create, lookup and get", uiConfCreateLookupAndGet},
		{"idle expiry and touch", uiConfIdleExpiryAndTouch},
		{"max expiry caps touch", uiConfMaxExpiryCapsTouch},
		{"revoke", uiConfRevoke},
		{"revoke by token hash", uiConfRevokeByTokenHash},
		{"list", uiConfList},
		{"prune", uiConfPrune},
	} {
		t.Run(c.name, func(t *testing.T) { c.fn(t, st) })
	}
}

func uiConfLoginCodeIsSingleUse(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	c := &storage.UILoginCode{CodeHash: "code-once", PrincipalID: "ops", TokenHash: "tok-a", Scope: "ui",
		CreatedAt: uiT0, ExpiresAt: uiT0.Add(time.Minute)}
	if err := st.CreateLoginCode(ctx, c); err != nil {
		t.Fatalf("CreateLoginCode: %v", err)
	}
	got, err := st.ConsumeLoginCode(ctx, "code-once", uiT0.Add(30*time.Second))
	if err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if got.PrincipalID != "ops" || got.TokenHash != "tok-a" || got.Scope != "ui" ||
		!got.CreatedAt.Equal(c.CreatedAt) || !got.ExpiresAt.Equal(c.ExpiresAt) {
		t.Errorf("consumed code = %+v, want the stored fields", got)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-once", uiT0.Add(31*time.Second)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("replayed consume: err = %v, want ErrNotFound", err)
	}
}

func uiConfExpiredLoginCodeIsRefusedAndGone(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	c := &storage.UILoginCode{CodeHash: "code-late", PrincipalID: "ops", TokenHash: "tok-a", Scope: "ui",
		CreatedAt: uiT0, ExpiresAt: uiT0.Add(time.Minute)}
	if err := st.CreateLoginCode(ctx, c); err != nil {
		t.Fatalf("CreateLoginCode: %v", err)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-late", uiT0.Add(time.Minute)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("consume at expiry: err = %v, want ErrNotFound", err)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-late", uiT0); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expired code survived its failed consume: err = %v", err)
	}
}

func uiConfUnknownLoginCode(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	if _, err := st.ConsumeLoginCode(ctx, "code-never", uiT0); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func uiConfConcurrentConsumesExactlyOneWins(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	c := &storage.UILoginCode{CodeHash: "code-race", PrincipalID: "ops", TokenHash: "tok-a", Scope: "ui",
		CreatedAt: uiT0, ExpiresAt: uiT0.Add(time.Minute)}
	if err := st.CreateLoginCode(ctx, c); err != nil {
		t.Fatalf("CreateLoginCode: %v", err)
	}
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = st.ConsumeLoginCode(ctx, "code-race", uiT0)
		}()
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, storage.ErrNotFound):
			t.Errorf("consume: unexpected error %v", err)
		}
	}
	if won != 1 {
		t.Errorf("%d consumes succeeded, want exactly 1", won)
	}
}

func uiConfCreateLookupAndGet(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	s := uiSession("ses-create", "ops", "tok-a", uiT0, 12*time.Hour, 7*24*time.Hour)
	if err := st.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := st.Lookup(ctx, s.SecretHash, uiT0.Add(time.Minute))
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	assertUISession(t, got, s)
	got, err = st.Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	assertUISession(t, got, s)
	if _, err := st.Lookup(ctx, "secret-nope", uiT0); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Lookup unknown secret: err = %v, want ErrNotFound", err)
	}
	if _, err := st.Get(ctx, "ses-nope"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get unknown id: err = %v, want ErrNotFound", err)
	}
}

func uiConfIdleExpiryAndTouch(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	s := uiSession("ses-idle", "ops", "tok-a", uiT0, time.Hour, 24*time.Hour)
	if err := st.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Lookup(ctx, s.SecretHash, uiT0.Add(time.Hour)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Lookup at idle expiry: err = %v, want ErrNotFound", err)
	}
	at := uiT0.Add(50 * time.Minute)
	if err := st.Touch(ctx, s.ID, at, at.Add(time.Hour)); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, err := st.Lookup(ctx, s.SecretHash, uiT0.Add(90*time.Minute))
	if err != nil {
		t.Fatalf("Lookup after touch: %v", err)
	}
	if !got.LastSeenAt.Equal(at) || !got.IdleExpiresAt.Equal(at.Add(time.Hour)) {
		t.Errorf("after touch: last_seen=%s idle_expires=%s, want %s and %s",
			got.LastSeenAt, got.IdleExpiresAt, at, at.Add(time.Hour))
	}
	if _, err := st.Lookup(ctx, s.SecretHash, at.Add(time.Hour)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Lookup at new idle expiry: err = %v, want ErrNotFound", err)
	}
	if err := st.Touch(ctx, "ses-nope", at, at); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Touch unknown id: err = %v, want ErrNotFound", err)
	}
}

func uiConfMaxExpiryCapsTouch(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	s := uiSession("ses-max", "ops", "tok-a", uiT0, time.Hour, 2*time.Hour)
	if err := st.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := uiT0.Add(90 * time.Minute)
	if err := st.Touch(ctx, s.ID, at, at.Add(time.Hour)); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, err := st.Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.IdleExpiresAt.Equal(s.ExpiresAt) {
		t.Errorf("idle expiry %s ran past the absolute expiry %s", got.IdleExpiresAt, s.ExpiresAt)
	}
	if _, err := st.Lookup(ctx, s.SecretHash, s.ExpiresAt.Add(-time.Millisecond)); err != nil {
		t.Errorf("Lookup just before max expiry: %v", err)
	}
	if _, err := st.Lookup(ctx, s.SecretHash, s.ExpiresAt); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Lookup at max expiry: err = %v, want ErrNotFound", err)
	}
}

func uiConfRevoke(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	s := uiSession("ses-revoke", "ops", "tok-a", uiT0, time.Hour, 24*time.Hour)
	if err := st.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	first := uiT0.Add(time.Minute)
	if err := st.Revoke(ctx, s.ID, first, "logout"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := st.Lookup(ctx, s.SecretHash, uiT0.Add(2*time.Minute)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Lookup after revoke: err = %v, want ErrNotFound", err)
	}
	if err := st.Revoke(ctx, s.ID, uiT0.Add(time.Hour), "revoked"); err != nil {
		t.Errorf("second Revoke: %v", err)
	}
	got, err := st.Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RevokedAt == nil || !got.RevokedAt.Equal(first) || got.RevokeReason != "logout" {
		t.Errorf("revocation = %v %q, want the first one (%s, logout)", got.RevokedAt, got.RevokeReason, first)
	}
	if err := st.Touch(ctx, s.ID, uiT0.Add(3*time.Minute), uiT0.Add(10*time.Hour)); err != nil {
		t.Errorf("Touch revoked: %v", err)
	}
	if _, err := st.Lookup(ctx, s.SecretHash, uiT0.Add(4*time.Minute)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("touch revived a revoked session: err = %v", err)
	}
	if err := st.Revoke(ctx, "ses-nope", first, "revoked"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Revoke unknown id: err = %v, want ErrNotFound", err)
	}
}

func uiConfRevokeByTokenHash(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	a1 := uiSession("ses-tok-a1", "alice", "tok-gone", uiT0, time.Hour, 24*time.Hour)
	a2 := uiSession("ses-tok-a2", "alice", "tok-gone", uiT0, time.Hour, 24*time.Hour)
	b := uiSession("ses-tok-b", "alice", "tok-kept", uiT0, time.Hour, 24*time.Hour)
	for _, s := range []*storage.UISession{a1, a2, b} {
		if err := st.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.ID, err)
		}
	}
	n, err := st.RevokeByTokenHash(ctx, "tok-gone", uiT0.Add(time.Minute), "token removed")
	if err != nil {
		t.Fatalf("RevokeByTokenHash: %v", err)
	}
	if n != 2 {
		t.Errorf("revoked %d sessions, want 2", n)
	}
	for _, s := range []*storage.UISession{a1, a2} {
		if _, err := st.Lookup(ctx, s.SecretHash, uiT0.Add(2*time.Minute)); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("%s still active: err = %v", s.ID, err)
		}
	}
	if _, err := st.Lookup(ctx, b.SecretHash, uiT0.Add(2*time.Minute)); err != nil {
		t.Errorf("session of another token revoked: %v", err)
	}
	if n, err := st.RevokeByTokenHash(ctx, "tok-gone", uiT0.Add(time.Hour), "again"); err != nil || n != 0 {
		t.Errorf("second RevokeByTokenHash = %d, %v; want 0, nil", n, err)
	}
}

func uiConfList(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	base := uiT0.Add(48 * time.Hour)
	for i, p := range []string{"lister", "lister", "lister", "other"} {
		s := uiSession(fmt.Sprintf("ses-list-%d", i), p, "tok-list", base.Add(time.Duration(i)*time.Minute), time.Hour, 24*time.Hour)
		if err := st.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := st.Revoke(ctx, "ses-list-1", base.Add(10*time.Minute), "revoked"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	all, err := st.List(ctx, storage.UISessionFilter{PrincipalID: "lister"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := ids(all); fmt.Sprint(got) != "[ses-list-2 ses-list-1 ses-list-0]" {
		t.Errorf("List(principal) = %v, want newest first", got)
	}
	active, err := st.List(ctx, storage.UISessionFilter{PrincipalID: "lister", ActiveOnly: true, Now: base.Add(15 * time.Minute)})
	if err != nil {
		t.Fatalf("List active: %v", err)
	}
	if got := ids(active); fmt.Sprint(got) != "[ses-list-2 ses-list-0]" {
		t.Errorf("List(active) = %v", got)
	}
	expired, err := st.List(ctx, storage.UISessionFilter{PrincipalID: "lister", ActiveOnly: true, Now: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("List active later: %v", err)
	}
	if len(expired) != 0 {
		t.Errorf("idle-expired sessions listed as active: %v", ids(expired))
	}
	limited, err := st.List(ctx, storage.UISessionFilter{PrincipalID: "lister", Limit: 1})
	if err != nil {
		t.Fatalf("List limit: %v", err)
	}
	if got := ids(limited); fmt.Sprint(got) != "[ses-list-2]" {
		t.Errorf("List(limit 1) = %v", got)
	}
}

func uiConfPrune(t *testing.T, st storage.UISessionStore) {
	ctx := context.Background()
	base := uiT0.Add(30 * 24 * time.Hour)
	live := uiSession("ses-prune-live", "pruner", "tok-p", base, time.Hour, 24*time.Hour)
	idle := uiSession("ses-prune-idle", "pruner", "tok-p", base.Add(-3*time.Hour), time.Hour, 24*time.Hour)
	gone := uiSession("ses-prune-revoked", "pruner", "tok-p", base, time.Hour, 24*time.Hour)
	for _, s := range []*storage.UISession{live, idle, gone} {
		if err := st.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.ID, err)
		}
	}
	if err := st.Revoke(ctx, gone.ID, base.Add(-time.Minute), "revoked"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	oldCode := &storage.UILoginCode{CodeHash: "code-prune", PrincipalID: "pruner", TokenHash: "tok-p", Scope: "ui",
		CreatedAt: base.Add(-time.Hour), ExpiresAt: base.Add(-time.Hour + time.Minute)}
	if err := st.CreateLoginCode(ctx, oldCode); err != nil {
		t.Fatalf("CreateLoginCode: %v", err)
	}
	n, err := st.Prune(ctx, base)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n < 2 {
		t.Errorf("Prune deleted %d sessions, want at least the idle and the revoked one", n)
	}
	for _, id := range []string{idle.ID, gone.ID} {
		if _, err := st.Get(ctx, id); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("%s survived Prune: err = %v", id, err)
		}
	}
	if _, err := st.Get(ctx, live.ID); err != nil {
		t.Errorf("live session pruned: %v", err)
	}
	// Consume ignores its clock argument's past: a pruned code is gone
	// even when asked at a time it would still be valid.
	if _, err := st.ConsumeLoginCode(ctx, oldCode.CodeHash, oldCode.CreatedAt); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expired code survived Prune: err = %v", err)
	}
}

func ids(ss []*storage.UISession) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.ID
	}
	return out
}

func assertUISession(t *testing.T, got, want *storage.UISession) {
	t.Helper()
	if got.ID != want.ID || got.SecretHash != want.SecretHash || got.PrincipalID != want.PrincipalID ||
		got.TokenHash != want.TokenHash || got.Scope != want.Scope || got.UserAgent != want.UserAgent ||
		got.RemoteAddr != want.RemoteAddr || got.RevokeReason != want.RevokeReason {
		t.Errorf("session fields = %+v, want %+v", got, want)
	}
	for _, p := range []struct {
		name      string
		got, want time.Time
	}{
		{"created_at", got.CreatedAt, want.CreatedAt},
		{"last_seen_at", got.LastSeenAt, want.LastSeenAt},
		{"idle_expires_at", got.IdleExpiresAt, want.IdleExpiresAt},
		{"expires_at", got.ExpiresAt, want.ExpiresAt},
	} {
		if !p.got.Equal(p.want) {
			t.Errorf("%s = %s, want %s", p.name, p.got, p.want)
		}
	}
	if (got.RevokedAt == nil) != (want.RevokedAt == nil) {
		t.Errorf("revoked_at = %v, want %v", got.RevokedAt, want.RevokedAt)
	}
}

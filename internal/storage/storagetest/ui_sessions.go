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

// uiRegistry is the principal registry the session cases run against:
// every session and login code references one of these identities,
// since the store refuses an identity it does not know.
type uiRegistry struct {
	auth storage.AuthStore
	// ops is a person with two API keys; other is another person with one.
	ops, other         *storage.Principal
	opsA, opsB, otherA *storage.Identity
}

func newUIRegistry(t *testing.T, drv storage.StorageDriver) *uiRegistry {
	t.Helper()
	r := &uiRegistry{auth: drv.Auth()}
	r.ops = newPrincipal(t, r.auth, "prn_ui_ops", uiT0)
	r.other = newPrincipal(t, r.auth, "prn_ui_other", uiT0)
	r.opsA = newIdentity(t, r.auth, r.ops.ID, "ui-ops-a", uiT0)
	r.opsB = newIdentity(t, r.auth, r.ops.ID, "ui-ops-b", uiT0)
	r.otherA = newIdentity(t, r.auth, r.other.ID, "ui-other-a", uiT0)
	return r
}

// session returns an unstored session of idn's principal, minted by idn.
func (r *uiRegistry) session(id string, idn *storage.Identity, created time.Time, idle, maxAge time.Duration) *storage.UISession {
	return &storage.UISession{
		ID:            id,
		SecretHash:    "secret-" + id,
		PrincipalID:   idn.PrincipalID,
		IdentityID:    idn.ID,
		Scope:         "ui",
		UserAgent:     "conformance/1.0",
		RemoteAddr:    "192.0.2.1",
		CreatedAt:     created,
		LastSeenAt:    created,
		IdleExpiresAt: created.Add(idle),
		ExpiresAt:     created.Add(maxAge),
	}
}

// code returns an unstored login code minted by idn.
func (r *uiRegistry) code(hash string, idn *storage.Identity, created time.Time, ttl time.Duration) *storage.UILoginCode {
	return &storage.UILoginCode{CodeHash: hash, PrincipalID: idn.PrincipalID, IdentityID: idn.ID, Scope: "ui",
		CreatedAt: created, ExpiresAt: created.Add(ttl)}
}

// UISessionConformance pins the UISessionStore contract every driver
// implements: single-use login codes, active-session lookup under idle
// and absolute expiry, touch, revocation, listing and pruning, and the
// binding to the principal registry: a session or code names the
// identity that minted it, and revoking that identity, or its
// principal, ends the sessions and codes it minted in the same step.
func UISessionConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	st := drv.UISessions()
	if st == nil {
		t.Fatal("UISessions() returned nil")
	}
	reg := newUIRegistry(t, drv)

	for _, c := range []struct {
		name string
		fn   func(*testing.T, storage.UISessionStore, *uiRegistry)
	}{
		{"login code is single use", uiConfLoginCodeIsSingleUse},
		{"expired login code is refused and gone", uiConfExpiredLoginCodeIsRefusedAndGone},
		{"unknown login code", uiConfUnknownLoginCode},
		{"concurrent consumes: exactly one wins", uiConfConcurrentConsumesExactlyOneWins},
		{"create, lookup and get", uiConfCreateLookupAndGet},
		{"unknown identity is refused", uiConfUnknownIdentityRefused},
		{"idle expiry and touch", uiConfIdleExpiryAndTouch},
		{"max expiry caps touch", uiConfMaxExpiryCapsTouch},
		{"max expiry holds without idle", uiConfMaxExpiryIndependentOfIdle},
		{"revoke", uiConfRevoke},
		{"identity revoke ends its sessions and codes", uiConfIdentityRevokeCascades},
		{"principal revoke ends every session and code", uiConfPrincipalRevokeCascades},
		{"list", uiConfList},
		{"prune", uiConfPrune},
	} {
		t.Run(c.name, func(t *testing.T) { c.fn(t, st, reg) })
	}
}

func uiConfLoginCodeIsSingleUse(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	c := r.code("code-once", r.opsA, uiT0, time.Minute)
	if err := st.CreateLoginCode(ctx, c); err != nil {
		t.Fatalf("CreateLoginCode: %v", err)
	}
	got, err := st.ConsumeLoginCode(ctx, "code-once", uiT0.Add(30*time.Second))
	if err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if got.PrincipalID != r.ops.ID || got.IdentityID != r.opsA.ID || got.Scope != "ui" ||
		!got.CreatedAt.Equal(c.CreatedAt) || !got.ExpiresAt.Equal(c.ExpiresAt) {
		t.Errorf("consumed code = %+v, want the stored fields", got)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-once", uiT0.Add(31*time.Second)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("replayed consume: err = %v, want ErrNotFound", err)
	}
}

func uiConfExpiredLoginCodeIsRefusedAndGone(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	if err := st.CreateLoginCode(ctx, r.code("code-late", r.opsA, uiT0, time.Minute)); err != nil {
		t.Fatalf("CreateLoginCode: %v", err)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-late", uiT0.Add(time.Minute)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("consume at expiry: err = %v, want ErrNotFound", err)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-late", uiT0); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expired code survived its failed consume: err = %v", err)
	}
}

func uiConfUnknownLoginCode(t *testing.T, st storage.UISessionStore, _ *uiRegistry) {
	ctx := context.Background()
	if _, err := st.ConsumeLoginCode(ctx, "code-never", uiT0); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func uiConfConcurrentConsumesExactlyOneWins(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	if err := st.CreateLoginCode(ctx, r.code("code-race", r.opsA, uiT0, time.Minute)); err != nil {
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

func uiConfCreateLookupAndGet(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	s := r.session("ses-create", r.opsA, uiT0, 12*time.Hour, 7*24*time.Hour)
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

// A session or code must name an identity the registry holds: the
// binding is a foreign key, not a convention.
func uiConfUnknownIdentityRefused(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	ghost := &storage.Identity{ID: "idn_ui_ghost", PrincipalID: r.ops.ID}
	if err := st.Create(ctx, r.session("ses-ghost", ghost, uiT0, time.Hour, 24*time.Hour)); err == nil {
		t.Error("Create with an unknown identity succeeded")
	}
	if err := st.CreateLoginCode(ctx, r.code("code-ghost", ghost, uiT0, time.Minute)); err == nil {
		t.Error("CreateLoginCode with an unknown identity succeeded")
	}
}

func uiConfIdleExpiryAndTouch(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	s := r.session("ses-idle", r.opsA, uiT0, time.Hour, 24*time.Hour)
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

func uiConfMaxExpiryCapsTouch(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	s := r.session("ses-max", r.opsA, uiT0, time.Hour, 2*time.Hour)
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

// uiConfMaxExpiryIndependentOfIdle pins the absolute expiry on its own:
// a row whose idle expiry lies past it (Touch never writes one, a
// buggy or older writer could) still ends at ExpiresAt.
func uiConfMaxExpiryIndependentOfIdle(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	s := r.session("ses-max-only", r.opsA, uiT0, 48*time.Hour, time.Hour)
	if err := st.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Lookup(ctx, s.SecretHash, uiT0.Add(time.Hour)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Lookup at max expiry with a later idle expiry: err = %v, want ErrNotFound", err)
	}
	active, err := st.List(ctx, storage.UISessionFilter{ActiveOnly: true, Now: uiT0.Add(time.Hour), PrincipalID: r.ops.ID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, a := range active {
		if a.ID == s.ID {
			t.Error("session past its max expiry listed as active")
		}
	}
}

func uiConfRevoke(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	s := r.session("ses-revoke", r.opsA, uiT0, time.Hour, 24*time.Hour)
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

// Revoking an identity ends the sessions and pending codes it minted,
// in the identity's own revoke call, and nothing of its principal's
// other identities.
func uiConfIdentityRevokeCascades(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	gone := newIdentity(t, r.auth, r.ops.ID, "ui-ops-gone", uiT0)
	a1 := r.session("ses-idn-a1", gone, uiT0, time.Hour, 24*time.Hour)
	a2 := r.session("ses-idn-a2", gone, uiT0, time.Hour, 24*time.Hour)
	kept := r.session("ses-idn-kept", r.opsB, uiT0, time.Hour, 24*time.Hour)
	ended := r.session("ses-idn-ended", gone, uiT0, time.Hour, 24*time.Hour)
	for _, s := range []*storage.UISession{a1, a2, kept, ended} {
		if err := st.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.ID, err)
		}
	}
	earlier := uiT0.Add(30 * time.Second)
	if err := st.Revoke(ctx, ended.ID, earlier, "logout"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	for _, c := range []*storage.UILoginCode{r.code("code-idn-gone", gone, uiT0, time.Hour), r.code("code-idn-kept", r.opsB, uiT0, time.Hour)} {
		if err := st.CreateLoginCode(ctx, c); err != nil {
			t.Fatalf("CreateLoginCode: %v", err)
		}
	}

	at := uiT0.Add(time.Minute)
	if err := r.auth.Identities().Revoke(ctx, gone.ID, at); err != nil {
		t.Fatalf("revoke identity: %v", err)
	}
	for _, s := range []*storage.UISession{a1, a2} {
		got, err := st.Get(ctx, s.ID)
		if err != nil {
			t.Fatalf("Get %s: %v", s.ID, err)
		}
		if got.RevokedAt == nil || !got.RevokedAt.Equal(at) || got.RevokeReason != storage.RevokeReasonIdentityRevoked {
			t.Errorf("%s after identity revoke = %v %q, want %s %q", s.ID, got.RevokedAt, got.RevokeReason, at, storage.RevokeReasonIdentityRevoked)
		}
		if _, err := st.Lookup(ctx, s.SecretHash, at.Add(time.Second)); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("%s still active: err = %v", s.ID, err)
		}
	}
	if got, _ := st.Get(ctx, ended.ID); got == nil || !got.RevokedAt.Equal(earlier) || got.RevokeReason != "logout" {
		t.Errorf("an already ended session lost its first revocation: %+v", got)
	}
	if _, err := st.Lookup(ctx, kept.SecretHash, at.Add(time.Second)); err != nil {
		t.Errorf("a session of the principal's other identity was ended: %v", err)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-idn-gone", at); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("a code the revoked identity minted survived: err = %v", err)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-idn-kept", at); err != nil {
		t.Errorf("a code of the other identity was dropped: %v", err)
	}
}

// Revoking a principal ends every session and code of it, whichever
// identity minted them, and none of another principal's.
func uiConfPrincipalRevokeCascades(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	doomed := newPrincipal(t, r.auth, "prn_ui_doomed", uiT0)
	d1 := newIdentity(t, r.auth, doomed.ID, "ui-doomed-1", uiT0)
	d2 := newIdentity(t, r.auth, doomed.ID, "ui-doomed-2", uiT0)
	s1 := r.session("ses-prn-1", d1, uiT0, time.Hour, 24*time.Hour)
	s2 := r.session("ses-prn-2", d2, uiT0, time.Hour, 24*time.Hour)
	kept := r.session("ses-prn-kept", r.otherA, uiT0, time.Hour, 24*time.Hour)
	for _, s := range []*storage.UISession{s1, s2, kept} {
		if err := st.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.ID, err)
		}
	}
	for _, c := range []*storage.UILoginCode{r.code("code-prn-gone", d2, uiT0, time.Hour), r.code("code-prn-kept", r.otherA, uiT0, time.Hour)} {
		if err := st.CreateLoginCode(ctx, c); err != nil {
			t.Fatalf("CreateLoginCode: %v", err)
		}
	}

	at := uiT0.Add(time.Minute)
	if err := r.auth.Principals().Revoke(ctx, doomed.ID, at); err != nil {
		t.Fatalf("revoke principal: %v", err)
	}
	for _, s := range []*storage.UISession{s1, s2} {
		got, err := st.Get(ctx, s.ID)
		if err != nil {
			t.Fatalf("Get %s: %v", s.ID, err)
		}
		if got.RevokedAt == nil || !got.RevokedAt.Equal(at) || got.RevokeReason != storage.RevokeReasonPrincipalRevoked {
			t.Errorf("%s after principal revoke = %v %q, want %s %q", s.ID, got.RevokedAt, got.RevokeReason, at, storage.RevokeReasonPrincipalRevoked)
		}
	}
	active, err := st.List(ctx, storage.UISessionFilter{ActiveOnly: true, Now: at.Add(time.Second), PrincipalID: doomed.ID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("revoked principal still has active sessions: %v", ids(active))
	}
	if _, err := st.Lookup(ctx, kept.SecretHash, at.Add(time.Second)); err != nil {
		t.Errorf("another principal's session was ended: %v", err)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-prn-gone", at); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("a code of the revoked principal survived: err = %v", err)
	}
	if _, err := st.ConsumeLoginCode(ctx, "code-prn-kept", at); err != nil {
		t.Errorf("another principal's code was dropped: %v", err)
	}
}

func uiConfList(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	lister := newPrincipal(t, r.auth, "prn_ui_lister", uiT0)
	lk := newIdentity(t, r.auth, lister.ID, "ui-lister", uiT0)
	base := uiT0.Add(48 * time.Hour)
	for i, idn := range []*storage.Identity{lk, lk, lk, r.otherA} {
		s := r.session(fmt.Sprintf("ses-list-%d", i), idn, base.Add(time.Duration(i)*time.Minute), time.Hour, 24*time.Hour)
		if err := st.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if err := st.Revoke(ctx, "ses-list-1", base.Add(10*time.Minute), "revoked"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	all, err := st.List(ctx, storage.UISessionFilter{PrincipalID: lister.ID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := ids(all); fmt.Sprint(got) != "[ses-list-2 ses-list-1 ses-list-0]" {
		t.Errorf("List(principal) = %v, want newest first", got)
	}
	active, err := st.List(ctx, storage.UISessionFilter{PrincipalID: lister.ID, ActiveOnly: true, Now: base.Add(15 * time.Minute)})
	if err != nil {
		t.Fatalf("List active: %v", err)
	}
	if got := ids(active); fmt.Sprint(got) != "[ses-list-2 ses-list-0]" {
		t.Errorf("List(active) = %v", got)
	}
	expired, err := st.List(ctx, storage.UISessionFilter{PrincipalID: lister.ID, ActiveOnly: true, Now: base.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("List active later: %v", err)
	}
	if len(expired) != 0 {
		t.Errorf("idle-expired sessions listed as active: %v", ids(expired))
	}
	limited, err := st.List(ctx, storage.UISessionFilter{PrincipalID: lister.ID, Limit: 1})
	if err != nil {
		t.Fatalf("List limit: %v", err)
	}
	if got := ids(limited); fmt.Sprint(got) != "[ses-list-2]" {
		t.Errorf("List(limit 1) = %v", got)
	}
}

func uiConfPrune(t *testing.T, st storage.UISessionStore, r *uiRegistry) {
	ctx := context.Background()
	pruner := newPrincipal(t, r.auth, "prn_ui_pruner", uiT0)
	pk := newIdentity(t, r.auth, pruner.ID, "ui-pruner", uiT0)
	base := uiT0.Add(30 * 24 * time.Hour)
	live := r.session("ses-prune-live", pk, base, time.Hour, 24*time.Hour)
	idle := r.session("ses-prune-idle", pk, base.Add(-3*time.Hour), time.Hour, 24*time.Hour)
	gone := r.session("ses-prune-revoked", pk, base, time.Hour, 24*time.Hour)
	for _, s := range []*storage.UISession{live, idle, gone} {
		if err := st.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.ID, err)
		}
	}
	if err := st.Revoke(ctx, gone.ID, base.Add(-time.Minute), "revoked"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	oldCode := r.code("code-prune", pk, base.Add(-time.Hour), time.Minute)
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
		got.IdentityID != want.IdentityID || got.Scope != want.Scope || got.UserAgent != want.UserAgent ||
		got.RemoteAddr != want.RemoteAddr || got.RevokeReason != want.RevokeReason {
		t.Errorf("session = %+v, want %+v", got, want)
	}
	for name, pair := range map[string][2]time.Time{
		"created_at": {got.CreatedAt, want.CreatedAt}, "last_seen_at": {got.LastSeenAt, want.LastSeenAt},
		"idle_expires_at": {got.IdleExpiresAt, want.IdleExpiresAt}, "expires_at": {got.ExpiresAt, want.ExpiresAt},
	} {
		if !pair[0].Equal(pair[1]) {
			t.Errorf("%s = %s, want %s", name, pair[0], pair[1])
		}
	}
	if (got.RevokedAt == nil) != (want.RevokedAt == nil) || (got.RevokedAt != nil && !got.RevokedAt.Equal(*want.RevokedAt)) {
		t.Errorf("revoked_at = %v, want %v", got.RevokedAt, want.RevokedAt)
	}
}

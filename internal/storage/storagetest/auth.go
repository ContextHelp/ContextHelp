package storagetest

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// authT0 is the conformance clock origin, on a millisecond boundary:
// the SQLite driver stores Unix milliseconds.
var authT0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// AuthConformance pins the principal registry contract every driver
// implements (ADR-079): the seeded owner, principal creation, listing,
// suspension and revocation with its cascade; identity creation,
// lookup, revocation and rate-limited touch; single-use tickets with
// expiry, kind and principal-status checks, atomic under concurrency;
// and the kv store kit's API keys live in.
func AuthConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	st := drv.Auth()
	if st == nil {
		t.Fatal("Auth() returned nil")
	}
	for _, c := range []struct {
		name string
		fn   func(*testing.T, storage.AuthStore)
	}{
		{"owner is seeded", authConfOwnerSeeded},
		{"principal create defaults and round trip", authConfPrincipalCreate},
		{"principal create refusals", authConfPrincipalCreateRefusals},
		{"principal list", authConfPrincipalList},
		{"principal suspend", authConfPrincipalSuspend},
		{"principal revoke cascades", authConfPrincipalRevoke},
		{"identity create, get and find", authConfIdentityCreate},
		{"identity create refusals", authConfIdentityCreateRefusals},
		{"identity list by principal", authConfIdentityList},
		{"identity revoke", authConfIdentityRevoke},
		{"identity touch at most once a minute", authConfIdentityTouch},
		{"ticket issue and single-use redeem", authConfTicketRedeem},
		{"ticket kind mismatch", authConfTicketKind},
		{"ticket expiry", authConfTicketExpiry},
		{"ticket unknown secret", authConfTicketUnknown},
		{"ticket of an inactive principal", authConfTicketInactivePrincipal},
		{"ticket issue refusals", authConfTicketIssueRefusals},
		{"ticket concurrent redeems: exactly one wins", authConfTicketConcurrency},
		{"kv put, get and delete", authConfKVPutGetDelete},
		{"kv list by prefix", authConfKVList},
	} {
		t.Run(c.name, func(t *testing.T) { c.fn(t, st) })
	}
}

// newPrincipal creates an active person with id and returns it.
func newPrincipal(t *testing.T, st storage.AuthStore, id string, at time.Time) *storage.Principal {
	t.Helper()
	p := &storage.Principal{ID: id, Kind: storage.PrincipalKindPerson, DisplayName: "Conformance " + id, CreatedAt: at}
	if err := st.Principals().Create(context.Background(), p); err != nil {
		t.Fatalf("create principal %s: %v", id, err)
	}
	return p
}

// newIdentity binds an api_key identity with subject to principal.
func newIdentity(t *testing.T, st storage.AuthStore, principal, subject string, at time.Time) *storage.Identity {
	t.Helper()
	i := &storage.Identity{PrincipalID: principal, Method: storage.IdentityMethodAPIKey, Subject: subject, Name: "key " + subject, CreatedAt: at}
	if err := st.Identities().Create(context.Background(), i); err != nil {
		t.Fatalf("create identity %s: %v", subject, err)
	}
	return i
}

func authConfOwnerSeeded(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	owner, err := st.Principals().Get(ctx, storage.OwnerPrincipal)
	if err != nil {
		t.Fatalf("Get(owner): %v", err)
	}
	if owner.Kind != storage.PrincipalKindOwner || owner.Status != storage.PrincipalActive || !owner.Active() {
		t.Errorf("owner = %+v, want kind owner, status active", owner)
	}
	if owner.DisplayName == "" {
		t.Error("owner has no display name")
	}
	if !slices.Contains(owner.Roles, "admin") {
		t.Errorf("owner roles = %v, want admin", owner.Roles)
	}
	if owner.CreatedAt.IsZero() || owner.RevokedAt != nil {
		t.Errorf("owner times = created %v revoked %v", owner.CreatedAt, owner.RevokedAt)
	}
	owners, err := st.Principals().List(ctx, storage.PrincipalFilter{Kind: storage.PrincipalKindOwner})
	if err != nil || len(owners) != 1 {
		t.Errorf("List(kind owner) = %d rows, %v; want exactly one", len(owners), err)
	}
}

func authConfPrincipalCreate(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	p := &storage.Principal{Kind: storage.PrincipalKindDevice, DisplayName: "phone"}
	if err := st.Principals().Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(p.ID, "prn_") || len(p.ID) != len("prn_")+26 {
		t.Errorf("generated id %q, want prn_<26-char ULID>", p.ID)
	}
	if !slices.Equal(p.Roles, storage.DefaultPrincipalRoles) || p.Status != storage.PrincipalActive || p.CreatedAt.IsZero() {
		t.Errorf("defaults not filled: %+v", p)
	}
	got, err := st.Principals().Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != p.ID || got.Kind != p.Kind || got.DisplayName != p.DisplayName || !slices.Equal(got.Roles, p.Roles) ||
		got.Status != p.Status || got.CreatedAt.Sub(p.CreatedAt).Abs() >= time.Millisecond || got.RevokedAt != nil {
		t.Errorf("Get = %+v, want %+v", got, p)
	}

	explicit := &storage.Principal{ID: "prn_conf_explicit", Kind: storage.PrincipalKindPeer, DisplayName: "peer",
		Roles: []string{"writer", "reader"}, CreatedAt: authT0}
	if err := st.Principals().Create(ctx, explicit); err != nil {
		t.Fatalf("Create explicit: %v", err)
	}
	got, err = st.Principals().Get(ctx, "prn_conf_explicit")
	if err != nil {
		t.Fatalf("Get explicit: %v", err)
	}
	if !slices.Equal(got.Roles, []string{"writer", "reader"}) || !got.CreatedAt.Equal(authT0) {
		t.Errorf("explicit round trip = %+v", got)
	}
}

func authConfPrincipalCreateRefusals(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	newPrincipal(t, st, "prn_conf_dup", authT0)
	err := st.Principals().Create(ctx, &storage.Principal{ID: "prn_conf_dup", Kind: storage.PrincipalKindPerson, DisplayName: "again"})
	if !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("duplicate id: %v, want ErrAlreadyExists", err)
	}
	for name, p := range map[string]*storage.Principal{
		"owner kind":   {Kind: storage.PrincipalKindOwner, DisplayName: "second owner"},
		"owner id":     {ID: storage.OwnerPrincipal, Kind: storage.PrincipalKindPerson, DisplayName: "impostor"},
		"unknown kind": {Kind: "agent", DisplayName: "bot"},
		"no name":      {Kind: storage.PrincipalKindPerson, DisplayName: "  "},
		"bad status":   {Kind: storage.PrincipalKindPerson, DisplayName: "x", Status: "banned"},
		"bad role":     {Kind: storage.PrincipalKindPerson, DisplayName: "x", Roles: []string{"read er"}},
	} {
		err := st.Principals().Create(ctx, p)
		if err == nil {
			t.Errorf("%s: created, want a refusal", name)
		}
		if strings.HasPrefix(name, "owner") && !errors.Is(err, storage.ErrOwnerProtected) {
			t.Errorf("%s: %v, want ErrOwnerProtected", name, err)
		}
	}
	if _, err := st.Principals().Get(ctx, "prn_conf_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get(missing): %v, want ErrNotFound", err)
	}
}

func authConfPrincipalList(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	a := newPrincipal(t, st, "prn_conf_list_a", authT0.Add(time.Minute))
	b := newPrincipal(t, st, "prn_conf_list_b", authT0.Add(2*time.Minute))
	if err := st.Principals().Suspend(ctx, b.ID); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	ids := func(ps []*storage.Principal) []string {
		out := make([]string, 0, len(ps))
		for _, p := range ps {
			out = append(out, p.ID)
		}
		return out
	}
	all, err := st.Principals().List(ctx, storage.PrincipalFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) < 3 || !slices.Contains(ids(all), storage.OwnerPrincipal) {
		t.Errorf("List = %v, want the owner and every principal", ids(all))
	}
	ia, ib := slices.Index(ids(all), a.ID), slices.Index(ids(all), b.ID)
	if ia < 0 || ib < 0 || ia > ib {
		t.Errorf("List order = %v, want %s before %s (oldest first)", ids(all), a.ID, b.ID)
	}
	persons, err := st.Principals().List(ctx, storage.PrincipalFilter{Kind: storage.PrincipalKindPerson, Status: storage.PrincipalSuspended})
	if err != nil {
		t.Fatalf("List(person, suspended): %v", err)
	}
	assertIDs(t, "List(person, suspended)", ids(persons), []string{b.ID})
	one, err := st.Principals().List(ctx, storage.PrincipalFilter{Limit: 1})
	if err != nil || len(one) != 1 {
		t.Errorf("List(limit 1) = %d rows, %v", len(one), err)
	}
}

func authConfPrincipalSuspend(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	p := newPrincipal(t, st, "prn_conf_suspend", authT0)
	if err := st.Principals().Suspend(ctx, p.ID); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	got, _ := st.Principals().Get(ctx, p.ID)
	if got == nil || got.Status != storage.PrincipalSuspended || got.Active() {
		t.Errorf("after Suspend = %+v, want status suspended", got)
	}
	if err := st.Principals().Suspend(ctx, p.ID); err != nil {
		t.Errorf("second Suspend: %v, want no error", err)
	}
	if err := st.Principals().Suspend(ctx, storage.OwnerPrincipal); !errors.Is(err, storage.ErrOwnerProtected) {
		t.Errorf("Suspend(owner): %v, want ErrOwnerProtected", err)
	}
	if err := st.Principals().Suspend(ctx, "prn_conf_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Suspend(missing): %v, want ErrNotFound", err)
	}
}

func authConfPrincipalRevoke(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	p := newPrincipal(t, st, "prn_conf_revoke", authT0)
	early := newIdentity(t, st, p.ID, "revoke-early", authT0)
	live := newIdentity(t, st, p.ID, "revoke-live", authT0.Add(time.Second))
	other := newPrincipal(t, st, "prn_conf_revoke_other", authT0)
	untouched := newIdentity(t, st, other.ID, "revoke-untouched", authT0)
	t1, t2 := authT0.Add(time.Hour), authT0.Add(2*time.Hour)
	if err := st.Identities().Revoke(ctx, early.ID, t1); err != nil {
		t.Fatalf("Revoke identity: %v", err)
	}

	if err := st.Principals().Revoke(ctx, p.ID, t2); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, _ := st.Principals().Get(ctx, p.ID)
	if got == nil || got.Status != storage.PrincipalRevoked || got.RevokedAt == nil || !got.RevokedAt.Equal(t2) {
		t.Errorf("after Revoke = %+v, want status revoked at %v", got, t2)
	}
	for _, c := range []struct {
		id   string
		want time.Time
	}{{early.ID, t1}, {live.ID, t2}} {
		i, err := st.Identities().Get(ctx, c.id)
		if err != nil {
			t.Fatalf("Get identity %s: %v", c.id, err)
		}
		if i.RevokedAt == nil || !i.RevokedAt.Equal(c.want) {
			t.Errorf("identity %s revoked at %v, want %v", c.id, i.RevokedAt, c.want)
		}
	}
	if i, _ := st.Identities().Get(ctx, untouched.ID); i == nil || i.RevokedAt != nil {
		t.Errorf("another principal's identity = %+v, want untouched", i)
	}

	if err := st.Principals().Revoke(ctx, p.ID, authT0.Add(3*time.Hour)); err != nil {
		t.Errorf("second Revoke: %v, want no error", err)
	}
	if got, _ = st.Principals().Get(ctx, p.ID); got == nil || !got.RevokedAt.Equal(t2) {
		t.Errorf("second Revoke moved revoked_at to %v, want %v kept", got.RevokedAt, t2)
	}
	if err := st.Principals().Suspend(ctx, p.ID); !errors.Is(err, storage.ErrPrincipalInactive) {
		t.Errorf("Suspend(revoked): %v, want ErrPrincipalInactive", err)
	}
	if err := st.Principals().Revoke(ctx, storage.OwnerPrincipal, t2); !errors.Is(err, storage.ErrOwnerProtected) {
		t.Errorf("Revoke(owner): %v, want ErrOwnerProtected", err)
	}
	if err := st.Principals().Revoke(ctx, "prn_conf_missing", t2); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Revoke(missing): %v, want ErrNotFound", err)
	}
}

func authConfIdentityCreate(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	p := newPrincipal(t, st, "prn_conf_idn", authT0)
	i := &storage.Identity{PrincipalID: p.ID, Method: storage.IdentityMethodProxy, Issuer: "tailscale",
		Subject: "alice@example.com", Name: "tailnet login"}
	if err := st.Identities().Create(ctx, i); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(i.ID, "idn_") || len(i.ID) != len("idn_")+26 || i.CreatedAt.IsZero() {
		t.Errorf("generated identity = %+v, want idn_<ULID> and a created time", i)
	}
	got, err := st.Identities().Get(ctx, i.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PrincipalID != p.ID || got.Method != i.Method || got.Issuer != i.Issuer || got.Subject != i.Subject ||
		got.Name != i.Name || got.CreatedAt.Sub(i.CreatedAt).Abs() >= time.Millisecond || got.RevokedAt != nil || got.LastUsedAt != nil {
		t.Errorf("Get = %+v, want %+v", got, i)
	}
	found, err := st.Identities().Find(ctx, storage.IdentityMethodProxy, "tailscale", "alice@example.com")
	if err != nil || found.ID != i.ID {
		t.Errorf("Find = %+v, %v; want %s", found, err, i.ID)
	}
	if _, err := st.Identities().Find(ctx, storage.IdentityMethodProxy, "", "alice@example.com"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Find with another issuer: %v, want ErrNotFound", err)
	}
	if _, err := st.Identities().Get(ctx, "idn_conf_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get(missing): %v, want ErrNotFound", err)
	}
}

func authConfIdentityCreateRefusals(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	p := newPrincipal(t, st, "prn_conf_idn_refusals", authT0)
	taken := newIdentity(t, st, p.ID, "taken", authT0)
	dup := &storage.Identity{PrincipalID: storage.OwnerPrincipal, Method: taken.Method, Issuer: taken.Issuer, Subject: taken.Subject}
	if err := st.Identities().Create(ctx, dup); !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("duplicate triple: %v, want ErrAlreadyExists", err)
	}
	orphan := &storage.Identity{PrincipalID: "prn_conf_missing", Method: storage.IdentityMethodAPIKey, Subject: "k1"}
	if err := st.Identities().Create(ctx, orphan); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("unknown principal: %v, want ErrNotFound", err)
	}
	for name, bad := range map[string]*storage.Identity{
		"unknown method": {PrincipalID: p.ID, Method: "password", Subject: "x"},
		"no subject":     {PrincipalID: p.ID, Method: storage.IdentityMethodAPIKey},
		"no principal":   {Method: storage.IdentityMethodAPIKey, Subject: "x"},
	} {
		if err := st.Identities().Create(ctx, bad); err == nil {
			t.Errorf("%s: created, want a refusal", name)
		}
	}
}

func authConfIdentityList(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	p := newPrincipal(t, st, "prn_conf_idn_list", authT0)
	second := newIdentity(t, st, p.ID, "list-2", authT0.Add(time.Minute))
	first := newIdentity(t, st, p.ID, "list-1", authT0)
	got, err := st.Identities().ListByPrincipal(ctx, p.ID)
	if err != nil {
		t.Fatalf("ListByPrincipal: %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, i := range got {
		ids = append(ids, i.ID)
	}
	assertIDs(t, "ListByPrincipal", ids, []string{first.ID, second.ID})
	none, err := st.Identities().ListByPrincipal(ctx, "prn_conf_missing")
	if err != nil || len(none) != 0 {
		t.Errorf("ListByPrincipal(missing) = %v, %v; want empty", none, err)
	}
}

func authConfIdentityRevoke(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	p := newPrincipal(t, st, "prn_conf_idn_revoke", authT0)
	i := newIdentity(t, st, p.ID, "revoke-me", authT0)
	t1 := authT0.Add(time.Hour)
	if err := st.Identities().Revoke(ctx, i.ID, t1); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, _ := st.Identities().Get(ctx, i.ID)
	if got == nil || got.RevokedAt == nil || !got.RevokedAt.Equal(t1) || got.Active() {
		t.Errorf("after Revoke = %+v, want revoked at %v", got, t1)
	}
	if err := st.Identities().Revoke(ctx, i.ID, t1.Add(time.Hour)); err != nil {
		t.Errorf("second Revoke: %v, want no error", err)
	}
	if got, _ = st.Identities().Get(ctx, i.ID); got == nil || !got.RevokedAt.Equal(t1) {
		t.Errorf("second Revoke moved revoked_at to %v, want %v kept", got.RevokedAt, t1)
	}
	if err := st.Identities().Revoke(ctx, "idn_conf_missing", t1); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Revoke(missing): %v, want ErrNotFound", err)
	}
}

func authConfIdentityTouch(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	p := newPrincipal(t, st, "prn_conf_idn_touch", authT0)
	i := newIdentity(t, st, p.ID, "touch-me", authT0)
	lastUsed := func() *time.Time {
		got, err := st.Identities().Get(ctx, i.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		return got.LastUsedAt
	}
	t1 := authT0.Add(time.Hour)
	if err := st.Identities().Touch(ctx, i.ID, t1); err != nil {
		t.Fatalf("first Touch: %v", err)
	}
	if got := lastUsed(); got == nil || !got.Equal(t1) {
		t.Errorf("after first Touch last_used = %v, want %v", got, t1)
	}
	if err := st.Identities().Touch(ctx, i.ID, t1.Add(30*time.Second)); err != nil {
		t.Fatalf("Touch within a minute: %v", err)
	}
	if got := lastUsed(); got == nil || !got.Equal(t1) {
		t.Errorf("Touch within a minute moved last_used to %v, want %v kept", got, t1)
	}
	t2 := t1.Add(storage.IdentityTouchEvery)
	if err := st.Identities().Touch(ctx, i.ID, t2); err != nil {
		t.Fatalf("Touch after a minute: %v", err)
	}
	if got := lastUsed(); got == nil || !got.Equal(t2) {
		t.Errorf("Touch after a minute left last_used at %v, want %v", got, t2)
	}
	if err := st.Identities().Revoke(ctx, i.ID, t2); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := st.Identities().Touch(ctx, i.ID, t2.Add(time.Hour)); err != nil {
		t.Errorf("Touch(revoked): %v, want no error", err)
	}
	if got := lastUsed(); got == nil || !got.Equal(t2) {
		t.Errorf("Touch(revoked) moved last_used to %v, want %v kept", got, t2)
	}
	if err := st.Identities().Touch(ctx, "idn_conf_missing", t2); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Touch(missing): %v, want ErrNotFound", err)
	}
}

func authConfTicketRedeem(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	p := newPrincipal(t, st, "prn_conf_tkt", authT0)
	secret, err := st.Tickets().Issue(ctx, storage.TicketIssue{Kind: storage.TicketKindMagicLink,
		Payload: []byte(`{"audiences":["work"]}`), TTL: 10 * time.Minute, CreatedBy: storage.OwnerPrincipal,
		PrincipalID: p.ID, Now: authT0})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if raw, err := base64.RawURLEncoding.DecodeString(secret); err != nil || len(raw) != 32 {
		t.Errorf("secret %q: %d bytes, %v; want 32 random bytes base64url", secret, len(raw), err)
	}
	got, err := st.Tickets().Redeem(ctx, storage.TicketKindMagicLink, secret, authT0.Add(time.Minute))
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if got.Kind != storage.TicketKindMagicLink || got.PrincipalID != p.ID || string(got.Payload) != `{"audiences":["work"]}` ||
		got.CreatedBy != storage.OwnerPrincipal || !got.CreatedAt.Equal(authT0) || !got.ExpiresAt.Equal(authT0.Add(10*time.Minute)) ||
		got.RedeemedAt == nil || !got.RedeemedAt.Equal(authT0.Add(time.Minute)) {
		t.Errorf("Redeem = %+v, want the issued fields redeemed at %v", got, authT0.Add(time.Minute))
	}
	if _, err := st.Tickets().Redeem(ctx, storage.TicketKindMagicLink, secret, authT0.Add(2*time.Minute)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("second Redeem: %v, want ErrNotFound", err)
	}

	invite, err := st.Tickets().Issue(ctx, storage.TicketIssue{Kind: storage.TicketKindInvite, TTL: time.Hour,
		CreatedBy: storage.OwnerPrincipal, Now: authT0})
	if err != nil {
		t.Fatalf("Issue invite: %v", err)
	}
	got, err = st.Tickets().Redeem(ctx, storage.TicketKindInvite, invite, authT0.Add(time.Minute))
	if err != nil {
		t.Fatalf("Redeem invite: %v", err)
	}
	if got.PrincipalID != "" || string(got.Payload) != "{}" {
		t.Errorf("invite = %+v, want no principal and an empty payload", got)
	}
}

func authConfTicketKind(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	secret, err := st.Tickets().Issue(ctx, storage.TicketIssue{Kind: storage.TicketKindInvite, TTL: time.Hour,
		CreatedBy: storage.OwnerPrincipal, Now: authT0})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := st.Tickets().Redeem(ctx, storage.TicketKindMagicLink, secret, authT0.Add(time.Minute)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Redeem with the wrong kind: %v, want ErrNotFound", err)
	}
	if _, err := st.Tickets().Redeem(ctx, storage.TicketKindInvite, secret, authT0.Add(time.Minute)); err != nil {
		t.Errorf("Redeem with the right kind after a mismatch: %v, want the ticket still redeemable", err)
	}
}

func authConfTicketExpiry(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	secret, err := st.Tickets().Issue(ctx, storage.TicketIssue{Kind: storage.TicketKindInvite, TTL: 10 * time.Minute,
		CreatedBy: storage.OwnerPrincipal, Now: authT0})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := st.Tickets().Redeem(ctx, storage.TicketKindInvite, secret, authT0.Add(10*time.Minute)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Redeem at expiry: %v, want ErrNotFound", err)
	}
	if _, err := st.Tickets().Redeem(ctx, storage.TicketKindInvite, secret, authT0.Add(time.Hour)); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Redeem after expiry: %v, want ErrNotFound", err)
	}
}

func authConfTicketUnknown(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	for _, secret := range []string{"", "not-a-ticket", strings.Repeat("A", 43)} {
		if _, err := st.Tickets().Redeem(ctx, storage.TicketKindInvite, secret, authT0); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Redeem(%q): %v, want ErrNotFound", secret, err)
		}
	}
}

func authConfTicketInactivePrincipal(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		status func(id string) error
	}{
		{"revoked", func(id string) error { return st.Principals().Revoke(ctx, id, authT0.Add(time.Minute)) }},
		{"suspended", func(id string) error { return st.Principals().Suspend(ctx, id) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := newPrincipal(t, st, "prn_conf_tkt_"+c.name, authT0)
			secret, err := st.Tickets().Issue(ctx, storage.TicketIssue{Kind: storage.TicketKindMagicLink, TTL: time.Hour,
				CreatedBy: storage.OwnerPrincipal, PrincipalID: p.ID, Now: authT0})
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			if err := c.status(p.ID); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if _, err := st.Tickets().Redeem(ctx, storage.TicketKindMagicLink, secret, authT0.Add(2*time.Minute)); !errors.Is(err, storage.ErrPrincipalInactive) {
				t.Errorf("Redeem for a %s principal: %v, want ErrPrincipalInactive", c.name, err)
			}
		})
	}
}

func authConfTicketIssueRefusals(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	for name, issue := range map[string]storage.TicketIssue{
		"unknown kind": {Kind: "reset", TTL: time.Hour, CreatedBy: storage.OwnerPrincipal},
		"zero ttl":     {Kind: storage.TicketKindInvite, CreatedBy: storage.OwnerPrincipal},
		"no issuer":    {Kind: storage.TicketKindInvite, TTL: time.Hour},
	} {
		if _, err := st.Tickets().Issue(ctx, issue); err == nil {
			t.Errorf("%s: issued, want a refusal", name)
		}
	}
	for name, issue := range map[string]storage.TicketIssue{
		"unknown issuer":    {Kind: storage.TicketKindInvite, TTL: time.Hour, CreatedBy: "prn_conf_missing"},
		"unknown principal": {Kind: storage.TicketKindMagicLink, TTL: time.Hour, CreatedBy: storage.OwnerPrincipal, PrincipalID: "prn_conf_missing"},
	} {
		if _, err := st.Tickets().Issue(ctx, issue); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("%s: %v, want ErrNotFound", name, err)
		}
	}
}

// authConfTicketConcurrency redeems one secret from many goroutines at
// once: exactly one redemption succeeds, every other one is
// ErrNotFound. A read-then-write Redeem lets several through.
func authConfTicketConcurrency(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	const workers = 16
	for round := range 3 {
		secret, err := st.Tickets().Issue(ctx, storage.TicketIssue{Kind: storage.TicketKindInvite, TTL: time.Hour,
			CreatedBy: storage.OwnerPrincipal, Now: authT0})
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		start := make(chan struct{})
		results := make(chan error, workers)
		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := st.Tickets().Redeem(ctx, storage.TicketKindInvite, secret, authT0.Add(time.Minute))
				results <- err
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		won, lost := 0, 0
		for err := range results {
			switch {
			case err == nil:
				won++
			case errors.Is(err, storage.ErrNotFound):
				lost++
			default:
				t.Errorf("round %d: unexpected redeem error: %v", round, err)
			}
		}
		if won != 1 || lost != workers-1 {
			t.Errorf("round %d: %d redemptions succeeded and %d were refused; want exactly 1 and %d", round, won, lost, workers-1)
		}
	}
}

func authConfKVPutGetDelete(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	kv := st.KV()
	if _, ok, err := kv.Get(ctx, "conf/missing"); ok || err != nil {
		t.Errorf("Get(missing) = ok %v, %v; want absent", ok, err)
	}
	if err := kv.Put(ctx, "conf/a", []byte("one")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if v, ok, err := kv.Get(ctx, "conf/a"); !ok || err != nil || string(v) != "one" {
		t.Errorf("Get = %q, %v, %v; want one", v, ok, err)
	}
	if err := kv.Put(ctx, "conf/a", []byte("two")); err != nil {
		t.Fatalf("Put again: %v", err)
	}
	if v, _, _ := kv.Get(ctx, "conf/a"); string(v) != "two" {
		t.Errorf("Get after overwrite = %q, want two", v)
	}
	if err := kv.Put(ctx, "conf/empty", nil); err != nil {
		t.Fatalf("Put(nil): %v", err)
	}
	if v, ok, err := kv.Get(ctx, "conf/empty"); !ok || err != nil || len(v) != 0 {
		t.Errorf("Get(empty) = %q, %v, %v; want present and empty", v, ok, err)
	}
	if err := kv.Delete(ctx, "conf/a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := kv.Get(ctx, "conf/a"); ok {
		t.Error("Get after Delete still finds the key")
	}
	if err := kv.Delete(ctx, "conf/a"); err != nil {
		t.Errorf("Delete(missing): %v, want no error", err)
	}
	if err := kv.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if _, _, err := kv.Get(ctx, "conf/empty"); err != nil {
		t.Errorf("Get after Close: %v; the driver owns the connection, so the store stays usable", err)
	}
}

func authConfKVList(t *testing.T, st storage.AuthStore) {
	ctx := context.Background()
	kv := st.KV()
	for _, k := range []string{"list/b", "list/a", "list_c", "listz", "other/a"} {
		if err := kv.Put(ctx, k, []byte(k)); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}
	keys, err := kv.List(ctx, "list/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	assertIDs(t, "List(list/)", keys, []string{"list/a", "list/b"})
	keys, err = kv.List(ctx, "list_")
	if err != nil {
		t.Fatalf("List(list_): %v", err)
	}
	assertIDs(t, "List(list_) treats _ literally", keys, []string{"list_c"})
	all, err := kv.List(ctx, "")
	if err != nil || !slices.IsSorted(all) || !slices.Contains(all, "other/a") {
		t.Errorf("List(\"\") = %v, %v; want every key, sorted", all, err)
	}
	none, err := kv.List(ctx, "nothing/")
	if err != nil || len(none) != 0 {
		t.Errorf("List(nothing/) = %v, %v; want empty", none, err)
	}
}

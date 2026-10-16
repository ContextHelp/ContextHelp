package sharing_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// clsStore is a SQLite registry with one person, one API key, an
// audience the person belongs to and the base policy.
type clsStore struct {
	t        *testing.T
	st       storage.SharingStore
	person   string
	identity string
	audience *storage.Audience
	policy   *storage.SharePolicy
}

func newClsStore(t *testing.T) *clsStore {
	t.Helper()
	ctx := context.Background()
	drv := storageutil.NewTestDriver(t)
	f := &clsStore{t: t, st: drv.Sharing()}
	for _, id := range []string{"prn_cls_a", "prn_cls_b"} {
		if err := drv.Auth().Principals().Create(ctx, &storage.Principal{ID: id, Kind: storage.PrincipalKindPerson, DisplayName: id, CreatedAt: clsNow}); err != nil {
			t.Fatal(err)
		}
	}
	f.person = "prn_cls_a"
	key := &storage.Identity{PrincipalID: f.person, Method: storage.IdentityMethodAPIKey, Subject: "cls-key", CreatedAt: clsNow}
	if err := drv.Auth().Identities().Create(ctx, key); err != nil {
		t.Fatal(err)
	}
	f.identity = key.ID
	f.audience = &storage.Audience{Name: "cls-team", CreatedAt: clsNow}
	if err := f.st.Audiences().Create(ctx, f.audience); err != nil {
		t.Fatal(err)
	}
	f.policy = f.addPolicy(clsBase())
	f.join(f.person, clsAt(time.Hour))
	return f
}

func (f *clsStore) addPolicy(p sharing.Policy) *storage.SharePolicy {
	f.t.Helper()
	spec, err := p.Spec()
	if err != nil {
		f.t.Fatal(err)
	}
	row := &storage.SharePolicy{Name: p.Name, Spec: spec, UpdatedAt: clsNow}
	if err := f.st.Policies().Create(context.Background(), row); err != nil {
		f.t.Fatal(err)
	}
	return row
}

func (f *clsStore) join(principal string, expires *time.Time) {
	f.t.Helper()
	m := &storage.AudienceMember{AudienceID: f.audience.ID, PrincipalID: principal, ExpiresAt: expires, AddedBy: storage.OwnerPrincipal, AddedAt: clsNow}
	if err := f.st.Audiences().AddMember(context.Background(), m); err != nil {
		f.t.Fatal(err)
	}
}

func (f *clsStore) grant() *storage.ShareGrant {
	f.t.Helper()
	g := &storage.ShareGrant{AudienceID: f.audience.ID, PolicyID: f.policy.ID, CreatedBy: storage.OwnerPrincipal, CreatedAt: clsNow}
	if err := f.st.Grants().Create(context.Background(), g); err != nil {
		f.t.Fatal(err)
	}
	return g
}

func (f *clsStore) classify(c sharing.Change) (sharing.Classification, sharing.State) {
	f.t.Helper()
	return sharing.ClassifyRead(context.Background(), f.st, c, clsNow)
}

func (f *clsStore) fingerprint(c sharing.Change) string {
	f.t.Helper()
	s, err := sharing.ReadState(context.Background(), f.st, c, clsNow)
	if err != nil {
		f.t.Fatal(err)
	}
	fp, err := sharing.Fingerprint(c, s)
	if err != nil {
		f.t.Fatal(err)
	}
	return fp
}

func (f *clsStore) want(c sharing.Change, class sharing.Class, reason string) {
	f.t.Helper()
	got, _ := f.classify(c)
	if got.Class != class || got.Reason != reason {
		f.t.Errorf("%s: got %s/%s (%s), want %s/%s", c.Action(), got.Class, got.Reason, got.Detail, class, reason)
	}
}

// TestReadState_ClassifiesStoredRegistry runs the route table against
// state read from a store.
func TestReadState_ClassifiesStoredRegistry(t *testing.T) {
	f := newClsStore(t)
	ctx := context.Background()
	wider := clsWith(func(p *sharing.Policy) { p.Capabilities.Export = true })
	narrower := clsWith(func(p *sharing.Policy) { p.Data += ";source==mail" })

	f.want(sharing.MemberPut{AudienceID: f.audience.ID, PrincipalID: "prn_cls_b"}, sharing.Widening, sharing.ReasonMembershipNew)
	f.want(sharing.MemberPut{AudienceID: f.audience.ID, PrincipalID: f.person, ExpiresAt: clsAt(2 * time.Hour)}, sharing.Widening, sharing.ReasonExpiryExtended)
	f.want(sharing.MemberPut{AudienceID: f.audience.ID, PrincipalID: f.person, ExpiresAt: clsAt(time.Minute)}, sharing.Narrowing, sharing.ReasonExpiryShortened)
	f.want(sharing.MemberPut{AudienceID: f.audience.ID, PrincipalID: f.person, ExpiresAt: clsAt(time.Hour)}, sharing.Neutral, sharing.ReasonExpiryUnchanged)

	// No grant yet: a policy edit reaches no viewer.
	f.want(sharing.PolicyUpdate{PolicyID: f.policy.ID, Policy: wider}, sharing.Neutral, sharing.ReasonNoGrantInForce)
	f.want(sharing.GrantCreate{AudienceID: f.audience.ID, PolicyID: f.policy.ID}, sharing.Widening, sharing.ReasonGrantCreated)
	g := f.grant()
	f.want(sharing.PolicyUpdate{PolicyID: f.policy.ID, Policy: wider}, sharing.Widening, sharing.ReasonSpecWider)
	f.want(sharing.PolicyUpdate{PolicyID: f.policy.ID, Policy: narrower}, sharing.Narrowing, sharing.ReasonSpecNarrower)
	got, _ := f.classify(sharing.PolicyUpdate{PolicyID: f.policy.ID, Policy: wider})
	if len(got.Beneficiaries) != 1 || got.Beneficiaries[0] != f.person {
		t.Errorf("policy update beneficiaries %v, want [%s]", got.Beneficiaries, f.person)
	}
	if err := f.st.Grants().Revoke(ctx, g.ID, clsNow.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	f.want(sharing.PolicyUpdate{PolicyID: f.policy.ID, Policy: wider}, sharing.Neutral, sharing.ReasonNoGrantInForce)

	if err := f.st.SetDenyClasses(ctx, f.person, []string{"personal"}); err != nil {
		t.Fatal(err)
	}
	f.want(sharing.CeilingSet{PrincipalID: f.person, DenyClasses: []string{"personal", "health"}}, sharing.Narrowing, sharing.ReasonCeilingRaised)
	f.want(sharing.CeilingSet{PrincipalID: f.person}, sharing.Widening, sharing.ReasonCeilingLowered)

	other := f.addPolicy(clsWith(func(p *sharing.Policy) { p.Name = "cls-narrow"; p.Data += ";source==mail" }))
	open := f.addPolicy(clsWith(func(p *sharing.Policy) { p.Name = "cls-open"; p.Capabilities.Export = true }))
	bind := func(policyID string) sharing.KeyPolicyBind {
		return sharing.KeyPolicyBind{KeyID: "key_cls", IdentityID: f.identity, PrincipalID: f.person, PolicyID: policyID}
	}
	f.want(bind(f.policy.ID), sharing.Narrowing, sharing.ReasonKeyBound)
	if err := f.st.BindIdentityPolicy(ctx, f.identity, f.policy.ID); err != nil {
		t.Fatal(err)
	}
	f.want(bind(f.policy.ID), sharing.Neutral, sharing.ReasonKeyBindingUnchanged)
	f.want(bind(other.ID), sharing.Narrowing, sharing.ReasonSpecNarrower)
	f.want(bind(open.ID), sharing.Widening, sharing.ReasonSpecWider)
	f.want(bind(""), sharing.Widening, sharing.ReasonKeyUnbound)
}

func TestClassifyRead_ReadErrorIsWidening(t *testing.T) {
	f := newClsStore(t)
	for _, c := range []sharing.Change{
		sharing.PolicyUpdate{PolicyID: "pol_missing", Policy: clsBase()},
		sharing.CeilingSet{PrincipalID: "prn_missing"},
		sharing.KeyPolicyBind{IdentityID: "idn_missing", PolicyID: f.policy.ID},
		sharing.KeyPolicyBind{IdentityID: f.identity, PolicyID: "pol_missing"},
		sharing.GrantCreate{AudienceID: f.audience.ID, PolicyID: "pol_missing"},
	} {
		got, _ := f.classify(c)
		if got.Class != sharing.Widening || got.Reason != sharing.ReasonStateUnreadable {
			t.Errorf("%#v: got %s/%s, want widening/%s", c, got.Class, got.Reason, sharing.ReasonStateUnreadable)
		}
	}
	if _, err := sharing.ReadState(context.Background(), f.st, clsUnknown{}, clsNow); err == nil {
		t.Error("ReadState of an unknown change must fail")
	}
	if _, err := sharing.ReadState(context.Background(), nil, sharing.GrantRevoke{}, clsNow); err == nil {
		t.Error("ReadState without a registry must fail")
	}
}

// TestFingerprint_TracksPreconditions: the fingerprint is stable while
// the rows a change depends on are, and moves when any of them does.
func TestFingerprint_TracksPreconditions(t *testing.T) {
	f := newClsStore(t)
	ctx := context.Background()

	member := sharing.MemberPut{AudienceID: f.audience.ID, PrincipalID: f.person, ExpiresAt: clsAt(2 * time.Hour)}
	before := f.fingerprint(member)
	if again := f.fingerprint(member); again != before {
		t.Fatalf("membership fingerprint not stable: %s then %s", before, again)
	}
	f.join("prn_cls_b", nil)
	if got := f.fingerprint(member); got != before {
		t.Error("another principal's membership moved this membership's fingerprint")
	}
	f.join(f.person, clsAt(3*time.Hour))
	if got := f.fingerprint(member); got == before {
		t.Error("a changed expiry left the membership fingerprint unchanged")
	}

	update := sharing.PolicyUpdate{PolicyID: f.policy.ID, Policy: clsBase()}
	before = f.fingerprint(update)
	g := f.grant()
	afterGrant := f.fingerprint(update)
	if afterGrant == before {
		t.Error("a new grant left the policy update fingerprint unchanged")
	}
	if err := f.st.Grants().Revoke(ctx, g.ID, clsNow); err != nil {
		t.Fatal(err)
	}
	if got := f.fingerprint(update); got == afterGrant {
		t.Error("a revoked grant left the policy update fingerprint unchanged")
	}
	create := sharing.GrantCreate{AudienceID: f.audience.ID, PolicyID: f.policy.ID}
	before = f.fingerprint(create)
	spec, _ := clsBase().Spec()
	if _, err := f.st.Policies().Update(ctx, f.policy.ID, spec, clsNow); err != nil {
		t.Fatal(err)
	}
	if got := f.fingerprint(create); got == before {
		t.Error("a policy version bump left the grant create fingerprint unchanged")
	}

	ceiling := sharing.CeilingSet{PrincipalID: f.person}
	before = f.fingerprint(ceiling)
	if err := f.st.SetDenyClasses(ctx, f.person, []string{"personal"}); err != nil {
		t.Fatal(err)
	}
	if got := f.fingerprint(ceiling); got == before {
		t.Error("a changed ceiling left its fingerprint unchanged")
	}

	bind := sharing.KeyPolicyBind{IdentityID: f.identity, PrincipalID: f.person, PolicyID: f.policy.ID}
	before = f.fingerprint(bind)
	if err := f.st.BindIdentityPolicy(ctx, f.identity, f.policy.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.fingerprint(bind); got == before {
		t.Error("a changed binding left the key policy fingerprint unchanged")
	}
}

func TestFingerprint_Pure(t *testing.T) {
	row := clsRow(t, "pol_a", 2, clsBase())
	g1, g2 := clsGrant("grt_1", "aud_x", "pol_a"), clsGrant("grt_2", "aud_y", "pol_a")
	update := sharing.PolicyUpdate{PolicyID: "pol_a", Policy: clsBase()}
	fp := func(s sharing.State) string {
		t.Helper()
		out, err := sharing.Fingerprint(update, s)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	a := fp(sharing.State{Now: clsNow, Policy: &sharing.PolicyState{Row: row, Grants: []*storage.ShareGrant{g1, g2}}})
	b := fp(sharing.State{Now: clsNow.Add(time.Hour), Policy: &sharing.PolicyState{Row: row, Grants: []*storage.ShareGrant{g2, g1},
		Members: map[string][]*storage.AudienceMember{"aud_x": {clsMember("aud_x", "prn_m", nil)}}}})
	if a != b {
		t.Errorf("fingerprint depends on the clock, grant order or members: %s vs %s", a, b)
	}
	moved := *row
	moved.Version = 3
	if c := fp(sharing.State{Now: clsNow, Policy: &sharing.PolicyState{Row: &moved, Grants: []*storage.ShareGrant{g1, g2}}}); c == a {
		t.Error("a version bump left the fingerprint unchanged")
	}
	local := clsNow.In(time.FixedZone("x", 3600))
	m1 := sharing.State{Membership: &sharing.MembershipState{Current: clsMember("aud_x", "prn_p", &clsNow)}}
	m2 := sharing.State{Membership: &sharing.MembershipState{Current: clsMember("aud_x", "prn_p", &local)}}
	put := sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p"}
	f1, err1 := sharing.Fingerprint(put, m1)
	f2, err2 := sharing.Fingerprint(put, m2)
	if err1 != nil || err2 != nil || f1 != f2 {
		t.Errorf("same instant in two zones: %s, %s (%v, %v)", f1, f2, err1, err2)
	}
	if none, _ := sharing.Fingerprint(put, sharing.State{Membership: &sharing.MembershipState{}}); none == f1 {
		t.Error("no membership and a membership share a fingerprint")
	}
	if a2, _ := sharing.Fingerprint(sharing.PolicyCreate{Policy: clsBase()}, sharing.State{}); a2 == "" {
		t.Error("a stateless change has a fingerprint too")
	}
}

func TestFingerprint_StateNotReadIsAnError(t *testing.T) {
	for _, c := range []sharing.Change{
		sharing.MemberPut{}, sharing.PolicyUpdate{}, sharing.GrantCreate{}, sharing.CeilingSet{}, sharing.KeyPolicyBind{}, clsUnknown{},
	} {
		if _, err := sharing.Fingerprint(c, sharing.State{Now: clsNow}); err == nil {
			t.Errorf("%T: fingerprint of state never read must fail", c)
		}
	}
	if _, err := sharing.Fingerprint(nil, sharing.State{}); err == nil {
		t.Error("fingerprint of a nil change must fail")
	}
}

// A key bound to the policy counts like a grant in force: its
// principal's view is intersected with the policy, so an edit moves it.
func TestReadState_BoundKeyIsInForceForAPolicyUpdate(t *testing.T) {
	f := newClsStore(t)
	wider := clsWith(func(p *sharing.Policy) { p.Capabilities.Export = true })
	narrower := clsWith(func(p *sharing.Policy) { p.Data += ";source==mail" })
	edit := sharing.PolicyUpdate{PolicyID: f.policy.ID, Policy: wider}
	f.want(edit, sharing.Neutral, sharing.ReasonNoGrantInForce)
	unbound := f.fingerprint(edit)

	if err := f.st.BindIdentityPolicy(context.Background(), f.identity, f.policy.ID); err != nil {
		t.Fatal(err)
	}
	f.want(edit, sharing.Widening, sharing.ReasonSpecWider)
	f.want(sharing.PolicyUpdate{PolicyID: f.policy.ID, Policy: narrower}, sharing.Narrowing, sharing.ReasonSpecNarrower)
	got, _ := f.classify(edit)
	if len(got.Beneficiaries) != 1 || got.Beneficiaries[0] != f.person {
		t.Errorf("beneficiaries %v, want the key's principal [%s]", got.Beneficiaries, f.person)
	}
	if f.fingerprint(edit) == unbound {
		t.Error("binding a key left the fingerprint unchanged")
	}
}

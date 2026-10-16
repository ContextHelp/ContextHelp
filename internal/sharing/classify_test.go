package sharing_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// clsNow is the classification clock.
var clsNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func clsAt(d time.Duration) *time.Time {
	at := clsNow.Add(d)
	return &at
}

// clsRow stores p as a policy row with id and version.
func clsRow(t *testing.T, id string, version int, p sharing.Policy) *storage.SharePolicy {
	t.Helper()
	spec, err := p.Spec()
	if err != nil {
		t.Fatal(err)
	}
	return &storage.SharePolicy{ID: id, Name: p.Name, Version: version, Spec: spec}
}

func clsMember(audience, principal string, expires *time.Time) *storage.AudienceMember {
	return &storage.AudienceMember{AudienceID: audience, PrincipalID: principal, ExpiresAt: expires, AddedBy: storage.OwnerPrincipal}
}

func clsGrant(id, audience, policy string) *storage.ShareGrant {
	return &storage.ShareGrant{ID: id, AudienceID: audience, PolicyID: policy, CreatedBy: storage.OwnerPrincipal}
}

// clsPolicyState is the state of an update of policy pol_a (stored as
// clsBase) with grants of it.
func clsPolicyState(t *testing.T, grants ...*storage.ShareGrant) sharing.State {
	return sharing.State{Now: clsNow, Policy: &sharing.PolicyState{
		Row:    clsRow(t, "pol_a", 3, clsBase()),
		Grants: grants,
		Members: map[string][]*storage.AudienceMember{
			"aud_x": {clsMember("aud_x", "prn_m1", nil), clsMember("aud_x", "prn_m2", clsAt(-time.Hour))},
			"aud_y": {clsMember("aud_y", "prn_m3", nil)},
		},
	}}
}

func clsKeyState(t *testing.T, current, target *storage.SharePolicy) sharing.State {
	ks := &sharing.KeyBindingState{IdentityID: "idn_k", Current: current, Target: target}
	if current != nil {
		ks.CurrentPolicyID = current.ID
	}
	return sharing.State{Now: clsNow, KeyBinding: ks}
}

// TestClassify_RouteTable follows ADR-083 §10 B, "Route
// classification", row by row.
func TestClassify_RouteTable(t *testing.T) {
	narrower := clsWith(func(p *sharing.Policy) { p.Name = "narrow"; p.Data += ";source==mail" })
	wider := clsWith(func(p *sharing.Policy) { p.Name = "wide"; p.Capabilities.Export = true })
	same := clsWith(func(p *sharing.Policy) { p.Name = "same" })
	inForce := clsGrant("grt_1", "aud_x", "pol_a")
	revoked := clsGrant("grt_2", "aud_y", "pol_a")
	revoked.RevokedAt = clsAt(-time.Minute)
	expired := clsGrant("grt_3", "aud_y", "pol_a")
	expired.ExpiresAt = clsAt(-time.Minute)

	cases := []struct {
		name   string
		change sharing.Change
		state  func(t *testing.T) sharing.State
		class  sharing.Class
		reason string
	}{
		{"POST /share/audiences", sharing.AudienceCreate{Name: "team"}, nil, sharing.Neutral, sharing.ReasonEmptyAudience},

		{"PUT member, no membership", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p", ExpiresAt: clsAt(time.Hour)},
			func(*testing.T) sharing.State {
				return sharing.State{Now: clsNow, Membership: &sharing.MembershipState{}}
			}, sharing.Widening, sharing.ReasonMembershipNew},
		{"PUT member, expiry later", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p", ExpiresAt: clsAt(2 * time.Hour)},
			clsMembership(clsAt(time.Hour)), sharing.Widening, sharing.ReasonExpiryExtended},
		{"PUT member, expiry removed", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p"},
			clsMembership(clsAt(time.Hour)), sharing.Widening, sharing.ReasonExpiryRemoved},
		{"PUT member, expired membership renewed", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p", ExpiresAt: clsAt(time.Hour)},
			clsMembership(clsAt(-time.Hour)), sharing.Widening, sharing.ReasonExpiryExtended},
		{"PUT member, expiry earlier", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p", ExpiresAt: clsAt(time.Hour)},
			clsMembership(clsAt(2 * time.Hour)), sharing.Narrowing, sharing.ReasonExpiryShortened},
		{"PUT member, expiry set on an open membership", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p", ExpiresAt: clsAt(time.Hour)},
			clsMembership(nil), sharing.Narrowing, sharing.ReasonExpiryShortened},
		{"PUT member, expiry equal", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p", ExpiresAt: clsAt(time.Hour)},
			clsMembership(clsAt(time.Hour)), sharing.Neutral, sharing.ReasonExpiryUnchanged},
		{"PUT member, both open", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p"},
			clsMembership(nil), sharing.Neutral, sharing.ReasonExpiryUnchanged},

		{"DELETE member", sharing.MemberRemove{AudienceID: "aud_x", PrincipalID: "prn_p"}, nil, sharing.Narrowing, sharing.ReasonMembershipRemoved},

		{"POST /share/policies", sharing.PolicyCreate{Policy: clsBase()}, nil, sharing.Neutral, sharing.ReasonPolicyNotGranted},
		{"POST /share/templates/{name}", sharing.TemplateInstall{Name: sharing.TemplateColleagues}, nil, sharing.Neutral, sharing.ReasonPolicyNotGranted},

		{"PUT policy, no grant", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: wider},
			func(t *testing.T) sharing.State { return clsPolicyState(t) }, sharing.Neutral, sharing.ReasonNoGrantInForce},
		{"PUT policy, only revoked and expired grants", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: wider},
			func(t *testing.T) sharing.State { return clsPolicyState(t, revoked, expired) }, sharing.Neutral, sharing.ReasonNoGrantInForce},
		{"PUT policy, grant in force, narrower", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: narrower},
			func(t *testing.T) sharing.State { return clsPolicyState(t, inForce, revoked) }, sharing.Narrowing, sharing.ReasonSpecNarrower},
		{"PUT policy, grant in force, same spec", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: same},
			func(t *testing.T) sharing.State { return clsPolicyState(t, inForce) }, sharing.Narrowing, sharing.ReasonSpecEqual},
		{"PUT policy, grant in force, wider", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: wider},
			func(t *testing.T) sharing.State { return clsPolicyState(t, inForce) }, sharing.Widening, sharing.ReasonSpecWider},
		{"PUT policy, grant with a future expiry", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: wider},
			func(t *testing.T) sharing.State {
				g := clsGrant("grt_4", "aud_x", "pol_a")
				g.ExpiresAt = clsAt(time.Hour)
				return clsPolicyState(t, g)
			}, sharing.Widening, sharing.ReasonSpecWider},

		{"POST /share/grants", sharing.GrantCreate{AudienceID: "aud_x", PolicyID: "pol_a"},
			func(t *testing.T) sharing.State {
				return sharing.State{Now: clsNow, Policy: &sharing.PolicyState{Row: clsRow(t, "pol_a", 1, clsBase())},
					Audience: &sharing.AudienceState{ID: "aud_x"}}
			}, sharing.Widening, sharing.ReasonGrantCreated},
		{"POST /share/grants, narrowest policy and expiry", sharing.GrantCreate{AudienceID: "aud_x", PolicyID: "pol_a", ExpiresAt: clsAt(time.Second)},
			func(t *testing.T) sharing.State {
				nothing := clsWith(func(p *sharing.Policy) { p.Fields = nil; p.Capabilities = sharing.Capabilities{} })
				return sharing.State{Now: clsNow, Policy: &sharing.PolicyState{Row: clsRow(t, "pol_a", 1, nothing)},
					Audience: &sharing.AudienceState{ID: "aud_x"}}
			}, sharing.Widening, sharing.ReasonGrantCreated},
		{"DELETE /share/grants/{g}", sharing.GrantRevoke{GrantID: "grt_1"}, nil, sharing.Narrowing, sharing.ReasonGrantRevoked},

		{"PUT ceiling, superset", sharing.CeilingSet{PrincipalID: "prn_p", DenyClasses: []string{"personal", "health"}},
			clsCeiling("personal"), sharing.Narrowing, sharing.ReasonCeilingRaised},
		{"PUT ceiling, from empty", sharing.CeilingSet{PrincipalID: "prn_p", DenyClasses: []string{"personal"}},
			clsCeiling(), sharing.Narrowing, sharing.ReasonCeilingRaised},
		{"PUT ceiling, equal", sharing.CeilingSet{PrincipalID: "prn_p", DenyClasses: []string{"personal", " health", "personal"}},
			clsCeiling("health", "personal"), sharing.Neutral, sharing.ReasonCeilingUnchanged},
		{"PUT ceiling, both empty", sharing.CeilingSet{PrincipalID: "prn_p"},
			clsCeiling(), sharing.Neutral, sharing.ReasonCeilingUnchanged},
		{"PUT ceiling, class removed", sharing.CeilingSet{PrincipalID: "prn_p", DenyClasses: []string{"personal"}},
			clsCeiling("health", "personal"), sharing.Widening, sharing.ReasonCeilingLowered},
		{"PUT ceiling, cleared", sharing.CeilingSet{PrincipalID: "prn_p"},
			clsCeiling("personal"), sharing.Widening, sharing.ReasonCeilingLowered},
		{"PUT ceiling, class swapped", sharing.CeilingSet{PrincipalID: "prn_p", DenyClasses: []string{"health"}},
			clsCeiling("personal"), sharing.Widening, sharing.ReasonCeilingLowered},

		{"PUT key policy, unbound to bound", sharing.KeyPolicyBind{KeyID: "key_k", IdentityID: "idn_k", PrincipalID: "prn_p", PolicyID: "pol_n"},
			func(t *testing.T) sharing.State { return clsKeyState(t, nil, clsRow(t, "pol_n", 1, wider)) }, sharing.Narrowing, sharing.ReasonKeyBound},
		{"PUT key policy, bound to unbound", sharing.KeyPolicyBind{KeyID: "key_k", IdentityID: "idn_k", PrincipalID: "prn_p"},
			func(t *testing.T) sharing.State { return clsKeyState(t, clsRow(t, "pol_a", 1, clsBase()), nil) }, sharing.Widening, sharing.ReasonKeyUnbound},
		{"PUT key policy, rebound narrower", sharing.KeyPolicyBind{KeyID: "key_k", IdentityID: "idn_k", PrincipalID: "prn_p", PolicyID: "pol_n"},
			func(t *testing.T) sharing.State {
				return clsKeyState(t, clsRow(t, "pol_a", 1, clsBase()), clsRow(t, "pol_n", 1, narrower))
			}, sharing.Narrowing, sharing.ReasonSpecNarrower},
		{"PUT key policy, rebound to an equal spec", sharing.KeyPolicyBind{KeyID: "key_k", IdentityID: "idn_k", PrincipalID: "prn_p", PolicyID: "pol_n"},
			func(t *testing.T) sharing.State {
				return clsKeyState(t, clsRow(t, "pol_a", 1, clsBase()), clsRow(t, "pol_n", 1, same))
			}, sharing.Narrowing, sharing.ReasonSpecEqual},
		{"PUT key policy, rebound wider", sharing.KeyPolicyBind{KeyID: "key_k", IdentityID: "idn_k", PrincipalID: "prn_p", PolicyID: "pol_n"},
			func(t *testing.T) sharing.State {
				return clsKeyState(t, clsRow(t, "pol_a", 1, clsBase()), clsRow(t, "pol_n", 1, wider))
			}, sharing.Widening, sharing.ReasonSpecWider},
		{"PUT key policy, same policy again", sharing.KeyPolicyBind{KeyID: "key_k", IdentityID: "idn_k", PrincipalID: "prn_p", PolicyID: "pol_a"},
			func(t *testing.T) sharing.State {
				row := clsRow(t, "pol_a", 1, clsBase())
				return clsKeyState(t, row, row)
			}, sharing.Neutral, sharing.ReasonKeyBindingUnchanged},
		{"PUT key policy, unbound stays unbound", sharing.KeyPolicyBind{KeyID: "key_k", IdentityID: "idn_k", PrincipalID: "prn_p"},
			func(t *testing.T) sharing.State { return clsKeyState(t, nil, nil) }, sharing.Neutral, sharing.ReasonKeyBindingUnchanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s sharing.State
			if tc.state != nil {
				s = tc.state(t)
			}
			got := sharing.Classify(tc.change, s)
			if got.Class != tc.class || got.Reason != tc.reason {
				t.Errorf("got %s/%s (%s), want %s/%s", got.Class, got.Reason, got.Detail, tc.class, tc.reason)
			}
		})
	}
}

func clsMembership(expires *time.Time) func(*testing.T) sharing.State {
	return func(*testing.T) sharing.State {
		return sharing.State{Now: clsNow, Membership: &sharing.MembershipState{Current: clsMember("aud_x", "prn_p", expires)}}
	}
}

func clsCeiling(classes ...string) func(*testing.T) sharing.State {
	return func(*testing.T) sharing.State {
		return sharing.State{Now: clsNow, Ceiling: &sharing.CeilingState{PrincipalID: "prn_p", DenyClasses: classes}}
	}
}

// clsUnknown is a change the classifier has no rule for.
type clsUnknown struct{}

func (clsUnknown) Action() string { return "share.unknown" }

// TestClassify_UnclassifiableIsWidening: whatever the classifier cannot
// show to be neutral or narrowing is widening (ADR-083 §10 B).
func TestClassify_UnclassifiableIsWidening(t *testing.T) {
	badSpec := &storage.SharePolicy{ID: "pol_a", Name: "base", Version: 2, Spec: json.RawMessage(`{"data":"mention==x"}`)}
	unknownField := &storage.SharePolicy{ID: "pol_a", Name: "base", Version: 2, Spec: json.RawMessage(`{"data":"type==note","grants":true}`)}
	wider := clsWith(func(p *sharing.Policy) { p.Capabilities.Export = true })
	cases := []struct {
		name   string
		change sharing.Change
		state  func(t *testing.T) sharing.State
		reason string
	}{
		{"nil change", nil, nil, sharing.ReasonUnclassifiable},
		{"unknown change", clsUnknown{}, nil, sharing.ReasonUnclassifiable},
		{"member put, state not read", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p"}, nil, sharing.ReasonStateMissing},
		{"member put, membership of another principal", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_q"},
			clsMembership(clsAt(time.Hour)), sharing.ReasonStateMismatch},
		{"policy update, state not read", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: clsBase()}, nil, sharing.ReasonStateMissing},
		{"policy update, row not read", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: clsBase()},
			func(*testing.T) sharing.State { return sharing.State{Now: clsNow, Policy: &sharing.PolicyState{}} }, sharing.ReasonStateMissing},
		{"policy update, another policy's row", sharing.PolicyUpdate{PolicyID: "pol_b", Policy: clsBase()},
			func(t *testing.T) sharing.State { return clsPolicyState(t, clsGrant("grt_1", "aud_x", "pol_a")) }, sharing.ReasonStateMismatch},
		{"policy update, no clock", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: clsBase()},
			func(t *testing.T) sharing.State {
				s := clsPolicyState(t, clsGrant("grt_1", "aud_x", "pol_a"))
				s.Now = time.Time{}
				return s
			}, sharing.ReasonStateMissing},
		{"policy update, stored spec unparseable", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: clsBase()},
			func(*testing.T) sharing.State {
				return sharing.State{Now: clsNow, Policy: &sharing.PolicyState{Row: badSpec, Grants: []*storage.ShareGrant{clsGrant("grt_1", "aud_x", "pol_a")}}}
			}, sharing.ReasonSpecUnparseable},
		{"policy update, stored spec with an unknown key", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: clsBase()},
			func(*testing.T) sharing.State {
				return sharing.State{Now: clsNow, Policy: &sharing.PolicyState{Row: unknownField, Grants: []*storage.ShareGrant{clsGrant("grt_1", "aud_x", "pol_a")}}}
			}, sharing.ReasonSpecUnparseable},
		{"policy update, new spec invalid", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: clsWith(func(p *sharing.Policy) { p.Data = "" })},
			func(t *testing.T) sharing.State { return clsPolicyState(t, clsGrant("grt_1", "aud_x", "pol_a")) }, sharing.ReasonSpecWider},
		{"grant create, audience not read", sharing.GrantCreate{AudienceID: "aud_x", PolicyID: "pol_a"}, nil, sharing.ReasonStateMissing},
		{"ceiling, state not read", sharing.CeilingSet{PrincipalID: "prn_p", DenyClasses: []string{"personal"}}, nil, sharing.ReasonStateMissing},
		{"ceiling, another principal's", sharing.CeilingSet{PrincipalID: "prn_q", DenyClasses: []string{"personal"}},
			clsCeiling(), sharing.ReasonStateMismatch},
		{"key policy, state not read", sharing.KeyPolicyBind{IdentityID: "idn_k", PolicyID: "pol_n"}, nil, sharing.ReasonStateMissing},
		{"key policy, another identity", sharing.KeyPolicyBind{IdentityID: "idn_other", PolicyID: "pol_n"},
			func(t *testing.T) sharing.State { return clsKeyState(t, nil, clsRow(t, "pol_n", 1, wider)) }, sharing.ReasonStateMismatch},
		{"key policy, target row not read", sharing.KeyPolicyBind{IdentityID: "idn_k", PolicyID: "pol_n"},
			func(t *testing.T) sharing.State { return clsKeyState(t, nil, nil) }, sharing.ReasonStateMissing},
		{"key policy, target row of another policy", sharing.KeyPolicyBind{IdentityID: "idn_k", PolicyID: "pol_n"},
			func(t *testing.T) sharing.State { return clsKeyState(t, nil, clsRow(t, "pol_z", 1, wider)) }, sharing.ReasonStateMismatch},
		{"key policy, current row not read", sharing.KeyPolicyBind{IdentityID: "idn_k", PolicyID: "pol_n"},
			func(t *testing.T) sharing.State {
				s := clsKeyState(t, nil, clsRow(t, "pol_n", 1, wider))
				s.KeyBinding.CurrentPolicyID = "pol_a"
				return s
			}, sharing.ReasonStateMissing},
		{"key policy, current spec unparseable", sharing.KeyPolicyBind{IdentityID: "idn_k", PolicyID: "pol_n"},
			func(t *testing.T) sharing.State { return clsKeyState(t, badSpec, clsRow(t, "pol_n", 1, clsBase())) }, sharing.ReasonSpecUnparseable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s sharing.State
			if tc.state != nil {
				s = tc.state(t)
			}
			got := sharing.Classify(tc.change, s)
			if got.Class != sharing.Widening || got.Reason != tc.reason {
				t.Errorf("got %s/%s (%s), want widening/%s", got.Class, got.Reason, got.Detail, tc.reason)
			}
		})
	}
}

// TestClassify_Beneficiaries names who a change may benefit, per the
// ADR-083 §10 B table; they may not approve it.
func TestClassify_Beneficiaries(t *testing.T) {
	wider := clsWith(func(p *sharing.Policy) { p.Capabilities.Export = true })
	cases := []struct {
		name   string
		change sharing.Change
		state  sharing.State
		want   []string
	}{
		{"member put", sharing.MemberPut{AudienceID: "aud_x", PrincipalID: "prn_p"},
			sharing.State{Now: clsNow, Membership: &sharing.MembershipState{}}, []string{"prn_p"}},
		{"policy update: members of audiences with a grant in force, expired members kept", sharing.PolicyUpdate{PolicyID: "pol_a", Policy: wider},
			clsPolicyState(t, clsGrant("grt_1", "aud_x", "pol_a"), func() *storage.ShareGrant {
				g := clsGrant("grt_2", "aud_y", "pol_a")
				g.RevokedAt = clsAt(-time.Minute)
				return g
			}()), []string{"prn_m1", "prn_m2"}},
		{"grant create: audience members", sharing.GrantCreate{AudienceID: "aud_x", PolicyID: "pol_a"},
			sharing.State{Now: clsNow, Audience: &sharing.AudienceState{ID: "aud_x", Members: []*storage.AudienceMember{
				clsMember("aud_x", "prn_b", nil), clsMember("aud_x", "prn_a", nil), clsMember("aud_x", "prn_b", nil),
			}}}, []string{"prn_a", "prn_b"}},
		{"ceiling", sharing.CeilingSet{PrincipalID: "prn_p"}, clsCeiling("personal")(t), []string{"prn_p"}},
		{"key policy", sharing.KeyPolicyBind{IdentityID: "idn_k", PrincipalID: "prn_p"},
			clsKeyState(t, clsRow(t, "pol_a", 1, clsBase()), nil), []string{"prn_p"}},
		{"grant revoke: none", sharing.GrantRevoke{GrantID: "grt_1"}, sharing.State{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sharing.Classify(tc.change, tc.state).Beneficiaries
			if !slices.Equal(got, tc.want) {
				t.Errorf("beneficiaries %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChangeActions(t *testing.T) {
	cases := []struct {
		change sharing.Change
		action string
	}{
		{sharing.AudienceCreate{}, "share.audience.create"},
		{sharing.MemberPut{}, "share.member.put"},
		{sharing.MemberRemove{}, "share.member.remove"},
		{sharing.PolicyCreate{}, "share.policy.create"},
		{sharing.TemplateInstall{}, "share.template.install"},
		{sharing.PolicyUpdate{}, "share.policy.update"},
		{sharing.GrantCreate{}, "share.grant.create"},
		{sharing.GrantRevoke{}, "share.grant.revoke"},
		{sharing.CeilingSet{}, "share.ceiling.set"},
		{sharing.KeyPolicyBind{}, "share.key_policy.bind"},
	}
	for _, tc := range cases {
		if got := tc.change.Action(); got != tc.action {
			t.Errorf("%T: action %q, want %q", tc.change, got, tc.action)
		}
	}
}

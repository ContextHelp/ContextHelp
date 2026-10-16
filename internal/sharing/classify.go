package sharing

import (
	"slices"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Share change classification (ADR-083 §10 B). A change to the sharing
// registry by a non-owner admin that widens anyone's access waits for
// approval; one that narrows or changes no view applies at once. This
// file decides which, structurally, from the change and the registry
// rows it touches (State), never by evaluating data. The class is
//
//   - Neutral when no viewer's compiled view differs, because no grant
//     in force reaches what the change touches;
//   - Narrowing when every row, field and capability visible after the
//     change was visible before it;
//   - Widening otherwise, and whenever the classifier cannot show one
//     of the first two: an unknown change, state that was not read or
//     belongs to other rows, a stored spec that does not parse.
//
// Classify is pure. ReadState reads the State a change needs and
// Fingerprint hashes it for the approval's staleness check
// (classify_state.go).

// Class is a share change's effect on what viewers may see.
type Class string

// Classes.
const (
	Neutral   Class = "neutral"
	Narrowing Class = "narrowing"
	Widening  Class = "widening"
)

// Reason codes: why a change got its class. They are stable,
// machine-readable values for the approval payload and the audit log.
const (
	// ReasonUnclassifiable: a change the classifier has no rule for.
	ReasonUnclassifiable = "unclassifiable"
	// ReasonStateMissing: the registry state the rule needs was not
	// read (or the clock was not set).
	ReasonStateMissing = "state_missing"
	// ReasonStateMismatch: the state supplied is of other rows than
	// the change names.
	ReasonStateMismatch = "state_mismatch"
	// ReasonStateUnreadable: reading the state failed (ClassifyRead).
	ReasonStateUnreadable = "state_unreadable"

	ReasonEmptyAudience     = "empty_audience"
	ReasonMembershipNew     = "membership_new"
	ReasonExpiryExtended    = "expiry_extended"
	ReasonExpiryRemoved     = "expiry_removed"
	ReasonExpiryShortened   = "expiry_shortened"
	ReasonExpiryUnchanged   = "expiry_unchanged"
	ReasonMembershipRemoved = "membership_removed"
	ReasonPolicyNotGranted  = "policy_not_granted"
	ReasonNoGrantInForce    = "no_grant_in_force"
	// ReasonSpecEqual, ReasonSpecNarrower and ReasonSpecWider report
	// CompareSpecs for a policy update with a grant in force and for a
	// key rebound to another policy; Detail names the component that
	// widened.
	ReasonSpecEqual       = "spec_equal"
	ReasonSpecNarrower    = "spec_narrower"
	ReasonSpecWider       = "spec_wider"
	ReasonSpecUnparseable = "spec_unparseable"
	ReasonGrantCreated    = "grant_created"
	ReasonGrantRevoked    = "grant_revoked"
	// ReasonCeilingRaised: the new deny list strictly includes the old.
	ReasonCeilingRaised    = "ceiling_raised"
	ReasonCeilingUnchanged = "ceiling_unchanged"
	// ReasonCeilingLowered: the new deny list misses a class the old
	// one had.
	ReasonCeilingLowered      = "ceiling_lowered"
	ReasonKeyBound            = "key_bound"
	ReasonKeyUnbound          = "key_unbound"
	ReasonKeyBindingUnchanged = "key_binding_unchanged"
)

// Classification is a share change's class, why, and the principals it
// may benefit (ADR-083 §10 B, "Beneficiaries"), who may not approve it.
type Classification struct {
	Class  Class  `json:"class"`
	Reason string `json:"reason"`
	// Detail adds to Reason: the widening spec component, a parse
	// error. Free text, not for matching.
	Detail string `json:"detail,omitempty"`
	// Beneficiaries are sorted principal IDs. The approval mechanism
	// recomputes them at decision time; these are as of State.
	Beneficiaries []string `json:"beneficiaries,omitempty"`
}

// Change actions, as the approval request's action names them
// (ADR-083 §9: share.grant.create, ...). One per mutating sharing
// route of ADR-083 §10 B; POST /share/invites is a credential.issue
// request (§10 A) and has none.
const (
	ActionAudienceCreate  = "share.audience.create"
	ActionMemberPut       = "share.member.put"
	ActionMemberRemove    = "share.member.remove"
	ActionPolicyCreate    = "share.policy.create"
	ActionTemplateInstall = "share.template.install"
	ActionPolicyUpdate    = "share.policy.update"
	ActionGrantCreate     = "share.grant.create"
	ActionGrantRevoke     = "share.grant.revoke"
	ActionCeilingSet      = "share.ceiling.set"
	ActionKeyPolicyBind   = "share.key_policy.bind"
)

// Change is a proposed sharing registry change, resolved to ids: the
// route's request body plus its target ids, as the share.change
// request's payload carries it.
type Change interface {
	Action() string
}

// AudienceCreate is POST /share/audiences.
type AudienceCreate struct {
	Name string `json:"name"`
}

// MemberPut is PUT /share/audiences/{a}/members/{p}: a new membership
// or a replaced expiry. A nil ExpiresAt is no expiry.
type MemberPut struct {
	AudienceID  string     `json:"audience_id"`
	PrincipalID string     `json:"principal_id"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// MemberRemove is DELETE /share/audiences/{a}/members/{p}.
type MemberRemove struct {
	AudienceID  string `json:"audience_id"`
	PrincipalID string `json:"principal_id"`
}

// PolicyCreate is POST /share/policies.
type PolicyCreate struct {
	Policy Policy `json:"policy"`
}

// TemplateInstall is POST /share/templates/{name}.
type TemplateInstall struct {
	Name string `json:"name"`
}

// PolicyUpdate is PUT /share/policies/{p}: Policy replaces the spec of
// PolicyID.
type PolicyUpdate struct {
	PolicyID string `json:"policy_id"`
	Policy   Policy `json:"policy"`
}

// GrantCreate is POST /share/grants. A nil ExpiresAt is no expiry.
type GrantCreate struct {
	AudienceID string     `json:"audience_id"`
	PolicyID   string     `json:"policy_id"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// GrantRevoke is DELETE /share/grants/{g}.
type GrantRevoke struct {
	GrantID string `json:"grant_id"`
}

// CeilingSet is PUT /principals/{id}/ceiling: DenyClasses replaces the
// principal's ceiling.
type CeilingSet struct {
	PrincipalID string   `json:"principal_id"`
	DenyClasses []string `json:"deny_classes"`
}

// KeyPolicyBind is PUT /share/keys/{key}/policy: the key's identity is
// bound to PolicyID, or unbound when it is "". PrincipalID is the
// key's principal. PUT /self/keys/{key}/policy is the same change but
// not an admin change (ADR-083 §10 B): it applies at once whatever its
// class.
type KeyPolicyBind struct {
	KeyID       string `json:"key_id"`
	IdentityID  string `json:"identity_id"`
	PrincipalID string `json:"principal_id"`
	PolicyID    string `json:"policy_id,omitempty"`
}

// Action implementations.
func (AudienceCreate) Action() string  { return ActionAudienceCreate }
func (MemberPut) Action() string       { return ActionMemberPut }
func (MemberRemove) Action() string    { return ActionMemberRemove }
func (PolicyCreate) Action() string    { return ActionPolicyCreate }
func (TemplateInstall) Action() string { return ActionTemplateInstall }
func (PolicyUpdate) Action() string    { return ActionPolicyUpdate }
func (GrantCreate) Action() string     { return ActionGrantCreate }
func (GrantRevoke) Action() string     { return ActionGrantRevoke }
func (CeilingSet) Action() string      { return ActionCeilingSet }
func (KeyPolicyBind) Action() string   { return ActionKeyPolicyBind }

// Classify returns the class of change c against registry state s, by
// the route table of ADR-083 §10 B:
//
//   - AudienceCreate: neutral, an empty audience has no grant.
//   - MemberPut with no current membership: widening. With one:
//     widening when the new expiry is later or removes it, narrowing
//     when earlier (an open membership gaining an expiry included),
//     neutral when equal. An expired membership is still a current
//     row: renewing it is a later expiry.
//   - MemberRemove: narrowing.
//   - PolicyCreate, TemplateInstall: neutral, a policy reaches no
//     viewer until granted.
//   - PolicyUpdate: neutral when no grant of the policy is in force at
//     s.Now (not revoked, not expired; audience membership is not
//     looked at). Else CompareSpecs of the stored spec against the new
//     one: narrowing when equal or narrower, widening otherwise.
//   - GrantCreate: widening, always.
//   - GrantRevoke: narrowing.
//   - CeilingSet: narrowing when the new deny list strictly includes
//     the current one, neutral when equal (after storage.NormalizeClasses),
//     widening otherwise.
//   - KeyPolicyBind: unbound to bound narrowing; bound to unbound
//     widening; bound to another policy by CompareSpecs, narrowing when
//     equal or narrower, widening otherwise. A binding that does not
//     change (the same policy, or unbound to unbound) is neutral.
//
// Anything else is widening: an unknown or nil change, a rule whose
// state section is nil or names other rows than c, a stored spec that
// does not decode.
func Classify(c Change, s State) Classification {
	switch c := c.(type) {
	case AudienceCreate:
		return Classification{Class: Neutral, Reason: ReasonEmptyAudience}
	case MemberPut:
		return classifyMemberPut(c, s)
	case MemberRemove:
		return Classification{Class: Narrowing, Reason: ReasonMembershipRemoved}
	case PolicyCreate, TemplateInstall:
		return Classification{Class: Neutral, Reason: ReasonPolicyNotGranted}
	case PolicyUpdate:
		return classifyPolicyUpdate(c, s)
	case GrantCreate:
		return classifyGrantCreate(c, s)
	case GrantRevoke:
		return Classification{Class: Narrowing, Reason: ReasonGrantRevoked}
	case CeilingSet:
		return classifyCeiling(c, s)
	case KeyPolicyBind:
		return classifyKeyPolicy(c, s)
	default:
		return widening(ReasonUnclassifiable, nil)
	}
}

func widening(reason string, beneficiaries []string) Classification {
	return Classification{Class: Widening, Reason: reason, Beneficiaries: beneficiaries}
}

func classifyMemberPut(c MemberPut, s State) Classification {
	who := []string{c.PrincipalID}
	if s.Membership == nil {
		return widening(ReasonStateMissing, who)
	}
	cur := s.Membership.Current
	if cur == nil {
		return widening(ReasonMembershipNew, who)
	}
	if cur.AudienceID != c.AudienceID || cur.PrincipalID != c.PrincipalID {
		return widening(ReasonStateMismatch, who)
	}
	out := Classification{Beneficiaries: who}
	switch {
	case cur.ExpiresAt == nil && c.ExpiresAt == nil:
		out.Class, out.Reason = Neutral, ReasonExpiryUnchanged
	case cur.ExpiresAt == nil:
		out.Class, out.Reason = Narrowing, ReasonExpiryShortened
	case c.ExpiresAt == nil:
		out.Class, out.Reason = Widening, ReasonExpiryRemoved
	case c.ExpiresAt.After(*cur.ExpiresAt):
		out.Class, out.Reason = Widening, ReasonExpiryExtended
	case c.ExpiresAt.Before(*cur.ExpiresAt):
		out.Class, out.Reason = Narrowing, ReasonExpiryShortened
	default:
		out.Class, out.Reason = Neutral, ReasonExpiryUnchanged
	}
	return out
}

func classifyPolicyUpdate(c PolicyUpdate, s State) Classification {
	ps := s.Policy
	if ps == nil || ps.Row == nil || s.Now.IsZero() {
		return widening(ReasonStateMissing, nil)
	}
	if ps.Row.ID != c.PolicyID {
		return widening(ReasonStateMismatch, nil)
	}
	inForce := ps.grantsInForce(s.Now)
	// A key bound to the policy sees its principal's view intersected
	// with the policy, so an edit changes what the key sees even when no
	// grant of the policy is in force.
	if len(inForce) == 0 && len(ps.Keys) == 0 {
		return Classification{Class: Neutral, Reason: ReasonNoGrantInForce}
	}
	var who []string
	for _, g := range inForce {
		who = append(who, memberIDs(ps.Members[g.AudienceID])...)
	}
	for _, k := range ps.Keys {
		who = append(who, k.PrincipalID)
	}
	old, err := PolicyFromSpec(ps.Row)
	if err != nil {
		out := widening(ReasonSpecUnparseable, sortedIDs(who))
		out.Detail = err.Error()
		return out
	}
	return bySpec(CompareSpecs(old, c.Policy), sortedIDs(who))
}

// bySpec maps a spec comparison to a class: equal or narrower is
// narrowing (ADR-083 §10 B).
func bySpec(cmp SpecComparison, who []string) Classification {
	out := Classification{Class: Narrowing, Beneficiaries: who}
	switch cmp.Order {
	case SpecEqual:
		out.Reason = ReasonSpecEqual
	case SpecNarrower:
		out.Reason = ReasonSpecNarrower
	default:
		out.Class, out.Reason, out.Detail = Widening, ReasonSpecWider, cmp.Component
	}
	return out
}

func classifyGrantCreate(c GrantCreate, s State) Classification {
	if s.Audience == nil {
		return widening(ReasonStateMissing, nil)
	}
	who := sortedIDs(memberIDs(s.Audience.Members))
	if s.Audience.ID != c.AudienceID {
		return widening(ReasonStateMismatch, who)
	}
	return widening(ReasonGrantCreated, who)
}

func classifyCeiling(c CeilingSet, s State) Classification {
	who := []string{c.PrincipalID}
	if s.Ceiling == nil {
		return widening(ReasonStateMissing, who)
	}
	if s.Ceiling.PrincipalID != c.PrincipalID {
		return widening(ReasonStateMismatch, who)
	}
	cur, next := storage.NormalizeClasses(s.Ceiling.DenyClasses), storage.NormalizeClasses(c.DenyClasses)
	switch {
	case !containsAll(next, cur):
		return widening(ReasonCeilingLowered, who)
	case len(next) == len(cur):
		return Classification{Class: Neutral, Reason: ReasonCeilingUnchanged, Beneficiaries: who}
	default:
		return Classification{Class: Narrowing, Reason: ReasonCeilingRaised, Beneficiaries: who}
	}
}

func classifyKeyPolicy(c KeyPolicyBind, s State) Classification {
	who := []string{c.PrincipalID}
	ks := s.KeyBinding
	if ks == nil {
		return widening(ReasonStateMissing, who)
	}
	if ks.IdentityID != c.IdentityID {
		return widening(ReasonStateMismatch, who)
	}
	switch {
	case ks.CurrentPolicyID == c.PolicyID:
		return Classification{Class: Neutral, Reason: ReasonKeyBindingUnchanged, Beneficiaries: who}
	case c.PolicyID == "":
		return widening(ReasonKeyUnbound, who)
	case ks.Target == nil:
		return widening(ReasonStateMissing, who)
	case ks.Target.ID != c.PolicyID:
		return widening(ReasonStateMismatch, who)
	case ks.CurrentPolicyID == "":
		return Classification{Class: Narrowing, Reason: ReasonKeyBound, Beneficiaries: who}
	case ks.Current == nil:
		return widening(ReasonStateMissing, who)
	case ks.Current.ID != ks.CurrentPolicyID:
		return widening(ReasonStateMismatch, who)
	}
	old, err := PolicyFromSpec(ks.Current)
	if err == nil {
		var next Policy
		if next, err = PolicyFromSpec(ks.Target); err == nil {
			return bySpec(CompareSpecs(old, next), who)
		}
	}
	out := widening(ReasonSpecUnparseable, who)
	out.Detail = err.Error()
	return out
}

func memberIDs(members []*storage.AudienceMember) []string {
	out := make([]string, 0, len(members))
	for _, m := range members {
		if m != nil {
			out = append(out, m.PrincipalID)
		}
	}
	return out
}

// sortedIDs sorts and dedupes ids; nil when empty.
func sortedIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// containsAll reports whether every entry of sub is in set.
func containsAll(set, sub []string) bool {
	for _, x := range sub {
		if !slices.Contains(set, x) {
			return false
		}
	}
	return true
}

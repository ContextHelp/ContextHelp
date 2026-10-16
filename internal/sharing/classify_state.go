package sharing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// State is the registry state a share change is classified against.
// Each change reads only its own section; a nil section is state not
// read, which Classify treats as widening.
type State struct {
	// Now is the clock grants in force are judged at (PolicyUpdate).
	Now time.Time
	// Membership is read for MemberPut.
	Membership *MembershipState
	// Policy is read for PolicyUpdate and GrantCreate.
	Policy *PolicyState
	// Audience is read for GrantCreate.
	Audience *AudienceState
	// Ceiling is read for CeilingSet.
	Ceiling *CeilingState
	// KeyBinding is read for KeyPolicyBind.
	KeyBinding *KeyBindingState
}

// MembershipState is the membership a MemberPut replaces.
type MembershipState struct {
	// Current is the membership of the principal in the audience,
	// expired or not; nil when there is none.
	Current *storage.AudienceMember
}

// PolicyState is a policy row and the grants of it.
type PolicyState struct {
	Row *storage.SharePolicy
	// Grants are the unrevoked grants of Row (PolicyUpdate only).
	// Classify keeps those of Row in force at State.Now.
	Grants []*storage.ShareGrant
	// Members are the memberships, expired ones included, of each
	// audience a grant in Grants binds, by audience ID: the
	// beneficiaries of a policy update.
	Members map[string][]*storage.AudienceMember
	// Keys are the live key identities bound to Row (PolicyUpdate
	// only): a bound key's view is its principal's intersected with the
	// policy, so each counts like a grant in force.
	Keys []storage.KeyBinding
}

// grantsInForce returns the grants of the row in force at now.
func (ps *PolicyState) grantsInForce(now time.Time) []*storage.ShareGrant {
	var out []*storage.ShareGrant
	for _, g := range ps.Grants {
		if g != nil && g.PolicyID == ps.Row.ID && g.ActiveAt(now) {
			out = append(out, g)
		}
	}
	return out
}

// AudienceState is the audience a grant binds and its memberships,
// expired ones included.
type AudienceState struct {
	ID      string
	Members []*storage.AudienceMember
}

// CeilingState is a principal's current ceiling.
type CeilingState struct {
	PrincipalID string
	DenyClasses []string
}

// KeyBindingState is a key identity's current policy binding and the
// policy a KeyPolicyBind binds it to.
type KeyBindingState struct {
	IdentityID string
	// CurrentPolicyID is the bound policy, "" when unbound; Current is
	// its row.
	CurrentPolicyID string
	Current         *storage.SharePolicy
	// Target is the row of the policy the change binds; nil for an
	// unbind.
	Target *storage.SharePolicy
}

// Registry is what ReadState reads: the sharing registry, as
// storage.SharingStore provides it. To classify in the same
// transaction as the write (ADR-083 §10 B), the caller passes a context
// that carries the transaction (SharingStore.InTx) to ReadState.
type Registry interface {
	Audiences() storage.AudienceStore
	Policies() storage.SharePolicyStore
	Grants() storage.ShareGrantStore
	DenyClasses(ctx context.Context, principalID string) ([]string, error)
	IdentityPolicy(ctx context.Context, identityID string) (string, error)
	PolicyKeyBindings(ctx context.Context, policyID string) ([]storage.KeyBinding, error)
}

// ReadState reads the registry state change c is classified against,
// at now. Changes that need no state (AudienceCreate, MemberRemove,
// PolicyCreate, TemplateInstall, GrantRevoke) read nothing. An unknown
// change, and any read error, is an error; Classify of a State that
// failed to read is widening (ClassifyRead does both).
func ReadState(ctx context.Context, r Registry, c Change, now time.Time) (State, error) {
	s := State{Now: now}
	if r == nil {
		return s, errors.New("share change: no registry")
	}
	var err error
	switch c := c.(type) {
	case AudienceCreate, MemberRemove, PolicyCreate, TemplateInstall, GrantRevoke:
	case MemberPut:
		s.Membership, err = readMembership(ctx, r, c.AudienceID, c.PrincipalID)
	case PolicyUpdate:
		s.Policy, err = readPolicyGrants(ctx, r, c.PolicyID)
	case GrantCreate:
		if s.Policy, err = readPolicy(ctx, r, c.PolicyID); err == nil {
			s.Audience, err = readAudience(ctx, r, c.AudienceID)
		}
	case CeilingSet:
		var classes []string
		if classes, err = r.DenyClasses(ctx, c.PrincipalID); err == nil {
			s.Ceiling = &CeilingState{PrincipalID: c.PrincipalID, DenyClasses: classes}
		}
	case KeyPolicyBind:
		s.KeyBinding, err = readKeyBinding(ctx, r, c)
	default:
		return s, fmt.Errorf("share change: no state for %T", c)
	}
	if err != nil {
		return State{Now: now}, fmt.Errorf("share change %s: read state: %w", c.Action(), err)
	}
	return s, nil
}

// ClassifyRead reads c's state and classifies c against it. A failed
// read is widening (ReasonStateUnreadable), with the State as far as
// it was read: nothing.
func ClassifyRead(ctx context.Context, r Registry, c Change, now time.Time) (Classification, State) {
	s, err := ReadState(ctx, r, c, now)
	if err != nil {
		out := widening(ReasonStateUnreadable, nil)
		out.Detail = err.Error()
		return out, s
	}
	return Classify(c, s), s
}

func readMembership(ctx context.Context, r Registry, audienceID, principalID string) (*MembershipState, error) {
	members, err := r.Audiences().ListMembers(ctx, audienceID)
	if err != nil {
		return nil, err
	}
	out := &MembershipState{}
	for _, m := range members {
		if m.PrincipalID == principalID {
			out.Current = m
		}
	}
	return out, nil
}

func readPolicy(ctx context.Context, r Registry, id string) (*PolicyState, error) {
	row, err := r.Policies().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &PolicyState{Row: row}, nil
}

func readPolicyGrants(ctx context.Context, r Registry, id string) (*PolicyState, error) {
	ps, err := readPolicy(ctx, r, id)
	if err != nil {
		return nil, err
	}
	if ps.Grants, err = r.Grants().List(ctx, storage.GrantFilter{PolicyID: id}); err != nil {
		return nil, err
	}
	if ps.Keys, err = r.PolicyKeyBindings(ctx, id); err != nil {
		return nil, err
	}
	ps.Members = map[string][]*storage.AudienceMember{}
	for _, g := range ps.Grants {
		if _, ok := ps.Members[g.AudienceID]; ok {
			continue
		}
		if ps.Members[g.AudienceID], err = r.Audiences().ListMembers(ctx, g.AudienceID); err != nil {
			return nil, err
		}
	}
	return ps, nil
}

func readAudience(ctx context.Context, r Registry, id string) (*AudienceState, error) {
	members, err := r.Audiences().ListMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	return &AudienceState{ID: id, Members: members}, nil
}

func readKeyBinding(ctx context.Context, r Registry, c KeyPolicyBind) (*KeyBindingState, error) {
	cur, err := r.IdentityPolicy(ctx, c.IdentityID)
	if err != nil {
		return nil, err
	}
	ks := &KeyBindingState{IdentityID: c.IdentityID, CurrentPolicyID: cur}
	if cur != "" {
		if ks.Current, err = r.Policies().Get(ctx, cur); err != nil {
			return nil, err
		}
	}
	if c.PolicyID != "" {
		if ks.Target, err = r.Policies().Get(ctx, c.PolicyID); err != nil {
			return nil, err
		}
	}
	return ks, nil
}

// fingerprintVersion versions the encoding Fingerprint hashes.
const fingerprintVersion = "share-change-fingerprint/1"

// Fingerprint hashes the registry rows change c's classification
// depends on, as "v1:sha256:<hex>" (storage.DigestPrefix). The approval
// mechanism records it when a request is filed and compares it when the
// last approval arrives: a different fingerprint means the
// preconditions moved and the request is stale (ADR-083 §9, §10 B).
//
// It is a pure function of the action, the target ids and these rows,
// in a fixed order, never of State.Now or of the order rows were read
// in:
//
//   - MemberPut: whether the membership exists and its expiry;
//   - PolicyUpdate: the policy id and version, and each grant of it
//     the state holds (id, audience, expiry, revoked), by id;
//   - GrantCreate: the policy id and version, and the audience id;
//   - CeilingSet: the principal and its normalized deny list;
//   - KeyPolicyBind: the identity, the bound policy id and version (or
//     unbound), and the target policy id and version (or unbind);
//   - the other changes depend on no row: their fingerprint covers the
//     change itself.
//
// Beneficiaries are left out: the mechanism recomputes them at decision
// time. A section c depends on that is nil is an error: a request
// must not freeze preconditions it never read.
func Fingerprint(c Change, s State) (string, error) {
	if c == nil {
		return "", errors.New("fingerprint: nil change")
	}
	rows, err := fingerprintRows(c, s)
	if err != nil {
		return "", fmt.Errorf("fingerprint %s: %w", c.Action(), err)
	}
	b, err := json.Marshal(struct {
		Version string `json:"v"`
		Action  string `json:"action"`
		Rows    any    `json:"rows"`
	}{fingerprintVersion, c.Action(), rows})
	if err != nil {
		return "", fmt.Errorf("fingerprint %s: %w", c.Action(), err)
	}
	sum := sha256.Sum256(b)
	return storage.DigestPrefix + hex.EncodeToString(sum[:]), nil
}

// policyRef is a policy row as a fingerprint names it.
type policyRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

func refOf(p *storage.SharePolicy) *policyRef {
	if p == nil {
		return nil
	}
	return &policyRef{ID: p.ID, Version: p.Version}
}

// utc normalizes an optional time, so equal instants hash equal.
func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

var errStateNotRead = errors.New("state not read")

func fingerprintRows(c Change, s State) (any, error) {
	switch c := c.(type) {
	case MemberPut:
		if s.Membership == nil {
			return nil, errStateNotRead
		}
		row := struct {
			Audience  string     `json:"audience"`
			Principal string     `json:"principal"`
			Exists    bool       `json:"exists"`
			ExpiresAt *time.Time `json:"expires_at"`
		}{Audience: c.AudienceID, Principal: c.PrincipalID}
		if m := s.Membership.Current; m != nil {
			row.Exists, row.ExpiresAt = true, utc(m.ExpiresAt)
		}
		return row, nil
	case PolicyUpdate:
		if s.Policy == nil || s.Policy.Row == nil {
			return nil, errStateNotRead
		}
		return struct {
			Policy *policyRef  `json:"policy"`
			Grants []grantRows `json:"grants"`
			Keys   []string    `json:"keys"`
		}{refOf(s.Policy.Row), grantFingerprint(s.Policy.Grants), keyFingerprint(s.Policy.Keys)}, nil
	case GrantCreate:
		if s.Policy == nil || s.Policy.Row == nil || s.Audience == nil {
			return nil, errStateNotRead
		}
		return struct {
			Policy   *policyRef `json:"policy"`
			Audience string     `json:"audience"`
		}{refOf(s.Policy.Row), s.Audience.ID}, nil
	case CeilingSet:
		if s.Ceiling == nil {
			return nil, errStateNotRead
		}
		return struct {
			Principal   string   `json:"principal"`
			DenyClasses []string `json:"deny_classes"`
		}{s.Ceiling.PrincipalID, storage.NormalizeClasses(s.Ceiling.DenyClasses)}, nil
	case KeyPolicyBind:
		if s.KeyBinding == nil {
			return nil, errStateNotRead
		}
		return struct {
			Identity string     `json:"identity"`
			Current  string     `json:"current"`
			Bound    *policyRef `json:"bound"`
			Target   *policyRef `json:"target"`
		}{s.KeyBinding.IdentityID, s.KeyBinding.CurrentPolicyID, refOf(s.KeyBinding.Current), refOf(s.KeyBinding.Target)}, nil
	case AudienceCreate, MemberRemove, PolicyCreate, TemplateInstall, GrantRevoke:
		return c, nil
	default:
		return nil, fmt.Errorf("unknown change %T", c)
	}
}

// keyFingerprint names the keys bound to a policy, sorted: a bind or
// unbind while a policy edit is pending changes it.
func keyFingerprint(keys []storage.KeyBinding) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.IdentityID+"@"+k.PrincipalID)
	}
	slices.Sort(out)
	return out
}

// grantRows is one grant as a fingerprint names it.
type grantRows struct {
	ID        string     `json:"id"`
	Audience  string     `json:"audience"`
	ExpiresAt *time.Time `json:"expires_at"`
	Revoked   bool       `json:"revoked"`
}

func grantFingerprint(grants []*storage.ShareGrant) []grantRows {
	out := make([]grantRows, 0, len(grants))
	for _, g := range grants {
		if g != nil {
			out = append(out, grantRows{ID: g.ID, Audience: g.AudienceID, ExpiresAt: utc(g.ExpiresAt), Revoked: g.RevokedAt != nil})
		}
	}
	slices.SortFunc(out, func(a, b grantRows) int { return strings.Compare(a.ID, b.ID) })
	return out
}

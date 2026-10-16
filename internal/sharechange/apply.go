package sharechange

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Events a sharing change appends to the audit log (ADR-080 §9): who
// changed what a viewer may see, in the order it happened. Each entry
// names the principal that asked for the change as its actor and
// carries the ids and names of what changed, never a secret. A change
// an approval applied also names the request and its approvers.
const (
	EventAudienceCreated = "share.audience.created"
	EventMemberAdded     = "share.member.added"
	EventMemberRemoved   = "share.member.removed"
	EventPolicyCreated   = "share.policy.created"
	EventPolicyUpdated   = "share.policy.updated"
	EventGrantCreated    = "share.grant.created"
	EventGrantRevoked    = "share.grant.revoked"
	EventCeilingSet      = "share.ceiling.set"
	EventKeyPolicyBound  = "share.key_policy.bound"
)

// Applied is what a change wrote: the audit event that records it, the
// rows it produced for the caller's answer, and the reference the
// approval request keeps as its result.
type Applied struct {
	// Event and Audit are the change's audit entry: its event type and
	// payload. The writer of the change appends it, as the actor it
	// names.
	Event string
	Audit map[string]any
	// Ref names the row written (audience, policy, grant id; the
	// membership as "<audience>/<principal>"; the principal whose
	// ceiling moved; the key identity rebound), "" when the change
	// wrote none.
	Ref string

	// Audience is the audience created, joined or granted.
	Audience *storage.Audience
	// Member is the membership written.
	Member *storage.AudienceMember
	// Policy is the policy created or updated, the one a grant binds,
	// or the one a key is bound to (nil for an unbind).
	Policy *storage.SharePolicy
	// Grant is the grant created.
	Grant *storage.ShareGrant
	// DenyClasses is the ceiling written, normalized, never nil after a
	// ceiling change.
	DenyClasses []string
	// KeyID and IdentityID name the key a binding changed.
	KeyID      string
	IdentityID string
}

// Error is a failed write. What names the thing the caller was
// changing, for its answer ("audience", "bind key policy").
type Error struct {
	What string
	Err  error
}

func (e *Error) Error() string { return e.What + ": " + e.Err.Error() }

func (e *Error) Unwrap() error { return e.Err }

func fail(what string, err error) *Error { return &Error{What: what, Err: err} }

// Apply writes change c to the registry st as actor at now, and returns
// what it wrote. It does not append the audit entry. A route applies a
// change at once through it (the owner's, and a narrowing or neutral
// one); the last approval of a share.change request applies the frozen
// payload through it, in the approval's transaction. Both pass the
// store of the transaction they run in.
func Apply(ctx context.Context, st storage.SharingStore, c sharing.Change, actor string, now time.Time) (*Applied, error) {
	switch c := c.(type) {
	case sharing.AudienceCreate:
		return applyAudienceCreate(ctx, st, c)
	case sharing.MemberPut:
		return applyMemberPut(ctx, st, c, actor, now)
	case sharing.MemberRemove:
		return applyMemberRemove(ctx, st, c)
	case sharing.PolicyCreate:
		return applyPolicyCreate(ctx, st, c.Policy)
	case sharing.TemplateInstall:
		p, ok := sharing.Template(c.Name)
		if !ok {
			return nil, fail("template", fmt.Errorf("no template %q: %w", c.Name, storage.ErrNotFound))
		}
		return applyPolicyCreate(ctx, st, p)
	case sharing.PolicyUpdate:
		return applyPolicyUpdate(ctx, st, c, now)
	case sharing.GrantCreate:
		return applyGrantCreate(ctx, st, c, actor, now)
	case sharing.GrantRevoke:
		return applyGrantRevoke(ctx, st, c, now)
	case sharing.CeilingSet:
		return applyCeilingSet(ctx, st, c)
	case sharing.KeyPolicyBind:
		return applyKeyPolicyBind(ctx, st, c)
	}
	return nil, fail("share change", fmt.Errorf("no write for %T", c))
}

func applyAudienceCreate(ctx context.Context, st storage.SharingStore, c sharing.AudienceCreate) (*Applied, error) {
	a := &storage.Audience{Name: c.Name}
	if err := st.Audiences().Create(ctx, a); err != nil {
		return nil, fail("audience", err)
	}
	return &Applied{
		Event: EventAudienceCreated, Audit: map[string]any{"audience_id": a.ID, "audience": a.Name},
		Ref: a.ID, Audience: a,
	}, nil
}

func applyMemberPut(ctx context.Context, st storage.SharingStore, c sharing.MemberPut, actor string, now time.Time) (*Applied, error) {
	a, err := st.Audiences().Get(ctx, c.AudienceID)
	if err != nil {
		return nil, fail("audience", err)
	}
	m := &storage.AudienceMember{AudienceID: a.ID, PrincipalID: c.PrincipalID, ExpiresAt: c.ExpiresAt, AddedBy: actor, AddedAt: now}
	if err := st.Audiences().AddMember(ctx, m); err != nil {
		return nil, fail("add audience member", err)
	}
	return &Applied{
		Event: EventMemberAdded,
		Audit: map[string]any{"audience_id": a.ID, "audience": a.Name, "principal_id": m.PrincipalID, "expires_at": m.ExpiresAt},
		Ref:   a.ID + "/" + m.PrincipalID, Audience: a, Member: m,
	}, nil
}

func applyMemberRemove(ctx context.Context, st storage.SharingStore, c sharing.MemberRemove) (*Applied, error) {
	a, err := st.Audiences().Get(ctx, c.AudienceID)
	if err != nil {
		return nil, fail("audience", err)
	}
	if err := st.Audiences().RemoveMember(ctx, a.ID, c.PrincipalID); err != nil {
		return nil, fail("audience member", err)
	}
	return &Applied{
		Event:    EventMemberRemoved,
		Audit:    map[string]any{"audience_id": a.ID, "audience": a.Name, "principal_id": c.PrincipalID},
		Audience: a,
	}, nil
}

func applyPolicyCreate(ctx context.Context, st storage.SharingStore, p sharing.Policy) (*Applied, error) {
	spec, err := p.Spec()
	if err != nil {
		return nil, fail("policy", err)
	}
	row := &storage.SharePolicy{Name: p.Name, Spec: spec}
	if err := st.Policies().Create(ctx, row); err != nil {
		return nil, fail("policy", err)
	}
	return &Applied{
		Event: EventPolicyCreated, Audit: map[string]any{"policy_id": row.ID, "policy": row.Name, "version": row.Version},
		Ref: row.ID, Policy: row,
	}, nil
}

func applyPolicyUpdate(ctx context.Context, st storage.SharingStore, c sharing.PolicyUpdate, now time.Time) (*Applied, error) {
	spec, err := c.Policy.Spec()
	if err != nil {
		return nil, fail("policy", err)
	}
	row, err := st.Policies().Update(ctx, c.PolicyID, spec, now)
	if err != nil {
		return nil, fail("policy", err)
	}
	return &Applied{
		Event: EventPolicyUpdated, Audit: map[string]any{"policy_id": row.ID, "policy": row.Name, "version": row.Version},
		Ref: row.ID, Policy: row,
	}, nil
}

func applyGrantCreate(ctx context.Context, st storage.SharingStore, c sharing.GrantCreate, actor string, now time.Time) (*Applied, error) {
	a, err := st.Audiences().Get(ctx, c.AudienceID)
	if err != nil {
		return nil, fail("audience", err)
	}
	p, err := st.Policies().Get(ctx, c.PolicyID)
	if err != nil {
		return nil, fail("policy", err)
	}
	g := &storage.ShareGrant{AudienceID: a.ID, PolicyID: p.ID, ExpiresAt: c.ExpiresAt, CreatedBy: actor, CreatedAt: now}
	if err := st.Grants().Create(ctx, g); err != nil {
		return nil, fail("grant", err)
	}
	return &Applied{
		Event: EventGrantCreated,
		Audit: map[string]any{
			"grant_id": g.ID, "audience_id": a.ID, "audience": a.Name,
			"policy_id": p.ID, "policy": p.Name, "policy_version": p.Version, "expires_at": g.ExpiresAt,
		},
		Ref: g.ID, Audience: a, Policy: p, Grant: g,
	}, nil
}

func applyGrantRevoke(ctx context.Context, st storage.SharingStore, c sharing.GrantRevoke, now time.Time) (*Applied, error) {
	if err := st.Grants().Revoke(ctx, c.GrantID, now); err != nil {
		return nil, fail("grant", err)
	}
	return &Applied{Event: EventGrantRevoked, Audit: map[string]any{"grant_id": c.GrantID}, Ref: c.GrantID}, nil
}

func applyCeilingSet(ctx context.Context, st storage.SharingStore, c sharing.CeilingSet) (*Applied, error) {
	for _, class := range c.DenyClasses {
		if err := storage.ValidateClassName(class); err != nil {
			return nil, fail("deny classes", err)
		}
	}
	classes := storage.NormalizeClasses(c.DenyClasses)
	if err := st.SetDenyClasses(ctx, c.PrincipalID, classes); err != nil {
		return nil, fail("principal", err)
	}
	if classes == nil {
		classes = []string{}
	}
	return &Applied{
		Event: EventCeilingSet, Audit: map[string]any{"principal_id": c.PrincipalID, "deny_classes": classes},
		Ref: c.PrincipalID, DenyClasses: classes,
	}, nil
}

func applyKeyPolicyBind(ctx context.Context, st storage.SharingStore, c sharing.KeyPolicyBind) (*Applied, error) {
	out := &Applied{Ref: c.IdentityID, KeyID: c.KeyID, IdentityID: c.IdentityID}
	audit := map[string]any{"key_id": c.KeyID, "identity_id": c.IdentityID, "policy_id": "", "policy": ""}
	if c.PolicyID != "" {
		p, err := st.Policies().Get(ctx, c.PolicyID)
		if err != nil {
			return nil, fail("policy", err)
		}
		out.Policy = p
		audit["policy_id"], audit["policy"] = p.ID, p.Name
	}
	if err := st.BindIdentityPolicy(ctx, c.IdentityID, c.PolicyID); err != nil {
		return nil, fail("bind key policy", err)
	}
	out.Event, out.Audit = EventKeyPolicyBound, audit
	return out, nil
}

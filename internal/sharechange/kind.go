package sharechange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// KindName is the discriminator of the share.change kind: a sharing
// registry change by a non-owner administrator that could widen
// someone's view (ADR-083 §10 B).
const KindName = "share.change"

// Deps are what the kind reads and writes. Sharing and Audit must be
// the stores of the driver that holds the approval store, so a request
// is decided, applied and recorded in one transaction.
type Deps struct {
	Sharing storage.SharingStore
	Auth    storage.AuthStore
	Audit   storage.AuditStore
	// Required is N for a new request: approvals.share.required. nil
	// means 1.
	Required func() int
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// kind is the share.change handler.
type kind struct{ Deps }

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}

// NewKind returns the share.change kind, to register on the approval
// mechanism.
func NewKind(d Deps) (approval.Kind, error) {
	if d.Sharing == nil || d.Auth == nil || d.Audit == nil {
		return approval.Kind{}, errors.New("share.change: sharing registry, principal registry and audit log are required")
	}
	k := kind{d}
	return approval.Kind{
		Name:          KindName,
		Actions:       Actions,
		Required:      d.Required,
		Validate:      k.validate,
		Fingerprint:   k.fingerprint,
		Eligible:      approval.ByScope(auth.ScopeApproveShares),
		Apply:         k.apply,
		Beneficiaries: k.beneficiaries,
	}, nil
}

// Register adds the share.change kind to reg.
func Register(reg *approval.Registry, d Deps) error {
	k, err := NewKind(d)
	if err != nil {
		return err
	}
	return reg.Register(k)
}

// Filing returns the filing of change c by requester: the change as its
// payload, the principal it is about as the subject. A reason is
// optional on this kind.
func Filing(requester *auth.Principal, c sharing.Change, reason string) (approval.Filing, error) {
	payload, err := Encode(c)
	if err != nil {
		return approval.Filing{}, err
	}
	return approval.Filing{
		Kind: KindName, Action: c.Action(), Requester: requester,
		SubjectID: SubjectOf(c), Reason: reason, Payload: payload,
	}, nil
}

// invalid is a refusal of the filing: 400 INVALID_REQUEST.
func invalid(format string, args ...any) error {
	return &approval.Error{Code: approval.CodeInvalid, Message: fmt.Sprintf(format, args...)}
}

// validate checks the filing and returns the payload to freeze: the
// change in its canonical encoding. The checks are those a route makes
// before it writes, so a request is never filed for a change the
// registry would refuse.
func (k kind) validate(ctx context.Context, f approval.Filing) (json.RawMessage, error) {
	c, err := Decode(f.Action, f.Payload)
	if err != nil {
		return nil, invalid("%s", err)
	}
	if want := SubjectOf(c); f.SubjectID != want {
		return nil, invalid("a %s request is about %q, not %q", f.Action, want, f.SubjectID)
	}
	if err := k.check(ctx, c); err != nil {
		return nil, err
	}
	return Encode(c)
}

// check refuses a change that is malformed, names a row that does not
// exist, or targets what no sharing change may.
func (k kind) check(ctx context.Context, c sharing.Change) error {
	switch c := c.(type) {
	case sharing.AudienceCreate:
		if err := storage.ValidateSharingName("audience", c.Name); err != nil {
			return invalid("name: %s", err)
		}
	case sharing.MemberPut:
		return k.checkMemberPut(ctx, c)
	case sharing.MemberRemove:
		if c.AudienceID == "" || c.PrincipalID == "" {
			return invalid("audience and principal are required")
		}
	case sharing.PolicyCreate:
		return c.Policy.Validate()
	case sharing.TemplateInstall:
		if _, ok := sharing.Template(c.Name); !ok {
			return invalid("no such template %q", c.Name)
		}
	case sharing.PolicyUpdate:
		if err := c.Policy.Validate(); err != nil {
			return invalid("%s", err)
		}
		_, err := k.Sharing.Policies().Get(ctx, c.PolicyID)
		return exists("policy", err)
	case sharing.GrantCreate:
		return k.checkGrantCreate(ctx, c)
	case sharing.GrantRevoke:
		_, err := k.Sharing.Grants().Get(ctx, c.GrantID)
		return exists("grant", err)
	case sharing.CeilingSet:
		return k.checkCeiling(ctx, c)
	case sharing.KeyPolicyBind:
		return k.checkKeyBind(ctx, c)
	default:
		return invalid("no share change %T", c)
	}
	return nil
}

// exists maps the error of a registry read to a refusal of the filing:
// a row that is not there is a bad request, anything else is a failure.
func exists(what string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrNotFound):
		return invalid("no such %s", what)
	}
	return fmt.Errorf("share.change: read %s: %w", what, err)
}

// future refuses an expiry that is already over.
func (k kind) future(at *time.Time) error {
	if at != nil && !at.After(k.now()) {
		return invalid("expires_at is not in the future")
	}
	return nil
}

func (k kind) checkMemberPut(ctx context.Context, c sharing.MemberPut) error {
	if c.PrincipalID == storage.OwnerPrincipal {
		return invalid("principal: the owner sees everything and joins no audience")
	}
	if _, err := k.Sharing.Audiences().Get(ctx, c.AudienceID); err != nil {
		return exists("audience", err)
	}
	p, err := k.Auth.Principals().Get(ctx, c.PrincipalID)
	if err != nil {
		return exists("principal", err)
	}
	if !p.Active() {
		return invalid("the principal is suspended or revoked")
	}
	return k.future(c.ExpiresAt)
}

func (k kind) checkGrantCreate(ctx context.Context, c sharing.GrantCreate) error {
	if _, err := k.Sharing.Audiences().Get(ctx, c.AudienceID); err != nil {
		return exists("audience", err)
	}
	if _, err := k.Sharing.Policies().Get(ctx, c.PolicyID); err != nil {
		return exists("policy", err)
	}
	return k.future(c.ExpiresAt)
}

func (k kind) checkCeiling(ctx context.Context, c sharing.CeilingSet) error {
	for _, class := range c.DenyClasses {
		if err := storage.ValidateClassName(class); err != nil {
			return invalid("deny_classes: %s", err)
		}
	}
	_, err := k.Sharing.DenyClasses(ctx, c.PrincipalID)
	return exists("principal", err)
}

// checkKeyBind refuses a key binding for the owner's key, a key that
// does not belong to the principal it names, or a policy that does not
// exist.
func (k kind) checkKeyBind(ctx context.Context, c sharing.KeyPolicyBind) error {
	if c.PrincipalID == storage.OwnerPrincipal {
		return invalid("only the owner binds the owner's keys")
	}
	idn, err := k.Auth.Identities().Get(ctx, c.IdentityID)
	if err != nil {
		return exists("key", err)
	}
	if idn.PrincipalID != c.PrincipalID {
		return invalid("key %s does not belong to principal %s", c.KeyID, c.PrincipalID)
	}
	if c.PolicyID != "" {
		_, err := k.Sharing.Policies().Get(ctx, c.PolicyID)
		return exists("policy", err)
	}
	return nil
}

// fingerprint is what a share.change request's preconditions hold: the
// classifier's digest of the rows the change's class depends on, and
// the facts the write depends on that the classifier does not read.
type fingerprint struct {
	// State is sharing.Fingerprint of the change against the rows it
	// reads, or "missing" when one of them is gone.
	State string `json:"state"`
	// Principal is the status of the member a membership change adds
	// ("gone" when it no longer exists): a principal suspended since
	// ends the request stale instead of failing its application.
	Principal string `json:"principal,omitempty"`
	// Lapsed is true once the expiry the change itself carries has
	// passed: applying it would write something already over.
	Lapsed bool `json:"lapsed,omitempty"`
}

func (k kind) fingerprint(ctx context.Context, r *storage.ApprovalRequest) (json.RawMessage, error) {
	c, err := Decode(r.Action, r.Payload)
	if err != nil {
		return nil, err
	}
	// The preconditions are read, and at the last approval compared and
	// applied, with no other change to the registry in between.
	if err := k.Sharing.LockRegistry(ctx); err != nil {
		return nil, err
	}
	now := k.now()
	fp := fingerprint{Lapsed: lapsed(c, now)}
	s, err := sharing.ReadState(ctx, k.Sharing, c, now)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		fp.State = "missing"
	case err != nil:
		return nil, err
	default:
		if fp.State, err = sharing.Fingerprint(c, s); err != nil {
			return nil, err
		}
	}
	if m, ok := c.(sharing.MemberPut); ok {
		switch p, err := k.Auth.Principals().Get(ctx, m.PrincipalID); {
		case errors.Is(err, storage.ErrNotFound):
			fp.Principal = "gone"
		case err != nil:
			return nil, err
		default:
			fp.Principal = p.Status
		}
	}
	return json.Marshal(fp)
}

// lapsed reports whether the expiry c carries is over at now.
func lapsed(c sharing.Change, now time.Time) bool {
	var at *time.Time
	switch c := c.(type) {
	case sharing.MemberPut:
		at = c.ExpiresAt
	case sharing.GrantCreate:
		at = c.ExpiresAt
	}
	return at != nil && !at.After(now)
}

// beneficiaries returns the principals the change would benefit, read
// now: they may not decide it. A state that cannot be read is an error,
// never an empty list: nobody is let through on a guess.
func (k kind) beneficiaries(ctx context.Context, r *storage.ApprovalRequest) ([]string, error) {
	c, err := Decode(r.Action, r.Payload)
	if err != nil {
		return nil, err
	}
	now := k.now()
	s, err := sharing.ReadState(ctx, k.Sharing, c, now)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	// A row that is gone benefits nobody through it; the change still
	// names the principal it is about.
	return sharing.Classify(c, s).Beneficiaries, nil
}

// apply writes the frozen change in the last approval's transaction as
// the request's requester, and appends the change's own audit entry
// naming the request and its approvers.
func (k kind) apply(ctx context.Context, r *storage.ApprovalRequest) (approval.Result, error) {
	c, err := Decode(r.Action, r.Payload)
	if err != nil {
		return approval.Result{}, err
	}
	now := k.now()
	applied, err := Apply(ctx, k.Sharing, c, r.RequestedBy, now)
	if err != nil {
		return approval.Result{}, err
	}
	approvers := []string{}
	for _, d := range r.Decisions {
		if d.Decision == storage.ApprovalVerdictApprove {
			approvers = append(approvers, d.ApproverID)
		}
	}
	payload := map[string]any{"request_id": r.ID, "approvers": approvers}
	for key, v := range applied.Audit {
		payload[key] = v
	}
	entry := &storage.AuditEntry{
		ID: uuid.New().String(), EventType: applied.Event, Actor: r.RequestedBy, Payload: payload, CreatedAt: now,
	}
	if err := k.Audit.Append(ctx, entry); err != nil {
		return approval.Result{}, fmt.Errorf("audit %s: %w", applied.Event, err)
	}
	return approval.Result{Ref: applied.Ref}, nil
}

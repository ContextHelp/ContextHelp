package steward

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Codes of a refused filing, beside the mechanism's own (approval
// Code*). Stable; the HTTP layer answers each with its status.
const (
	// CodeDisabled: steward.enabled is off (403).
	CodeDisabled = "STEWARD_REQUEST_DISABLED"
	// CodeRequesterKind: the requester is not a person (403).
	CodeRequesterKind = "STEWARD_REQUEST_REQUESTER_KIND"
	// CodeRequesterRole: the requester does not hold request:steward
	// (403).
	CodeRequesterRole = "STEWARD_REQUEST_REQUESTER_ROLE"
	// CodeStewardIneligible: the steward is not an active person
	// holding request:steward (400).
	CodeStewardIneligible = "STEWARD_REQUEST_STEWARD_INELIGIBLE"
	// CodeScopeInvalid: the scope (or audience) cannot be frozen into a
	// valid steward scope under the instance's settings (400).
	CodeScopeInvalid = "STEWARD_REQUEST_SCOPE_INVALID"
	// CodeWindowInvalid: the window is empty, inverted or already over
	// (400).
	CodeWindowInvalid = "STEWARD_REQUEST_WINDOW_INVALID"
	// CodeWindowTooLong: the window exceeds steward.max_duration (400).
	CodeWindowTooLong = "STEWARD_REQUEST_WINDOW_TOO_LONG"
	// CodeRenewalInvalid: the grant to renew does not exist or is
	// another steward's (400).
	CodeRenewalInvalid = "STEWARD_REQUEST_RENEWAL_INVALID"
)

// refuse returns a refusal with code.
func refuse(code, format string, args ...any) *approval.Error {
	return &approval.Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// KindOptions configures the steward.grant kind.
type KindOptions struct {
	// Settings returns the instance's steward settings; read on every
	// filing, approval and use. Required.
	Settings func() sharing.StewardSettings
	// Required returns N for a new request (approvals.steward.required),
	// frozen onto the request; nil means 1.
	Required func() int
	// MinReasonChars returns the shortest reason a request may give
	// (steward.min_reason_chars); read on every filing. nil, or a value
	// <= 0, means the default.
	MinReasonChars func() int
	// Sharing is the sharing registry: the audiences as_audience
	// compiles, and the grant store the final approval writes. Required.
	Sharing storage.SharingStore
	// Auth is the principal registry the steward is checked against.
	// Required.
	Auth storage.AuthStore
	// Audit is the audit log the grant's creation is recorded in; it
	// must be the driver's, so the entry joins the approval's
	// transaction. Required.
	Audit storage.AuditStore
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// kind implements the four functions of an approval kind over one set
// of options.
type kind struct{ o KindOptions }

// Kind returns the steward.grant kind to register on the approval
// mechanism (ADR-083 §2, §9):
//
//   - Validate checks the filing and freezes the operation: the steward,
//     the scope compiled (an explicit scope validated by the policy
//     parser, or as_audience compiled from the audience's policies now)
//     with its digest, and the window, normalised to the stores'
//     resolution;
//   - Fingerprint records what the payload depends on: the feature
//     switch, the steward still qualifying, the window within the cap and
//     still open, and, for as_audience, the audience's grants and policy
//     versions. A change before the last approval ends the request
//     stale;
//   - Eligible is a person (or the owner) holding approve:steward;
//   - Apply writes the grant row in the final approval's transaction.
//
// The steward is the request's subject, so it never approves its own
// grant; the mechanism keeps the requester out as well.
func Kind(o KindOptions) (approval.Kind, error) {
	switch {
	case o.Settings == nil:
		return approval.Kind{}, errors.New("steward kind: settings are required")
	case o.Sharing == nil || o.Auth == nil || o.Audit == nil:
		return approval.Kind{}, errors.New("steward kind: the sharing registry, the principal registry and the audit log are required")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	k := &kind{o: o}
	return approval.Kind{
		Name:        KindName,
		Actions:     []string{ActionRequest},
		Required:    o.Required,
		Validate:    k.validate,
		Fingerprint: k.fingerprint,
		Eligible:    eligibleApprover,
		Apply:       k.apply,
	}, nil
}

// eligibleApprover admits a person, or the owner, that holds
// approve:steward (ADR-083 §1): a device, a peer or an agent never
// approves, whatever scopes its credential carries.
func eligibleApprover(ctx context.Context, r *storage.ApprovalRequest, p *auth.Principal) (bool, error) {
	if p == nil || (p.Kind != storage.PrincipalKindPerson && p.Kind != storage.PrincipalKindOwner) {
		return false, nil
	}
	return approval.ByScope(auth.ScopeApproveSteward)(ctx, r, p)
}

func (k *kind) now() time.Time { return clockMS(k.o.Now()) }

// stewardEligible reports whether id is an active person that holds
// request:steward.
func (k *kind) stewardEligible(ctx context.Context, id string) (bool, error) {
	p, err := k.o.Auth.Principals().Get(ctx, id)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("steward %s: %w", id, err)
	}
	return p.Kind == storage.PrincipalKindPerson && p.Active() && HoldsRequestSteward(p.Roles), nil
}

// minReason is the shortest reason a filing may give now.
func (k *kind) minReason() int {
	if k.o.MinReasonChars != nil {
		if n := k.o.MinReasonChars(); n > 0 {
			return n
		}
	}
	return config.DefaultStewardMinReasonChars
}

func (k *kind) validate(ctx context.Context, f approval.Filing) (json.RawMessage, error) {
	if got, want := utf8.RuneCountInString(strings.TrimSpace(f.Reason)), k.minReason(); got < want {
		return nil, refuse(approval.CodeReasonTooShort, "a %s request needs a reason of at least %d characters, got %d", KindName, want, got)
	}
	set := k.o.Settings()
	if !set.Enabled {
		return nil, refuse(CodeDisabled, "steward access is not enabled on this instance (steward.enabled)")
	}
	if f.Requester.Kind != storage.PrincipalKindPerson {
		return nil, refuse(CodeRequesterKind, "only a person requests steward access, not a %s", f.Requester.Kind)
	}
	if !f.Requester.HasScope(auth.ScopeRequestSteward) {
		return nil, refuse(CodeRequesterRole, "requesting steward access needs the %s scope", auth.ScopeRequestSteward)
	}
	req, err := decodeRequest(f.Payload)
	if err != nil {
		return nil, err
	}
	if req.StewardID == "" || req.StewardID != f.SubjectID {
		return nil, refuse(approval.CodeInvalid, "the payload's steward_id must name the request's subject, got %q and %q", req.StewardID, f.SubjectID)
	}
	ok, err := k.stewardEligible(ctx, req.StewardID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse(CodeStewardIneligible, "%s is not an active person holding %s", req.StewardID, auth.ScopeRequestSteward)
	}
	now := k.now()
	frozen, dropped, err := k.freezeScope(ctx, req, set, now)
	if err != nil {
		return nil, err
	}
	scope, err := frozen.JSON()
	if err != nil {
		return nil, err
	}
	notBefore, expires, err := k.window(req, set, now)
	if err != nil {
		return nil, err
	}
	if err := k.checkRenewal(ctx, req); err != nil {
		return nil, err
	}
	digest, err := sharing.StewardScopeDigest(scope)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Payload{
		StewardID: req.StewardID, MatterRef: req.MatterRef, Scope: scope, ScopeDigest: digest, AsAudience: req.AsAudience,
		DroppedCapabilities: dropped, MaskingInert: frozen.Redacts(), NotBefore: notBefore, ExpiresAt: expires, RenewsID: req.RenewsID,
	})
}

// freezeScope returns the scope to freeze, and, for as_audience, the
// capabilities the audience's policies open that the freeze dropped. An
// explicit scope is a list of one spec, and one naming a capability a
// steward scope never opens, or a redact, is refused (Check,
// sharing.ParseStewardRequestScope); as_audience freezes each of the
// audience's policies as a spec of its own, its redact included.
func (k *kind) freezeScope(ctx context.Context, req Request, set sharing.StewardSettings, now time.Time) (sharing.StewardScope, []string, error) {
	var scope, none sharing.StewardScope
	var dropped []string
	switch {
	case len(req.Scope) > 0 && req.AsAudience != "":
		return none, nil, refuse(CodeScopeInvalid, "give a scope or an audience to compile (as_audience), not both")
	case len(req.Scope) == 0 && req.AsAudience == "":
		return none, nil, refuse(CodeScopeInvalid, "give a scope, or an audience to compile (as_audience)")
	case req.AsAudience != "":
		view, err := k.audience(ctx, req.AsAudience, now)
		if err != nil {
			return none, nil, err
		}
		if view.Missing {
			return none, nil, refuse(CodeScopeInvalid, "no audience named %q", req.AsAudience)
		}
		if scope, dropped, err = sharing.FreezeAudienceScope(view.policies, set); err != nil {
			return none, nil, refuse(CodeScopeInvalid, "audience %q: %v", req.AsAudience, err)
		}
	default:
		var err error
		if scope, err = sharing.ParseStewardRequestScope(req.Scope); err != nil {
			return none, nil, &approval.Error{Code: CodeScopeInvalid, Message: err.Error(), Err: err}
		}
	}
	if err := scope.Check(set); err != nil {
		return none, nil, &approval.Error{Code: CodeScopeInvalid, Message: err.Error(), Err: err}
	}
	return scope, dropped, nil
}

// window returns the validated window in the stores' resolution.
func (k *kind) window(req Request, set sharing.StewardSettings, now time.Time) (time.Time, time.Time, error) {
	notBefore := now
	if !req.NotBefore.IsZero() {
		notBefore = clockMS(req.NotBefore)
	}
	if req.ExpiresAt.IsZero() {
		return time.Time{}, time.Time{}, refuse(CodeWindowInvalid, "expires_at is required")
	}
	expires := clockMS(req.ExpiresAt)
	switch {
	case !expires.After(notBefore):
		return time.Time{}, time.Time{}, refuse(CodeWindowInvalid, "the window must end after it starts: not_before %s, expires_at %s", notBefore.Format(time.RFC3339), expires.Format(time.RFC3339))
	case !expires.After(now):
		return time.Time{}, time.Time{}, refuse(CodeWindowInvalid, "the window is already over: expires_at %s", expires.Format(time.RFC3339))
	case set.MaxDuration > 0 && expires.Sub(notBefore) > set.MaxDuration:
		return time.Time{}, time.Time{}, refuse(CodeWindowTooLong, "the window is %s; steward.max_duration is %s", expires.Sub(notBefore), set.MaxDuration)
	}
	return notBefore, expires, nil
}

// checkRenewal checks the grant a request renews is the same steward's.
func (k *kind) checkRenewal(ctx context.Context, req Request) error {
	if req.RenewsID == "" {
		return nil
	}
	prev, err := k.o.Sharing.StewardGrants().Get(ctx, req.RenewsID)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && prev.StewardID != req.StewardID) {
		return refuse(CodeRenewalInvalid, "no grant %q of steward %s to renew", req.RenewsID, req.StewardID)
	}
	return err
}

// fingerprintState is what the payload depends on (the preconditions).
type fingerprintState struct {
	Enabled         bool `json:"enabled"`
	StewardEligible bool `json:"steward_eligible"`
	WithinCap       bool `json:"within_cap"`
	WindowOpen      bool `json:"window_open"`
	// Audience is the state of the audience as_audience compiled, nil
	// for an explicit scope.
	Audience *audienceState `json:"audience,omitempty"`
}

func (k *kind) fingerprint(ctx context.Context, r *storage.ApprovalRequest) (json.RawMessage, error) {
	p, err := DecodePayload(r.Payload)
	if err != nil {
		return nil, err
	}
	set := k.o.Settings()
	now := k.now()
	eligible, err := k.stewardEligible(ctx, p.StewardID)
	if err != nil {
		return nil, err
	}
	fp := fingerprintState{
		Enabled: set.Enabled, StewardEligible: eligible,
		WithinCap:  set.MaxDuration <= 0 || p.ExpiresAt.Sub(p.NotBefore) <= set.MaxDuration,
		WindowOpen: now.Before(p.ExpiresAt),
	}
	if p.AsAudience != "" {
		view, err := k.audience(ctx, p.AsAudience, now)
		if err != nil {
			return nil, err
		}
		fp.Audience = &view.audienceState
	}
	return json.Marshal(fp)
}

func (k *kind) apply(ctx context.Context, r *storage.ApprovalRequest) (approval.Result, error) {
	canon, err := approval.Canonicalize(r.Payload)
	if err != nil {
		return approval.Result{}, fmt.Errorf("frozen payload: %w", err)
	}
	if approval.Digest(canon) != r.PayloadDigest {
		return approval.Result{}, fmt.Errorf("request %s: the payload no longer matches the digest the approvers bound to", r.ID)
	}
	p, err := DecodePayload(r.Payload)
	if err != nil {
		return approval.Result{}, err
	}
	digest, err := sharing.StewardScopeDigest(p.Scope)
	if err != nil {
		return approval.Result{}, err
	}
	if digest != p.ScopeDigest {
		return approval.Result{}, fmt.Errorf("request %s: the scope no longer matches its digest", r.ID)
	}
	if _, err := sharing.ParseStewardScope(p.Scope); err != nil {
		return approval.Result{}, err
	}
	now := k.now()
	g := &storage.StewardGrant{
		RequestID: r.ID, StewardID: p.StewardID, MatterRef: p.MatterRef, Scope: p.Scope, ScopeDigest: p.ScopeDigest,
		NotBefore: p.NotBefore, ExpiresAt: p.ExpiresAt, ApprovedAt: now, RenewsID: p.RenewsID,
	}
	if err := k.o.Sharing.StewardGrants().Create(ctx, g); err != nil {
		return approval.Result{}, err
	}
	approvers := []string{}
	for _, d := range r.Decisions {
		if d.Decision == storage.ApprovalVerdictApprove {
			approvers = append(approvers, d.ApproverID)
		}
	}
	payload := map[string]any{
		"grant_id": g.ID, "request_id": r.ID, "steward_id": g.StewardID, "approvers": approvers, "scope_digest": g.ScopeDigest,
		"not_before": g.NotBefore, "expires_at": g.ExpiresAt,
	}
	if p.AsAudience != "" {
		payload["as_audience"] = p.AsAudience
	}
	if err := k.o.Audit.Append(ctx, &storage.AuditEntry{
		ID: uuid.New().String(), EventType: EventGrantCreated, Actor: r.RequestedBy, Payload: payload, CreatedAt: now,
	}); err != nil {
		return approval.Result{}, fmt.Errorf("audit %s: %w", EventGrantCreated, err)
	}
	return approval.Result{Ref: g.ID}, nil
}

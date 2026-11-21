package approval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Options configures a Service.
type Options struct {
	// PendingTTL is how long a request may wait for its approvals
	// (approvals.pending_ttl); required.
	PendingTTL time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Service is the approval mechanism over one store.
type Service struct {
	store      storage.ApprovalStore
	audit      storage.AuditStore
	kinds      *Registry
	now        func() time.Time
	pendingTTL time.Duration
}

// NewService returns the mechanism over store, recording transitions
// in audit (the same driver's audit log, so entries join the store's
// transactions) for the kinds of reg.
func NewService(store storage.ApprovalStore, audit storage.AuditStore, reg *Registry, opts Options) (*Service, error) {
	if store == nil || audit == nil || reg == nil {
		return nil, errors.New("approval: store, audit log and registry are required")
	}
	if opts.PendingTTL <= 0 {
		return nil, fmt.Errorf("approval: pending TTL must be positive, got %s", opts.PendingTTL)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, audit: audit, kinds: reg, now: now, pendingTTL: opts.PendingTTL}, nil
}

// Kinds is the service's registry.
func (s *Service) Kinds() *Registry { return s.kinds }

// clock returns now in the store's resolution: UTC milliseconds.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

// File files f: the kind validates and freezes the payload, the
// preconditions are fingerprinted, N is frozen and the request is
// stored pending until now + approvals.pending_ttl, all in one
// transaction with its audit entry.
func (s *Service) File(ctx context.Context, f Filing) (*storage.ApprovalRequest, error) {
	k, err := s.checkFiling(f)
	if err != nil {
		return nil, err
	}
	n, err := k.required()
	if err != nil {
		return nil, err
	}
	var r *storage.ApprovalRequest
	err = s.store.InTx(ctx, func(ctx context.Context) error {
		raw, err := k.Validate(ctx, f)
		if err != nil {
			if CodeOf(err) != "" {
				return err
			}
			return &Error{Code: CodeInvalid, Message: err.Error(), Err: err}
		}
		payload, err := Canonicalize(raw)
		if err != nil {
			return &Error{Code: CodeInvalid, Message: "payload: " + err.Error(), Err: err}
		}
		now := s.clock()
		r = &storage.ApprovalRequest{
			Kind: k.Name, Action: f.Action, RequestedBy: f.Requester.ID, SubjectID: strings.TrimSpace(f.SubjectID),
			Reason: strings.TrimSpace(f.Reason), Payload: payload, PayloadDigest: Digest(payload),
			ApprovalsRequired: n, RequestedAt: now, ExpiresAt: now.Add(s.pendingTTL),
		}
		if r.Preconditions, err = s.fingerprint(ctx, k, r); err != nil {
			return err
		}
		if err := s.store.Create(ctx, r); err != nil {
			return err
		}
		return s.record(ctx, EventFiled, r.RequestedBy, r, now, map[string]any{"reason": r.Reason, "expires_at": r.ExpiresAt})
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// checkFiling refuses a filing the mechanism itself rejects, before
// the kind sees it.
func (s *Service) checkFiling(f Filing) (*Kind, error) {
	if f.Requester == nil || f.Requester.ID == "" {
		return nil, refuse(CodeInvalid, "a filing needs an authenticated requester")
	}
	if isOwner(f.Requester) {
		return nil, refuse(CodeOwnerCannotFile, "the owner's changes apply at once: the owner files no approval request")
	}
	k, ok := s.kinds.Lookup(f.Kind)
	if !ok {
		return nil, refuse(CodeKindUnknown, "no approval kind %q", f.Kind)
	}
	if !slices.Contains(k.Actions, f.Action) {
		return nil, refuse(CodeInvalid, "approval kind %s has no action %q (want one of %s)", k.Name, f.Action, strings.Join(k.Actions, ", "))
	}
	if got := utf8.RuneCountInString(strings.TrimSpace(f.Reason)); got < k.MinReason {
		return nil, refuse(CodeReasonTooShort, "a %s request needs a reason of at least %d characters, got %d", k.Name, k.MinReason, got)
	}
	return k, nil
}

// fingerprint returns the kind's canonical fingerprint of r.
func (s *Service) fingerprint(ctx context.Context, k *Kind, r *storage.ApprovalRequest) ([]byte, error) {
	raw, err := k.Fingerprint(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("approval kind %s: fingerprint: %w", k.Name, err)
	}
	return Canonicalize(raw)
}

// Decided is the outcome of a decision.
type Decided struct {
	// Request is the request after the decision, its decisions included.
	Request *storage.ApprovalRequest
	// State is its state after the decision.
	State State
	// Reveal is the kind's once-only result (Result.Reveal), set only in
	// the final approver's outcome.
	Reveal any
}

// Approve records approver's approval of request id, bound to digest.
// The N-th approval re-checks the preconditions: when they hold, the
// kind applies the payload and the request is approved, in the same
// transaction; when they moved, the request is stale, nothing is
// applied, and the error is CodeStale. A failed apply rolls back the
// decision with it.
func (s *Service) Approve(ctx context.Context, id string, approver *auth.Principal, digest, note string) (*Decided, error) {
	var out *Decided
	stale := false
	err := s.store.InTx(ctx, func(ctx context.Context) error {
		k, r, now, err := s.openDecision(ctx, id, approver, digest)
		if err != nil {
			return err
		}
		if err := s.decide(ctx, r, approver, storage.ApprovalVerdictApprove, note, now); err != nil {
			return err
		}
		approvals := len(approverIDs(r))
		if approvals < r.ApprovalsRequired {
			out = &Decided{Request: r, State: StateRequested}
			return nil
		}
		current, err := s.fingerprint(ctx, k, r)
		if err != nil {
			return err
		}
		if !preconditionsHold(r.Preconditions, current) {
			stale = true
			return s.close(ctx, r, StateStale, approver.ID, now, "", EventStale, nil)
		}
		res, err := k.Apply(ctx, r)
		if err != nil {
			return fmt.Errorf("approval kind %s: apply: %w", k.Name, err)
		}
		if err := s.close(ctx, r, StateApproved, approver.ID, now, res.Ref, EventApproved, map[string]any{"approvers": approverIDs(r)}); err != nil {
			return err
		}
		out = &Decided{Request: r, State: StateApproved, Reveal: res.Reveal}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if stale {
		return nil, refuse(CodeStale, "approval request %s is stale: what it was filed against changed, nothing was applied; file it again", id)
	}
	return out, nil
}

// Reject records approver's rejection of request id, bound to digest:
// one rejection by an eligible approver rejects the request.
func (s *Service) Reject(ctx context.Context, id string, approver *auth.Principal, digest, note string) (*Decided, error) {
	var out *Decided
	err := s.store.InTx(ctx, func(ctx context.Context) error {
		_, r, now, err := s.openDecision(ctx, id, approver, digest)
		if err != nil {
			return err
		}
		if err := s.decide(ctx, r, approver, storage.ApprovalVerdictReject, note, now); err != nil {
			return err
		}
		if err := s.close(ctx, r, StateRejected, approver.ID, now, "", EventRejected, map[string]any{"note": note}); err != nil {
			return err
		}
		out = &Decided{Request: r, State: StateRejected}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Withdraw withdraws request id as actor: its requester, or the owner.
func (s *Service) Withdraw(ctx context.Context, id string, actor *auth.Principal) (*storage.ApprovalRequest, error) {
	var out *storage.ApprovalRequest
	err := s.store.InTx(ctx, func(ctx context.Context) error {
		r, err := s.store.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}
		k, _ := s.kinds.Lookup(r.Kind)
		if ok, err := s.visible(ctx, k, r, actor); err != nil || !ok {
			return notVisible(id, err)
		}
		now := s.clock()
		if err := requirePending(r, now); err != nil {
			return err
		}
		if actor.ID != r.RequestedBy && !isOwner(actor) {
			return refuse(CodeNotRequester, "only the requester or the owner withdraws approval request %s", id)
		}
		if err := s.close(ctx, r, StateWithdrawn, actor.ID, now, "", EventWithdrawn, nil); err != nil {
			return err
		}
		out = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// openDecision locks request id and checks approver may decide it now,
// bound to digest: visible, pending, separated and eligible.
func (s *Service) openDecision(ctx context.Context, id string, approver *auth.Principal, digest string) (*Kind, *storage.ApprovalRequest, time.Time, error) {
	if approver == nil || approver.ID == "" {
		return nil, nil, time.Time{}, refuse(CodeNotEligible, "a decision needs an authenticated approver")
	}
	r, err := s.store.GetForUpdate(ctx, id)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	k, known := s.kinds.Lookup(r.Kind)
	if ok, err := s.visible(ctx, k, r, approver); err != nil || !ok {
		return nil, nil, time.Time{}, notVisible(id, err)
	}
	if !known {
		return nil, nil, time.Time{}, refuse(CodeKindUnknown, "approval request %s is of kind %q, which is not registered: it cannot be decided", id, r.Kind)
	}
	now := s.clock()
	if err := requirePending(r, now); err != nil {
		return nil, nil, time.Time{}, err
	}
	if err := s.separate(ctx, k, r, approver); err != nil {
		return nil, nil, time.Time{}, err
	}
	if digest != r.PayloadDigest {
		return nil, nil, time.Time{}, refuse(CodeDigestMismatch, "the decision names digest %q; approval request %s is frozen at %s", digest, id, r.PayloadDigest)
	}
	return k, r, now, nil
}

// separate enforces ADR-082 §9's separation for approver on r: never the
// requester, never the subject (unless the kind admits it) or another
// beneficiary, eligible by the kind's rule, one decision each.
func (s *Service) separate(ctx context.Context, k *Kind, r *storage.ApprovalRequest, approver *auth.Principal) error {
	if approver.ID == r.RequestedBy {
		return refuse(CodeRequesterCannotDecide, "the requester decides nothing on its own request")
	}
	if approver.ID == r.SubjectID && !k.SubjectMayApprove {
		return refuse(CodeBeneficiaryCannotDecide, "the subject of a %s request may not decide it", k.Name)
	}
	if k.Beneficiaries != nil {
		ids, err := k.Beneficiaries(ctx, r)
		if err != nil {
			return fmt.Errorf("approval kind %s: beneficiaries: %w", k.Name, err)
		}
		for _, id := range ids {
			if id == approver.ID && (id != r.SubjectID || !k.SubjectMayApprove) {
				return refuse(CodeBeneficiaryCannotDecide, "the request would benefit %s, who may not decide it", id)
			}
		}
	}
	ok, err := k.Eligible(ctx, r, approver)
	if err != nil {
		return fmt.Errorf("approval kind %s: eligibility: %w", k.Name, err)
	}
	if !ok {
		return refuse(CodeNotEligible, "%s is not an eligible approver of %s requests", approver.ID, k.Name)
	}
	for _, d := range r.Decisions {
		if d.ApproverID == approver.ID {
			return refuse(CodeAlreadyDecided, "%s already decided approval request %s", approver.ID, r.ID)
		}
	}
	return nil
}

// decide stores approver's verdict on r, appends it to r.Decisions and
// records it.
func (s *Service) decide(ctx context.Context, r *storage.ApprovalRequest, approver *auth.Principal, v storage.ApprovalVerdict, note string, now time.Time) error {
	d := storage.ApprovalDecision{RequestID: r.ID, ApproverID: approver.ID, Decision: v, PayloadDigest: r.PayloadDigest, Note: strings.TrimSpace(note), DecidedAt: now}
	if err := s.store.AddDecision(ctx, &d); err != nil {
		if errors.Is(err, storage.ErrAlreadyExists) {
			return refuse(CodeAlreadyDecided, "%s already decided approval request %s", approver.ID, r.ID)
		}
		return err
	}
	r.Decisions = append(r.Decisions, d)
	return s.record(ctx, EventDecided, approver.ID, r, now, map[string]any{
		"decision": string(v), "note": d.Note, "approvals": len(approverIDs(r)),
	})
}

// close moves r to the terminal state to and records event.
func (s *Service) close(ctx context.Context, r *storage.ApprovalRequest, to State, actor string, now time.Time, ref, event string, extra map[string]any) error {
	if _, ok := Transition(StateOf(r, now), to); !ok {
		return refuse(CodeClosed, "approval request %s cannot move from %s to %s", r.ID, StateOf(r, now), to)
	}
	if err := s.store.Transition(ctx, r.ID, storage.ApprovalStatus(to), now, ref); err != nil {
		if errors.Is(err, storage.ErrApprovalNotPending) {
			return refuse(CodeClosed, "approval request %s was closed by another decision", r.ID)
		}
		return err
	}
	r.Status, r.DecidedAt, r.ResultRef = storage.ApprovalStatus(to), &now, ref
	if extra == nil {
		extra = map[string]any{}
	}
	if ref != "" {
		extra["result_ref"] = ref
	}
	return s.record(ctx, event, actor, r, now, extra)
}

// preconditionsHold compares the canonical fingerprint frozen at filing
// with the current one.
func preconditionsHold(frozen, current []byte) bool {
	f, err := Canonicalize(frozen)
	return err == nil && bytes.Equal(f, current)
}

// notVisible is the refusal of a request the caller may not see: not
// found, so its existence is not confirmed. A cause other than
// invisibility (an eligibility read that failed) is returned as is.
func notVisible(id string, cause error) error {
	if cause != nil {
		return cause
	}
	return fmt.Errorf("approval request %q: %w", id, storage.ErrNotFound)
}

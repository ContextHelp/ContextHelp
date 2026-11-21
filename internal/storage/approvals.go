package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The approval mechanism's store (ADR-082 §9): one table of requests,
// discriminated by kind, each a frozen payload that someone other than
// its requester must accept, and one table of the decisions on them.
// What an approval produces lives in its consumer's own tables; the
// store knows no kind. The state machine is internal/approval's: the
// store keeps rows, refuses malformed ones and serialises transitions.

// ApprovalStatus is the stored state of a request. Expired is not one:
// it is computed from ExpiresAt and the clock (ApprovalRequest.ExpiredAt)
// and never written, so no late sweeper can keep a request alive.
type ApprovalStatus string

// The stored states.
const (
	// ApprovalRequested is pending: filed, not yet decided.
	ApprovalRequested ApprovalStatus = "requested"
	// ApprovalApproved: the last approval arrived and the kind applied
	// the payload in its transaction.
	ApprovalApproved ApprovalStatus = "approved"
	// ApprovalRejected: an eligible approver rejected it.
	ApprovalRejected ApprovalStatus = "rejected"
	// ApprovalWithdrawn: its requester or the owner withdrew it.
	ApprovalWithdrawn ApprovalStatus = "withdrawn"
	// ApprovalStale: the state its payload was frozen against had
	// changed when the last approval arrived; nothing was applied.
	ApprovalStale ApprovalStatus = "stale"
)

// ApprovalStatuses lists the stored states, pending first.
var ApprovalStatuses = []ApprovalStatus{ApprovalRequested, ApprovalApproved, ApprovalRejected, ApprovalWithdrawn, ApprovalStale}

// Terminal reports whether s is a final state: every stored state but
// requested.
func (s ApprovalStatus) Terminal() bool {
	return s != ApprovalRequested && slices.Contains(ApprovalStatuses, s)
}

// ApprovalVerdict is what one decision says.
type ApprovalVerdict string

// The verdicts.
const (
	ApprovalVerdictApprove ApprovalVerdict = "approve"
	ApprovalVerdictReject  ApprovalVerdict = "reject"
)

// ErrApprovalNotPending is a transition of a request that is no longer
// requested: another decision or a withdrawal closed it first.
var ErrApprovalNotPending = errors.New("approval request is not pending")

// ApprovalRequest is one request on the approval mechanism.
type ApprovalRequest struct {
	// ID is apr_<ULID>.
	ID string `json:"id"`
	// Kind names the consumer ("steward.grant"); Action the operation
	// inside it ("share.grant.create").
	Kind   string `json:"kind"`
	Action string `json:"action"`
	// RequestedBy is the principal that filed it; it never changes.
	RequestedBy string `json:"requested_by"`
	// SubjectID is the principal the request is about, when there is
	// one; "" otherwise.
	SubjectID string `json:"subject_id,omitempty"`
	Reason    string `json:"reason"`
	// Payload is the operation, frozen as canonical JSON when filed;
	// PayloadDigest is its digest, to which every decision binds.
	Payload       json.RawMessage `json:"payload"`
	PayloadDigest string          `json:"payload_digest"`
	// Preconditions is the state the payload was frozen against, as the
	// kind computed it (JSON).
	Preconditions json.RawMessage `json:"preconditions"`
	// ApprovalsRequired is N, frozen when filed.
	ApprovalsRequired int            `json:"approvals_required"`
	Status            ApprovalStatus `json:"status"`
	RequestedAt       time.Time      `json:"requested_at"`
	ExpiresAt         time.Time      `json:"expires_at"`
	// DecidedAt is when the request left requested.
	DecidedAt *time.Time `json:"decided_at,omitempty"`
	// ResultRef names what the approval produced (a grant, membership,
	// identity or key id); "" until approved.
	ResultRef string `json:"result_ref,omitempty"`
	// Decisions are the decisions recorded on it, oldest first. Get and
	// List fill them; Create ignores them.
	Decisions []ApprovalDecision `json:"decisions"`
}

// ExpiredAt reports whether the request is pending past its TTL at now:
// the computed expired state.
func (r *ApprovalRequest) ExpiredAt(now time.Time) bool {
	return r != nil && r.Status == ApprovalRequested && !now.Before(r.ExpiresAt)
}

// ApprovalDecision is one principal's decision on a request.
type ApprovalDecision struct {
	RequestID  string          `json:"request_id"`
	ApproverID string          `json:"approver_id"`
	Decision   ApprovalVerdict `json:"decision"`
	// PayloadDigest is the digest the decision binds to.
	PayloadDigest string    `json:"payload_digest"`
	Note          string    `json:"note"`
	DecidedAt     time.Time `json:"decided_at"`
}

// ApprovalFilter narrows ApprovalStore.List. Every set field must hold.
type ApprovalFilter struct {
	Kind        string
	Status      ApprovalStatus
	RequestedBy string
	SubjectID   string
	// Limit caps the result; 0 means no cap.
	Limit int
}

// ApprovalStore keeps approval requests and their decisions.
type ApprovalStore interface {
	// InTx runs fn in one database transaction and commits when fn
	// returns nil; an error or a panic rolls back every write fn made.
	// The context fn receives carries the transaction: this store and
	// the driver's AuditLog, called with it, write inside it, and an
	// InTx called with it joins it instead of opening another. A store
	// that ignores the context writes outside it (and on SQLite waits
	// for the transaction's write lock), so an approval kind applies its
	// payload only through stores that join.
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
	// Create stores r in state requested. An empty ID is assigned
	// (NewApprovalRequestID), a zero RequestedAt becomes now and empty
	// Preconditions become {}. An empty kind, action, requester or
	// digest, a payload or preconditions that is not JSON, N below 1,
	// an ExpiresAt not after RequestedAt, or a status other than
	// requested is an error; an unknown requester or subject is
	// ErrNotFound; a taken ID is ErrAlreadyExists.
	Create(ctx context.Context, r *ApprovalRequest) error
	// Get returns the request with id and its decisions, or ErrNotFound.
	Get(ctx context.Context, id string) (*ApprovalRequest, error)
	// GetForUpdate is Get holding the request's row until the
	// transaction of ctx ends, so concurrent deciders of one request
	// queue instead of racing. Outside InTx it is Get.
	GetForUpdate(ctx context.Context, id string) (*ApprovalRequest, error)
	// List returns the requests matching f, newest first, with their
	// decisions.
	List(ctx context.Context, f ApprovalFilter) ([]*ApprovalRequest, error)
	// AddDecision stores d. A zero DecidedAt becomes now. An empty
	// request, approver or digest, or a verdict other than approve or
	// reject, is an error; an unknown request or approver is
	// ErrNotFound; a second decision by one approver on one request is
	// ErrAlreadyExists.
	AddDecision(ctx context.Context, d *ApprovalDecision) error
	// Transition moves request id from requested to the terminal state
	// to at at, recording resultRef. A to that is not terminal is an
	// error; an unknown id is ErrNotFound; a request no longer
	// requested is ErrApprovalNotPending and is left unchanged.
	Transition(ctx context.Context, id string, to ApprovalStatus, at time.Time, resultRef string) error
}

// NewApprovalRequestID returns a fresh apr_<ULID>.
func NewApprovalRequestID() string { return "apr_" + NewULID() }

// PrepareNewApprovalRequest fills the defaults of a request about to be
// created and validates it, as ApprovalStore.Create documents.
func PrepareNewApprovalRequest(r *ApprovalRequest, now time.Time) error {
	if r == nil {
		return errors.New("approval request: nil")
	}
	r.Kind, r.Action, r.RequestedBy = strings.TrimSpace(r.Kind), strings.TrimSpace(r.Action), strings.TrimSpace(r.RequestedBy)
	r.SubjectID = strings.TrimSpace(r.SubjectID)
	switch {
	case r.Kind == "" || r.Action == "":
		return errors.New("approval request: kind and action are required")
	case r.RequestedBy == "":
		return errors.New("approval request: requested_by is required")
	case r.PayloadDigest == "":
		return errors.New("approval request: payload_digest is required")
	case !json.Valid(r.Payload):
		return errors.New("approval request: payload must be JSON")
	case r.ApprovalsRequired < 1:
		return fmt.Errorf("approval request: approvals_required must be at least 1, got %d", r.ApprovalsRequired)
	case r.Status != "" && r.Status != ApprovalRequested:
		return fmt.Errorf("approval request: a new request is %s, not %s", ApprovalRequested, r.Status)
	}
	if len(strings.TrimSpace(string(r.Preconditions))) == 0 {
		r.Preconditions = json.RawMessage(`{}`)
	}
	if !json.Valid(r.Preconditions) {
		return errors.New("approval request: preconditions must be JSON")
	}
	if r.ID == "" {
		r.ID = NewApprovalRequestID()
	}
	if r.RequestedAt.IsZero() {
		r.RequestedAt = now
	}
	r.RequestedAt = r.RequestedAt.UTC()
	r.ExpiresAt = r.ExpiresAt.UTC()
	if !r.ExpiresAt.After(r.RequestedAt) {
		return errors.New("approval request: expires_at must be after requested_at")
	}
	r.Status = ApprovalRequested
	r.DecidedAt = nil
	r.ResultRef = ""
	r.Decisions = []ApprovalDecision{}
	return nil
}

// PrepareApprovalDecision fills the defaults of a decision about to be
// stored and validates it, as ApprovalStore.AddDecision documents.
func PrepareApprovalDecision(d *ApprovalDecision, now time.Time) error {
	if d == nil {
		return errors.New("approval decision: nil")
	}
	switch {
	case d.RequestID == "" || d.ApproverID == "":
		return errors.New("approval decision: request id and approver id are required")
	case d.Decision != ApprovalVerdictApprove && d.Decision != ApprovalVerdictReject:
		return fmt.Errorf("approval decision: verdict %q: want %s or %s", d.Decision, ApprovalVerdictApprove, ApprovalVerdictReject)
	case d.PayloadDigest == "":
		return errors.New("approval decision: payload_digest is required")
	}
	if d.DecidedAt.IsZero() {
		d.DecidedAt = now
	}
	d.DecidedAt = d.DecidedAt.UTC()
	return nil
}

// ValidateApprovalTransition reports whether to is a state Transition
// may move a request to.
func ValidateApprovalTransition(to ApprovalStatus) error {
	if !to.Terminal() {
		return fmt.Errorf("approval transition: %q is not a terminal state", to)
	}
	return nil
}

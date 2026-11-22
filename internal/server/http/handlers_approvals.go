package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The approval queue over the API (ADR-083 §9): the requests the caller
// filed, is the subject of or may decide, and the decisions on them.
// Requests are filed by the routes of their kinds, never here. Every
// route is under write:self, which every role and every browser session
// holds: who may see or decide a request is the request's own rule
// (requester, subject, the kind's eligible approvers, the owner),
// checked by the mechanism in the decision's transaction. A request the
// caller may not see is 404, like one that does not exist.

// maxApprovalListLimit caps GET /approvals?limit=.
const maxApprovalListLimit = 500

// approvalRoutes serves the approval queue. svc is nil when the router
// is built without one (route enumeration).
type approvalRoutes struct{ svc *approval.Service }

func newApprovalRoutes(rc RouterConfig) *approvalRoutes { return &approvalRoutes{svc: rc.Approvals} }

// ready reports whether the mechanism is wired, writing the refusal when
// it is not.
func (a *approvalRoutes) ready(w http.ResponseWriter) bool {
	if a.svc == nil {
		WriteError(w, http.StatusNotFound, CodeNotFound, "this instance has no approval mechanism")
		return false
	}
	return true
}

// approvalRecord is a request as the caller sees it: the stored row,
// its state now (expired computed), how many approvals it holds and
// whether the caller may decide it.
type approvalRecord struct {
	ID                string                     `json:"id"`
	Kind              string                     `json:"kind"`
	Action            string                     `json:"action"`
	State             approval.State             `json:"state"`
	RequestedBy       string                     `json:"requested_by"`
	SubjectID         string                     `json:"subject_id,omitempty"`
	Reason            string                     `json:"reason"`
	Payload           json.RawMessage            `json:"payload"`
	PayloadDigest     string                     `json:"payload_digest"`
	Preconditions     json.RawMessage            `json:"preconditions"`
	ApprovalsRequired int                        `json:"approvals_required"`
	Approvals         int                        `json:"approvals"`
	RequestedAt       time.Time                  `json:"requested_at"`
	ExpiresAt         time.Time                  `json:"expires_at"`
	DecidedAt         *time.Time                 `json:"decided_at,omitempty"`
	ResultRef         string                     `json:"result_ref,omitempty"`
	Decisions         []storage.ApprovalDecision `json:"decisions"`
	CanDecide         bool                       `json:"can_decide"`
}

func recordOf(r *storage.ApprovalRequest, state approval.State, canDecide bool) approvalRecord {
	approvals := 0
	for _, d := range r.Decisions {
		if d.Decision == storage.ApprovalVerdictApprove {
			approvals++
		}
	}
	decisions := r.Decisions
	if decisions == nil {
		decisions = []storage.ApprovalDecision{}
	}
	return approvalRecord{
		ID: r.ID, Kind: r.Kind, Action: r.Action, State: state, RequestedBy: r.RequestedBy, SubjectID: r.SubjectID,
		Reason: r.Reason, Payload: r.Payload, PayloadDigest: r.PayloadDigest, Preconditions: r.Preconditions,
		ApprovalsRequired: r.ApprovalsRequired, Approvals: approvals, RequestedAt: r.RequestedAt, ExpiresAt: r.ExpiresAt,
		DecidedAt: r.DecidedAt, ResultRef: r.ResultRef, Decisions: decisions, CanDecide: canDecide,
	}
}

// decisionResponse answers a decision: the request after it and, for
// the approval that applied it, the kind's once-only result (a secret
// the response alone carries).
type decisionResponse struct {
	approvalRecord
	Result any `json:"result,omitempty"`
}

// writeApprovalError answers a refusal of the mechanism with its status
// and stable code, a request the caller may not see with 404, anything
// else with a 500 whose cause goes to the log, not the caller.
func writeApprovalError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *approval.Error
	switch {
	case errors.As(err, &ae):
		WriteError(w, approvalStatus(ae.Code), ae.Code, ae.Message)
	case errors.Is(err, storage.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such approval request")
	default:
		slog.ErrorContext(r.Context(), "approval request failed", "route", r.Method+" "+r.URL.Path, "err", err)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not complete the approval request")
	}
}

// writeDecisionError is writeApprovalError for a decision on a stored
// request: one of a kind this instance no longer registers is a
// conflict with the request's state (409), not a malformed call (400,
// which is what filing a kind that does not exist answers).
func writeDecisionError(w http.ResponseWriter, r *http.Request, err error) {
	if approval.CodeOf(err) == approval.CodeKindUnknown {
		WriteError(w, http.StatusConflict, approval.CodeKindUnknown, err.Error())
		return
	}
	writeApprovalError(w, r, err)
}

// approvalStatus maps a mechanism code, or a kind's filing code, to its
// HTTP status.
func approvalStatus(code string) int {
	switch code {
	case approval.CodeInvalid, approval.CodeKindUnknown, approval.CodeReasonTooShort,
		steward.CodeStewardIneligible, steward.CodeScopeInvalid, steward.CodeWindowInvalid,
		steward.CodeWindowTooLong, steward.CodeRenewalInvalid:
		return http.StatusBadRequest
	case approval.CodeOwnerCannotRequest, approval.CodeRequesterCannotDecide, approval.CodeBeneficiaryCannotDecide,
		approval.CodeNotEligible, approval.CodeNotRequester,
		steward.CodeDisabled, steward.CodeRequesterRole:
		return http.StatusForbidden
	case approval.CodeClosed, approval.CodeExpired, approval.CodeStale, approval.CodeAlreadyDecided, approval.CodeDigestMismatch:
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

// approvalListParams are the query parameters GET /approvals takes.
var approvalListParams = []string{"kind", "state", "limit"}

// parseApprovalFilter reads GET /approvals?kind=&state=&limit=, refusing
// an unknown or repeated parameter and a bad value.
func parseApprovalFilter(r *http.Request) (approval.ListFilter, string) {
	var f approval.ListFilter
	q := r.URL.Query()
	for name, vals := range q {
		known := false
		for _, p := range approvalListParams {
			known = known || p == name
		}
		if !known {
			return f, "unknown parameter " + strconv.Quote(name) + " (want kind, state, limit)"
		}
		if len(vals) != 1 || strings.TrimSpace(vals[0]) == "" {
			return f, "parameter " + strconv.Quote(name) + " takes exactly one non-empty value"
		}
	}
	f.Kind = strings.TrimSpace(q.Get("kind"))
	if s := strings.TrimSpace(q.Get("state")); s != "" {
		f.State = approval.State(s)
		valid := false
		for _, st := range approval.States {
			valid = valid || st == f.State
		}
		if !valid {
			return f, "state " + strconv.Quote(s) + ": want requested, approved, rejected, withdrawn, stale or expired"
		}
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > maxApprovalListLimit {
			return f, "limit " + strconv.Quote(l) + ": want 1 to " + strconv.Itoa(maxApprovalListLimit)
		}
		f.Limit = n
	}
	return f, ""
}

// List handles GET /api/v1/approvals?kind=&state=&limit=: the requests
// the caller filed, is the subject of or may decide (the owner: every
// one), newest first.
func (a *approvalRoutes) List(w http.ResponseWriter, r *http.Request) {
	if !a.ready(w) {
		return
	}
	f, bad := parseApprovalFilter(r)
	if bad != "" {
		WriteError(w, http.StatusBadRequest, CodeInvalidParam, bad)
		return
	}
	items, err := a.svc.List(r.Context(), caller(r), f)
	if err != nil {
		writeApprovalError(w, r, err)
		return
	}
	out := make([]approvalRecord, 0, len(items))
	for _, it := range items {
		out = append(out, recordOf(it.Request, it.State, it.CanDecide))
	}
	WriteJSON(w, http.StatusOK, listBody(out, len(out)))
}

// Get handles GET /api/v1/approvals/{id}.
func (a *approvalRoutes) Get(w http.ResponseWriter, r *http.Request) {
	if !a.ready(w) {
		return
	}
	it, err := a.svc.Get(r.Context(), chi.URLParam(r, "id"), caller(r))
	if err != nil {
		writeApprovalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, recordOf(it.Request, it.State, it.CanDecide))
}

// decisionRequest is the body of POST /api/v1/approvals/{id}/approve
// and /reject: the digest of the payload the caller reviewed, to which
// the decision binds, and an optional note.
type decisionRequest struct {
	PayloadDigest string `json:"payload_digest"`
	Note          string `json:"note"`
}

// decisionBody reads a decision body, writing the 400 when it is bad.
func decisionBody(w http.ResponseWriter, r *http.Request) (decisionRequest, bool) {
	var req decisionRequest
	if !decodeRegistryBody(w, r, &req) {
		return req, false
	}
	req.PayloadDigest = strings.TrimSpace(req.PayloadDigest)
	if req.PayloadDigest == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "payload_digest: the digest of the payload you reviewed is required")
		return req, false
	}
	return req, true
}

// Approve handles POST /api/v1/approvals/{id}/approve. The N-th
// approval applies the request; its answer alone carries the kind's
// once-only result.
func (a *approvalRoutes) Approve(w http.ResponseWriter, r *http.Request) {
	if !a.ready(w) {
		return
	}
	req, ok := decisionBody(w, r)
	if !ok {
		return
	}
	d, err := a.svc.Approve(r.Context(), chi.URLParam(r, "id"), caller(r), req.PayloadDigest, req.Note)
	if err != nil {
		writeDecisionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, decisionResponse{approvalRecord: recordOf(d.Request, d.State, false), Result: d.Reveal})
}

// Reject handles POST /api/v1/approvals/{id}/reject: one rejection by
// an eligible approver rejects the request.
func (a *approvalRoutes) Reject(w http.ResponseWriter, r *http.Request) {
	if !a.ready(w) {
		return
	}
	req, ok := decisionBody(w, r)
	if !ok {
		return
	}
	d, err := a.svc.Reject(r.Context(), chi.URLParam(r, "id"), caller(r), req.PayloadDigest, req.Note)
	if err != nil {
		writeDecisionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, recordOf(d.Request, d.State, false))
}

// Withdraw handles DELETE /api/v1/approvals/{id}: the requester, or the
// owner, withdraws a pending request. The answer is the withdrawn
// request.
func (a *approvalRoutes) Withdraw(w http.ResponseWriter, r *http.Request) {
	if !a.ready(w) {
		return
	}
	got, err := a.svc.Withdraw(r.Context(), chi.URLParam(r, "id"), caller(r))
	if err != nil {
		writeApprovalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, recordOf(got, approval.StateWithdrawn, false))
}

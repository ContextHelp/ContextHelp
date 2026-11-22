package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Steward access over the API (ADR-083 §2, §3): file a steward grant
// request, list and show the grants the caller is party to, revoke one,
// and elevate a browser session into one (handlers_steward_elevate.go).
// Every route is absent (404, ahead of every other check) unless
// steward.enabled is on. Filing and elevating take request:steward;
// reading and revoking take write:self, which every principal holds,
// and the grant's own rule decides: its steward, the requester of the
// request it came from, a holder of approve:steward and the owner see
// and revoke it, and to anyone else it does not exist (404). Approving
// is the approval queue's (/approvals).

// Stable codes of the steward routes, beside the filing codes of the
// steward.grant kind (steward.Code*) and the resolution codes of a
// grant that fails a check (STEWARD_<REASON>, sharing.StewardRefusal).
const (
	// codeElevationBrowserOnly: an API credential asked to elevate or to
	// end an elevation; only a web UI browser session elevates (403).
	codeElevationBrowserOnly = "STEWARD_ELEVATION_BROWSER_ONLY"
	// codeElevationActive: the session is elevated into another grant;
	// end that elevation first (409).
	codeElevationActive = "STEWARD_ELEVATION_ACTIVE"
	// codeGrantClosed: the grant is already revoked (409).
	codeGrantClosed = "STEWARD_GRANT_CLOSED"
)

// stewardRoutes serves steward access. Its parts are nil when the
// router is built without them (route enumeration, an instance with no
// approval mechanism); the routes are then absent.
type stewardRoutes struct {
	svc       *service.Service
	approvals *approval.Service
	stewards  *steward.Service
	sessions  *authn.Sessions
	sec       *security.Emitter
	audit     *sharing.AuditWriter
}

func newStewardRoutes(svc *service.Service, rc RouterConfig) *stewardRoutes {
	return &stewardRoutes{svc: svc, approvals: rc.Approvals, stewards: rc.Stewards, sessions: rc.Sessions, sec: rc.Security, audit: rc.Audit}
}

// enabled reports whether the steward routes exist: the instance is
// wired for steward grants and steward.enabled is on, as the settings
// stand on this request.
func (s *stewardRoutes) enabled() bool {
	return s.stewards != nil && s.approvals != nil && s.stewards.Settings().Enabled
}

// stewardGrantRecord is a grant as the caller sees it: the stored row,
// its state and effective expiry now, and the requester of the request
// it came from. It names no object of the instance.
type stewardGrantRecord struct {
	*storage.StewardGrant
	State              storage.StewardGrantState `json:"state"`
	EffectiveExpiresAt time.Time                 `json:"effective_expires_at"`
	RequestedBy        string                    `json:"requested_by"`
}

func grantRecordOf(v *steward.GrantView) stewardGrantRecord {
	return stewardGrantRecord{StewardGrant: v.Grant, State: v.State, EffectiveExpiresAt: v.EffectiveExpiry, RequestedBy: v.RequestedBy}
}

// stewardFilingBody is the body of POST /api/v1/steward/grants. The
// steward defaults to the caller; exactly one of scope and as_audience
// gives the scope; expires_at is required; not_before defaults to now.
type stewardFilingBody struct {
	StewardID  string          `json:"steward_id"`
	Reason     string          `json:"reason"`
	MatterRef  string          `json:"matter_ref"`
	Scope      json.RawMessage `json:"scope"`
	AsAudience string          `json:"as_audience"`
	NotBefore  *time.Time      `json:"not_before"`
	ExpiresAt  *time.Time      `json:"expires_at"`
	RenewsID   string          `json:"renews_id"`
}

// decodeStrictBody reads a JSON body into v, refusing an unknown key: a
// misspelled field of a legal request is an error, not silence. An
// empty body leaves v as it is.
func decodeStrictBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRegistryBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// File handles POST /api/v1/steward/grants: it files a steward.grant
// request as the caller and answers 202 with the request and its
// Location on the approval queue. Nothing is granted until enough
// approvers other than the caller and the steward approve it.
func (s *stewardRoutes) File(w http.ResponseWriter, r *http.Request) {
	var body stewardFilingBody
	if !decodeStrictBody(w, r, &body) {
		return
	}
	p := caller(r)
	req := steward.Request{
		StewardID: strings.TrimSpace(body.StewardID), MatterRef: body.MatterRef,
		AsAudience: body.AsAudience, RenewsID: body.RenewsID,
	}
	if req.StewardID == "" {
		req.StewardID = p.ID
	}
	if len(body.Scope) > 0 && !bytes.Equal(bytes.TrimSpace(body.Scope), []byte("null")) {
		req.Scope = body.Scope
	}
	if body.NotBefore != nil {
		req.NotBefore = *body.NotBefore
	}
	if body.ExpiresAt != nil {
		req.ExpiresAt = *body.ExpiresAt
	}
	payload, err := json.Marshal(req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request: "+err.Error())
		return
	}
	filed, err := s.approvals.File(r.Context(), approval.Filing{
		Kind: steward.KindName, Action: steward.ActionRequest, Requester: p, SubjectID: req.StewardID,
		Reason: body.Reason, Payload: payload,
	})
	if err != nil {
		writeApprovalError(w, r, err)
		return
	}
	w.Header().Set("Location", approvalLocation(filed.ID))
	WriteJSON(w, http.StatusAccepted, recordOf(filed, approval.StateRequested, false))
}

// stewardListParams are the query parameters GET /steward/grants takes.
var stewardListParams = map[string]bool{"state": true, "steward": true, "limit": true}

// stewardStates are the states a listing filters by.
var stewardStates = []storage.StewardGrantState{
	storage.StewardStatePending, storage.StewardStateActive, storage.StewardStateExpired, storage.StewardStateRevoked,
}

// parseStewardFilter reads GET /steward/grants?state=&steward=&limit=,
// refusing an unknown or repeated parameter and a bad value.
func parseStewardFilter(r *http.Request) (steward.ListFilter, string) {
	var f steward.ListFilter
	q := r.URL.Query()
	for name, vals := range q {
		if !stewardListParams[name] {
			return f, "unknown parameter " + strconv.Quote(name) + " (want state, steward, limit)"
		}
		if len(vals) != 1 || strings.TrimSpace(vals[0]) == "" {
			return f, "parameter " + strconv.Quote(name) + " takes exactly one non-empty value"
		}
	}
	f.StewardID = strings.TrimSpace(q.Get("steward"))
	if st := strings.TrimSpace(q.Get("state")); st != "" {
		f.State = storage.StewardGrantState(st)
		valid := false
		for _, s := range stewardStates {
			valid = valid || s == f.State
		}
		if !valid {
			return f, "state " + strconv.Quote(st) + ": want approved, active, expired or revoked"
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

// List handles GET /api/v1/steward/grants?state=&steward=&limit=: the
// grants the caller sees, newest approval first.
func (s *stewardRoutes) List(w http.ResponseWriter, r *http.Request) {
	f, bad := parseStewardFilter(r)
	if bad != "" {
		WriteError(w, http.StatusBadRequest, CodeInvalidParam, bad)
		return
	}
	views, err := s.stewards.List(r.Context(), caller(r), f)
	if err != nil {
		writeStewardError(w, r, err)
		return
	}
	out := make([]stewardGrantRecord, 0, len(views))
	for _, v := range views {
		out = append(out, grantRecordOf(v))
	}
	WriteJSON(w, http.StatusOK, listBody(out, len(out)))
}

// Get handles GET /api/v1/steward/grants/{id}.
func (s *stewardRoutes) Get(w http.ResponseWriter, r *http.Request) {
	v, err := s.stewards.Get(r.Context(), chi.URLParam(r, "id"), caller(r))
	if err != nil {
		writeStewardError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, grantRecordOf(v))
}

// stewardRevokeBody is the optional body of a revocation.
type stewardRevokeBody struct {
	Reason string `json:"reason"`
}

// Revoke handles POST /api/v1/steward/grants/{id}/revoke: the grant
// ends for good, effective on its steward's next request. The answer is
// the revoked grant.
func (s *stewardRoutes) Revoke(w http.ResponseWriter, r *http.Request) {
	var body stewardRevokeBody
	if !decodeStrictBody(w, r, &body) {
		return
	}
	id, p := chi.URLParam(r, "id"), caller(r)
	if _, err := s.stewards.Revoke(r.Context(), id, p, body.Reason); err != nil {
		writeStewardError(w, r, err)
		return
	}
	s.Get(w, r)
}

// writeStewardError answers a failed steward grant read or revocation:
// a grant the caller may not see is 404 like one that does not exist,
// a grant already revoked is 409, anything else a 500 whose cause goes
// to the log.
func writeStewardError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such steward grant")
	case errors.Is(err, storage.ErrStewardGrantNotApproved):
		WriteError(w, http.StatusConflict, codeGrantClosed, "the steward grant is already revoked")
	default:
		slog.ErrorContext(r.Context(), "steward grant request failed", "route", r.Method+" "+r.URL.Path, "err", err)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not complete the steward grant request")
	}
}

// approvalLocation is the Location of a pending request.
func approvalLocation(id string) string { return "/api/v1/approvals/" + id }

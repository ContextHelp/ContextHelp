package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// defaultAuditLimit caps a read audit listing when limit is unset.
const defaultAuditLimit = 100

// ListShareAudit handles GET /api/v1/share/audit (ADR-080 §9): the
// read audit of non-owner requests, newest first. Each entry is a
// receipt: its decision (allowed, denied or deferred), the ordered
// checks behind it with an outcome and a reason code each, and the
// operation digest. Narrowed by ?principal= (who read), ?object= (what
// was read), ?decision= (allowed, denied or deferred) and ?since= (a date
// YYYY-MM-DD, an RFC 3339 time, or a duration such as 24h or 7d back
// from now), capped by ?limit= (100 by default). Under admin:audit.
// The owner's own requests are not logged, except its previews of
// another viewer, which name the previewed principal or policy in
// policy_versions.
//
// Steward entries (ADR-083 §5) are listed with their grant id, route,
// decision, checks and counts; never the grant's reason, which stays
// with the grant. Their object id lists go to the owner and holders of
// approve:steward only: for anyone else they are left out, and
// ?object= does not match through them.
func (s *shareRoutes) ListShareAudit(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	q := r.URL.Query()
	if err := checkParams(q, []string{"principal", "object", "decision", "since", "limit"}); err != nil {
		writeParamError(w, err)
		return
	}
	f := storage.AccessFilter{
		PrincipalID: strings.TrimSpace(q.Get("principal")),
		ObjectID:    strings.TrimSpace(q.Get("object")),
		Limit:       defaultAuditLimit,
		// Object id lists of steward entries: owner and approvers only.
		HideStewardObjects: !seesStewardObjects(r),
	}
	if d := strings.TrimSpace(q.Get("decision")); d != "" {
		f.Decision = storage.AccessDecision(d)
		if !f.Decision.Valid() {
			writeParamError(w, badParam("decision", "decision must be allowed, denied or deferred"))
			return
		}
	}
	if q.Has("limit") {
		n, perr := intParam(q, "limit", defaultAuditLimit, 1, 10000)
		if perr != nil {
			writeParamError(w, perr)
			return
		}
		f.Limit = n
	}
	if since := strings.TrimSpace(q.Get("since")); since != "" {
		t, ok := parseSince(since, time.Now())
		if !ok {
			writeParamError(w, badParam("since", "since must be a date (YYYY-MM-DD), an RFC 3339 time or a duration (24h, 7d)"))
			return
		}
		f.Since = t
	}
	entries, err := s.sharing.AccessLog().Query(r.Context(), f)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if entries == nil {
		entries = []*storage.AccessEntry{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"data": entries, "total": len(entries)})
}

// parseSince reads a since value: a date, an RFC 3339 time, or a
// duration (with d for days) back from now.
func parseSince(s string, now time.Time) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	d := s
	days := false
	if strings.HasSuffix(d, "d") {
		d, days = strings.TrimSuffix(d, "d")+"h", true
	}
	dur, err := time.ParseDuration(d)
	if err != nil || dur <= 0 {
		return time.Time{}, false
	}
	if days {
		dur *= 24
	}
	return now.Add(-dur), true
}

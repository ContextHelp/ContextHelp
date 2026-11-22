package http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Elevation (ADR-083 §3): a steward's browser session reads under one
// grant at a time, and only while elevated. POST stores the grant on
// the session row once the grant passes every check the next request
// will run; from then on the session authenticator names it on every
// request and the request resolves the grant-only viewer. DELETE, the
// idle timeout (steward.elevation_idle) or a request the grant can no
// longer serve ends it; the session lives on. In v1 an API credential
// never elevates.

// reasonBrowserOnly is the refusal reason of an elevation asked with an
// API credential, as the security event records it.
const reasonBrowserOnly = "browser_only"

// elevationRecord answers an elevation: the grant, the session, the
// matter, when the elevation was last used and when it ends if idle,
// and when the grant itself ends.
type elevationRecord struct {
	GrantID       string    `json:"grant_id"`
	SessionID     string    `json:"session_id"`
	MatterRef     string    `json:"matter_ref,omitempty"`
	ActiveAt      time.Time `json:"active_at"`
	IdleExpiresAt time.Time `json:"idle_expires_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// browserSession reports whether p is a web UI browser session this
// router can elevate, writing the refusal when it is not.
func (s *stewardRoutes) browserSession(w http.ResponseWriter, p *authn.Principal) bool {
	if s.sessions != nil && p.SessionKind() == authn.SessionKindUI {
		return true
	}
	writeBrowserOnly(w)
	return false
}

// writeBrowserOnly answers an elevation asked by anything but a web UI
// browser session.
func writeBrowserOnly(w http.ResponseWriter) {
	WriteError(w, http.StatusForbidden, codeElevationBrowserOnly,
		"only a web UI browser session elevates into a steward grant; an API credential never does")
}

// Elevate handles POST /api/v1/steward/grants/{id}/elevate: the
// caller's browser session reads under grant id from its next request.
// The caller must see the grant (404 otherwise) and be its steward, and
// the grant must pass every check of the resolver now (the resolution
// code otherwise); a session elevated into another grant is 409. The
// same grant again refreshes the elevation.
func (s *stewardRoutes) Elevate(w http.ResponseWriter, r *http.Request) {
	ctx, id, p := r.Context(), chi.URLParam(r, "id"), caller(r)
	if !s.browserSession(w, p) {
		steward.RecordElevationRefused(ctx, s.sec, p.ID, id, reasonBrowserOnly, false)
		return
	}
	v, err := s.stewards.Get(ctx, id, p)
	if err != nil {
		writeStewardError(w, r, err)
		return
	}
	if current := p.Meta[authn.MetaStewardGrantID]; current != "" && current != id {
		WriteError(w, http.StatusConflict, codeElevationActive,
			"this session is elevated into another steward grant; end that elevation first")
		return
	}
	if s.svc == nil || s.svc.Viewers() == nil {
		WriteError(w, http.StatusNotFound, CodeNotFound, "resource not found")
		return
	}
	// The check the next request runs, run now: a grant that would be
	// refused is never stored on the session.
	if _, err := s.svc.Viewers().Resolve(ctx, p.ID, p.Kind, p.Meta[authn.MetaIdentityID], sharing.AsSteward(id)); err != nil {
		var refusal *sharing.StewardRefusal
		if errors.As(err, &refusal) {
			steward.RecordElevationRefused(ctx, s.sec, p.ID, id, refusal.Reason, refusal.Deferred)
			writeStewardRefusal(w, p, refusal)
			return
		}
		slog.ErrorContext(ctx, "steward elevation: viewer resolution failed", "principal", p.ID, "err", err)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "viewer resolution failed")
		return
	}
	at, err := s.sessions.Elevate(ctx, p, id)
	switch {
	case errors.Is(err, storage.ErrSessionElevated):
		WriteError(w, http.StatusConflict, codeElevationActive,
			"this session is elevated into another steward grant; end that elevation first")
		return
	case errors.Is(err, authn.ErrElevationNeedsSession):
		writeBrowserOnly(w)
		return
	case errors.Is(err, storage.ErrNotFound):
		WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "the session ended")
		return
	case err != nil:
		slog.ErrorContext(ctx, "steward elevation failed", "principal", p.ID, "err", err)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not elevate the session")
		return
	}
	WriteJSON(w, http.StatusOK, elevationRecord{
		GrantID: id, SessionID: p.Meta[authn.MetaSessionID], MatterRef: v.Grant.MatterRef,
		ActiveAt: at, IdleExpiresAt: at.Add(s.sessions.ElevationIdle()), ExpiresAt: v.EffectiveExpiry,
	})
}

// EndElevation handles DELETE /api/v1/steward/grants/{id}/elevate: the
// caller's browser session stops reading under grant id, and its next
// request is the ordinary view. A session not elevated into id is 404.
func (s *stewardRoutes) EndElevation(w http.ResponseWriter, r *http.Request) {
	ctx, id, p := r.Context(), chi.URLParam(r, "id"), caller(r)
	if !s.browserSession(w, p) {
		return
	}
	ended, err := s.sessions.EndElevation(ctx, p, id, authn.ElevationEndedByCaller)
	switch {
	case errors.Is(err, authn.ErrElevationNeedsSession):
		writeBrowserOnly(w)
		return
	case err != nil:
		slog.ErrorContext(ctx, "steward elevation end failed", "principal", p.ID, "err", err)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not end the elevation")
		return
	case !ended:
		WriteError(w, http.StatusNotFound, CodeNotFound, "this session is not elevated into that steward grant")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

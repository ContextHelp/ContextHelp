package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// write:self routes (ADR-079 §5): a principal manages its own keys,
// identities and sessions. The target of every write is checked
// against the caller (Principal.IsSelf) before anything changes, so a
// write:self holder reaches nobody else's; admin:principals manages
// others' through /principals/{id}.

// caller returns the authenticated principal, or the local owner on a
// private instance.
func caller(r *http.Request) *authn.Principal {
	if p, ok := authn.FromContext(r.Context()); ok {
		return p
	}
	return authn.LocalPrincipal()
}

// ListOwnKeys handles GET /api/v1/self/keys.
func (g *registryRoutes) ListOwnKeys(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	g.writeKeys(w, r, caller(r).ID)
}

// CreateOwnKey handles POST /api/v1/self/keys: a key for the caller's
// own principal, never wider than the caller's effective scopes, so a
// narrowed key or a browser session cannot mint its way up to the
// principal's full role scopes.
func (g *registryRoutes) CreateOwnKey(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	var req createKeyRequest
	if !decodeRegistryBody(w, r, &req) {
		return
	}
	g.mintKey(w, r, caller(r).ID, req, true)
}

// RevokeOwnKey handles DELETE /api/v1/self/keys/{key}.
func (g *registryRoutes) RevokeOwnKey(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	info, err := g.keys.Get(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		writeRegistryError(w, err, "key")
		return
	}
	if !caller(r).IsSelf(info.PrincipalID) {
		WriteError(w, http.StatusForbidden, CodeNotSelf, "this key belongs to another principal; admin:principals manages others' keys")
		return
	}
	if err := g.keys.Revoke(r.Context(), info.ID); err != nil {
		writeRegistryError(w, err, "revoke key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListOwnIdentities handles GET /api/v1/self/identities.
func (g *registryRoutes) ListOwnIdentities(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	g.writeIdentities(w, r, caller(r).ID)
}

// RemoveOwnIdentity handles DELETE /api/v1/self/identities/{identity}.
func (g *registryRoutes) RemoveOwnIdentity(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	idn, err := g.reg.Identities().Get(r.Context(), chi.URLParam(r, "identity"))
	if err != nil {
		writeRegistryError(w, err, "identity")
		return
	}
	if !caller(r).IsSelf(idn.PrincipalID) {
		WriteError(w, http.StatusForbidden, CodeNotSelf, "this identity belongs to another principal; admin:principals manages others' identities")
		return
	}
	if err := g.revokeIdentity(r, idn); err != nil {
		writeRegistryError(w, err, "revoke identity")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ownSession is one listed session of the caller.
type ownSession struct {
	*storage.UISession
	// Status is active, expired or revoked.
	Status string `json:"status"`
	// Current marks the session the request itself rides on.
	Current bool `json:"current,omitempty"`
}

// Session statuses as the listing reports them.
const (
	sessionStatusActive  = "active"
	sessionStatusExpired = "expired"
	sessionStatusRevoked = "revoked"
)

func sessionStatus(s *storage.UISession, now time.Time) string {
	switch {
	case s.RevokedAt != nil:
		return sessionStatusRevoked
	case !s.Active(now):
		return sessionStatusExpired
	}
	return sessionStatusActive
}

// ListOwnSessions handles GET /api/v1/self/sessions?all=1: the
// caller's browser sessions, active ones unless all is set. A private
// instance has none.
func (g *registryRoutes) ListOwnSessions(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	if g.sessions == nil {
		WriteJSON(w, http.StatusOK, listBody([]ownSession{}, 0))
		return
	}
	p := caller(r)
	all := r.URL.Query().Get("all") == "1" || r.URL.Query().Get("all") == "true"
	list, err := g.sessions.ListByPrincipal(r.Context(), p.ID, all)
	if err != nil {
		writeRegistryError(w, err, "list sessions")
		return
	}
	now := g.sessions.Now()
	out := make([]ownSession, 0, len(list))
	for _, s := range list {
		out = append(out, ownSession{UISession: s, Status: sessionStatus(s, now), Current: s.ID == p.Meta[authn.MetaSessionID]})
	}
	WriteJSON(w, http.StatusOK, listBody(out, len(out)))
}

// EndOwnSession handles DELETE /api/v1/self/sessions/{session}. Ending
// the session the request rides on also clears its cookie.
func (g *registryRoutes) EndOwnSession(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	if g.sessions == nil {
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such session")
		return
	}
	id := chi.URLParam(r, "session")
	s, err := g.sessions.Get(r.Context(), id)
	if err != nil {
		writeRegistryError(w, err, "session")
		return
	}
	p := caller(r)
	if !p.IsSelf(s.PrincipalID) {
		WriteError(w, http.StatusForbidden, CodeNotSelf, "this session belongs to another principal; dpkms session revoke ends others' sessions")
		return
	}
	if err := g.sessions.Revoke(r.Context(), id, authn.RevokeReasonLogout); err != nil {
		writeRegistryError(w, err, "end session")
		return
	}
	if p.IsSession() && p.Meta[authn.MetaSessionID] == id {
		http.SetCookie(w, sessionCookie(r, "", time.Time{}))
	}
	w.WriteHeader(http.StatusNoContent)
}

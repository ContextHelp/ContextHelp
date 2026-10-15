package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The principal registry over the API (ADR-079 §5): principals, their
// identities and keys under admin:principals (handlers_principals.go),
// a principal's own keys, identities and sessions under write:self
// (handlers_self.go). Every route reads and writes the registry
// directly, never through the viewer-scoped driver: scopes govern it,
// not share policies.
//
// Two rules hold on every route that yields a credential, a principal,
// an identity, a key or a sign-in link:
//
//   - Nothing widens. Whatever a route yields holds at most the
//     effective scopes of the caller that asked for it (callerCeiling):
//     a principal's roles, the roles of the principal an identity is
//     bound to or a link minted for, a key's scopes. A narrowed admin
//     key, a browser session or a reader can never mint its way up or
//     take over a more privileged principal.
//   - The owner is protected. Only the owner touches the owner's
//     credentials over the API; another admin is refused. The host's
//     dpkms auth key create --principal owner stays the bootstrap and
//     recovery path.

// Error codes of the registry routes.
const (
	// CodePrincipalInactive is an operation that needs an active
	// principal (a key, a link) on a suspended or revoked one.
	CodePrincipalInactive = "PRINCIPAL_INACTIVE"
	// CodeOwnerProtected is an attempt to create, suspend or revoke the
	// owner principal, or to touch its credentials as someone else.
	CodeOwnerProtected = "OWNER_PROTECTED"
	// CodeAlreadyExists is a create whose id or credential triple is
	// taken.
	CodeAlreadyExists = "ALREADY_EXISTS"
	// CodeNotSelf is a write:self route reached for a credential or
	// session that belongs to another principal.
	CodeNotSelf = "NOT_SELF"
	// CodeInvalidScope is a key asked for with a scope that does not
	// exist or that the principal's roles do not grant.
	CodeInvalidScope = "INVALID_SCOPE"
	// CodeScopeNotHeld is a principal, identity, link or key that would
	// hold a scope the caller does not: nothing yielded over the API
	// holds more than the caller that asked for it.
	CodeScopeNotHeld = "SCOPE_NOT_HELD"
)

// maxRegistryBody bounds the body of a registry write.
const maxRegistryBody = 16 << 10

// Identity methods POST /principals/{id}/identities binds by hand: a
// login a trusted proxy will assert, or an OIDC subject. Keys and links
// are bound by their own ceremonies; passkeys and certificates by theirs.
var manualIdentityMethods = []string{storage.IdentityMethodProxy, storage.IdentityMethodOIDC}

// Default roles of a principal created without any, by kind (ADR-079
// §1): a person reads, a device captures, a peer pushes.
var defaultRolesByKind = map[string][]string{
	storage.PrincipalKindPerson: {authn.RoleReader},
	storage.PrincipalKindDevice: {authn.RoleWriter},
	storage.PrincipalKindPeer:   {authn.RoleWriter},
}

// registryRoutes serves the principal registry. reg and keys are nil
// only when the router is built without a driver (route enumeration).
type registryRoutes struct {
	reg      storage.AuthStore
	keys     *authn.Keys
	sessions *authn.Sessions
}

func newRegistryRoutes(rc RouterConfig) *registryRoutes {
	return &registryRoutes{reg: rc.Registry, keys: rc.Keys, sessions: rc.Sessions}
}

// ready reports whether the registry is wired, writing the refusal
// when it is not.
func (g *registryRoutes) ready(w http.ResponseWriter) bool {
	if g.reg == nil || g.keys == nil {
		WriteError(w, http.StatusNotFound, CodeNotFound, "this instance has no principal registry")
		return false
	}
	return true
}

// ownerGuard refuses a caller other than the owner acting on the owner
// principal's credentials, writing the refusal.
func ownerGuard(w http.ResponseWriter, r *http.Request, principalID string) bool {
	if principalID == storage.OwnerPrincipal && !caller(r).IsSelf(storage.OwnerPrincipal) {
		WriteError(w, http.StatusForbidden, CodeOwnerProtected,
			"only the owner manages the owner principal's credentials over the API; on the host, dpkms auth key create --principal owner")
		return false
	}
	return true
}

// ceiling is the scope set nothing a caller asks for may exceed.
type ceiling []authn.Scope

// callerCeiling returns the caller's effective scopes that a role
// bundle grants: what a credential it asks for may hold. The
// session-only scopes drop out, so a browser session's key is its
// reads and nothing more.
func callerCeiling(r *http.Request) ceiling {
	p := caller(r)
	granted := authn.ScopesForRoles(p.Roles)
	held := ceiling{}
	for _, s := range p.Scopes {
		if slices.Contains(granted, s) {
			held = append(held, s)
		}
	}
	return held
}

// exceeds returns the first of scopes the ceiling lacks.
func (c ceiling) exceeds(scopes []authn.Scope) (authn.Scope, bool) {
	for _, s := range scopes {
		if !slices.Contains(c, s) {
			return s, true
		}
	}
	return "", false
}

// withinCeiling reports whether the caller may yield something holding
// scopes, writing the 403 when it may not. what names that thing for
// the message ("a principal with roles writer", "a key").
func withinCeiling(w http.ResponseWriter, r *http.Request, scopes []authn.Scope, what string) bool {
	if s, over := callerCeiling(r).exceeds(scopes); over {
		WriteError(w, http.StatusForbidden, CodeScopeNotHeld,
			fmt.Sprintf("%s would hold %s, which the caller does not: nothing yielded over the API holds more than the caller that asked for it", what, s))
		return false
	}
	return true
}

func decodeRegistryBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRegistryBody)).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON")
		return false
	}
	return true
}

// writeRegistryError maps the registry's sentinels to statuses; any
// other error is a 500 naming what failed.
func writeRegistryError(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such "+what)
	case errors.Is(err, storage.ErrAlreadyExists):
		WriteError(w, http.StatusConflict, CodeAlreadyExists, what+" already exists")
	case errors.Is(err, storage.ErrPrincipalInactive):
		WriteError(w, http.StatusConflict, CodePrincipalInactive, "the principal is suspended or revoked")
	case errors.Is(err, storage.ErrOwnerProtected):
		WriteError(w, http.StatusForbidden, CodeOwnerProtected, "the owner principal cannot be created, suspended or revoked")
	default:
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not "+what)
	}
}

func listBody(data any, total int) map[string]any {
	return map[string]any{"data": data, "total": total}
}

// ListPrincipals handles GET /api/v1/principals?kind=&status=.
func (g *registryRoutes) ListPrincipals(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	q := r.URL.Query()
	f := storage.PrincipalFilter{Kind: q.Get("kind"), Status: q.Get("status"), Limit: parseIntDefault(q.Get("limit"), 0)}
	if f.Kind != "" && !slices.Contains(storage.PrincipalKinds, f.Kind) {
		WriteError(w, http.StatusBadRequest, "INVALID_PARAM", "kind: want one of "+strings.Join(storage.PrincipalKinds, ", "))
		return
	}
	if f.Status != "" && !slices.Contains(storage.PrincipalStatuses, f.Status) {
		WriteError(w, http.StatusBadRequest, "INVALID_PARAM", "status: want one of "+strings.Join(storage.PrincipalStatuses, ", "))
		return
	}
	list, err := g.reg.Principals().List(r.Context(), f)
	if err != nil {
		writeRegistryError(w, err, "list principals")
		return
	}
	if list == nil {
		list = []*storage.Principal{}
	}
	WriteJSON(w, http.StatusOK, listBody(list, len(list)))
}

// createPrincipalRequest is the body of POST /api/v1/principals.
type createPrincipalRequest struct {
	Kind        string   `json:"kind"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
}

// CreatePrincipal handles POST /api/v1/principals. The new principal's
// roles grant no scope the caller lacks.
func (g *registryRoutes) CreatePrincipal(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	var req createPrincipalRequest
	if !decodeRegistryBody(w, r, &req) {
		return
	}
	if !slices.Contains(storage.PrincipalKinds, req.Kind) || req.Kind == storage.PrincipalKindOwner {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "kind: want person, device or peer")
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "display_name is required")
		return
	}
	roles := req.Roles
	if len(roles) == 0 {
		roles = slices.Clone(defaultRolesByKind[req.Kind])
	}
	if err := authn.ValidateRoles(roles); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "roles: "+err.Error())
		return
	}
	if !withinCeiling(w, r, authn.ScopesForRoles(roles), "a principal with roles "+strings.Join(roles, ", ")) {
		return
	}
	p := &storage.Principal{Kind: req.Kind, DisplayName: strings.TrimSpace(req.DisplayName), Roles: roles}
	if err := g.reg.Principals().Create(r.Context(), p); err != nil {
		writeRegistryError(w, err, "create principal")
		return
	}
	WriteJSON(w, http.StatusCreated, p)
}

// GetPrincipal handles GET /api/v1/principals/{id}.
func (g *registryRoutes) GetPrincipal(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	p, err := g.reg.Principals().Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

// SuspendPrincipal handles POST /api/v1/principals/{id}/suspend.
func (g *registryRoutes) SuspendPrincipal(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !ownerGuard(w, r, id) {
		return
	}
	if err := g.reg.Principals().Suspend(r.Context(), id); err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	g.answerPrincipal(w, r, id)
}

// RevokePrincipal handles POST /api/v1/principals/{id}/revoke: the
// principal loses every identity, key and session in one transaction.
func (g *registryRoutes) RevokePrincipal(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !ownerGuard(w, r, id) {
		return
	}
	if err := g.reg.Principals().Revoke(r.Context(), id, time.Now().UTC()); err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	g.answerPrincipal(w, r, id)
}

func (g *registryRoutes) answerPrincipal(w http.ResponseWriter, r *http.Request, id string) {
	p, err := g.reg.Principals().Get(r.Context(), id)
	if err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

// ListIdentities handles GET /api/v1/principals/{id}/identities.
func (g *registryRoutes) ListIdentities(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := g.reg.Principals().Get(r.Context(), id); err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	g.writeIdentities(w, r, id)
}

func (g *registryRoutes) writeIdentities(w http.ResponseWriter, r *http.Request, principalID string) {
	list, err := g.reg.Identities().ListByPrincipal(r.Context(), principalID)
	if err != nil {
		writeRegistryError(w, err, "list identities")
		return
	}
	if list == nil {
		list = []*storage.Identity{}
	}
	WriteJSON(w, http.StatusOK, listBody(list, len(list)))
}

// addIdentityRequest is the body of POST /api/v1/principals/{id}/identities.
type addIdentityRequest struct {
	Method  string `json:"method"`
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
	Name    string `json:"name"`
}

// AddIdentity handles POST /api/v1/principals/{id}/identities: binds a
// proxy login or an OIDC subject to the principal by hand. Binding to
// a principal whose roles grant more than the caller holds would hand
// the caller that principal, so it is refused.
func (g *registryRoutes) AddIdentity(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	var req addIdentityRequest
	if !decodeRegistryBody(w, r, &req) {
		return
	}
	if !slices.Contains(manualIdentityMethods, req.Method) {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "method: want "+strings.Join(manualIdentityMethods, " or ")+
			" (keys, links and passkeys are bound by their own ceremonies)")
		return
	}
	if strings.TrimSpace(req.Subject) == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "subject is required")
		return
	}
	id := chi.URLParam(r, "id")
	if !ownerGuard(w, r, id) {
		return
	}
	p, err := g.reg.Principals().Get(r.Context(), id)
	if err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	if !p.Active() {
		WriteError(w, http.StatusConflict, CodePrincipalInactive, "the principal is suspended or revoked")
		return
	}
	if !withinCeiling(w, r, authn.ScopesForRoles(p.Roles), "an identity on principal "+p.ID) {
		return
	}
	idn := &storage.Identity{PrincipalID: p.ID, Method: req.Method, Issuer: strings.TrimSpace(req.Issuer),
		Subject: strings.TrimSpace(req.Subject), Name: strings.TrimSpace(req.Name)}
	if err := g.reg.Identities().Create(r.Context(), idn); err != nil {
		writeRegistryError(w, err, "identity")
		return
	}
	WriteJSON(w, http.StatusCreated, idn)
}

// RemoveIdentity handles DELETE /api/v1/principals/{id}/identities/{identity}:
// the identity is revoked, with the sessions it minted; an api_key
// identity takes its key record with it.
func (g *registryRoutes) RemoveIdentity(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !ownerGuard(w, r, id) {
		return
	}
	idn, err := g.reg.Identities().Get(r.Context(), chi.URLParam(r, "identity"))
	if err != nil || idn.PrincipalID != id {
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such identity on this principal")
		return
	}
	if err := g.revokeIdentity(r, idn); err != nil {
		writeRegistryError(w, err, "revoke identity")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// revokeIdentity ends idn: through Keys for an api_key row, so the key
// record stops verifying too, else through the identity store.
func (g *registryRoutes) revokeIdentity(r *http.Request, idn *storage.Identity) error {
	if idn.Method == storage.IdentityMethodAPIKey {
		return g.keys.Revoke(r.Context(), idn.Subject)
	}
	return g.reg.Identities().Revoke(r.Context(), idn.ID, time.Now().UTC())
}

// ListPrincipalKeys handles GET /api/v1/principals/{id}/keys.
func (g *registryRoutes) ListPrincipalKeys(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := g.reg.Principals().Get(r.Context(), id); err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	g.writeKeys(w, r, id)
}

func (g *registryRoutes) writeKeys(w http.ResponseWriter, r *http.Request, principalID string) {
	list, err := g.keys.ListByPrincipal(r.Context(), principalID)
	if err != nil {
		writeRegistryError(w, err, "list keys")
		return
	}
	WriteJSON(w, http.StatusOK, listBody(list, len(list)))
}

// createKeyRequest is the body of POST .../keys.
type createKeyRequest struct {
	Name string `json:"name"`
	// ExpiresIn is a lifetime in seconds; ExpiresAt an RFC 3339 time.
	// At most one may be set; neither means no expiry.
	ExpiresIn int       `json:"expires_in"`
	ExpiresAt time.Time `json:"expires_at"`
	// Scopes narrows the key; empty means every scope the key may hold.
	Scopes []authn.Scope `json:"scopes"`
}

// createdKey is the body of a successful key creation: the key, shown
// once, with its description.
type createdKey struct {
	Key string `json:"key"`
	authn.IssuedKey
}

// expiry resolves the request's expiry fields (expiryOf).
func (req createKeyRequest) expiry(now time.Time) (time.Time, error) {
	return expiryOf(req.ExpiresIn, req.ExpiresAt, now)
}

// CreatePrincipalKey handles POST /api/v1/principals/{id}/keys: an
// admin mints a key for a principal. Without scopes the key asks for
// the principal's whole role bundle, which the caller must hold in
// full; a narrowed admin names the scopes it can grant.
func (g *registryRoutes) CreatePrincipalKey(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	var req createKeyRequest
	if !decodeRegistryBody(w, r, &req) {
		return
	}
	id := chi.URLParam(r, "id")
	if !ownerGuard(w, r, id) {
		return
	}
	g.mintKey(w, r, id, req, false)
}

// mintKey mints a key for principalID from req, narrowed twice: to
// the principal's role scopes and to the caller's ceiling. forSelf
// picks the default when no scopes are asked for: the caller's ceiling
// for its own key, the principal's whole bundle for another's.
func (g *registryRoutes) mintKey(w http.ResponseWriter, r *http.Request, principalID string, req createKeyRequest, forSelf bool) {
	expiresAt, err := req.expiry(time.Now().UTC())
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	target, err := g.reg.Principals().Get(r.Context(), principalID)
	if err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	if !target.Active() {
		WriteError(w, http.StatusConflict, CodePrincipalInactive, "the principal is suspended or revoked")
		return
	}
	granted := authn.ScopesForRoles(target.Roles)
	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = granted
		if forSelf {
			scopes = callerCeiling(r)
		}
	}
	for _, s := range scopes {
		switch {
		case !slices.Contains(authn.AllScopes, s):
			WriteError(w, http.StatusBadRequest, CodeInvalidScope, fmt.Sprintf("unknown scope %q", s))
			return
		case !slices.Contains(granted, s):
			WriteError(w, http.StatusBadRequest, CodeInvalidScope,
				fmt.Sprintf("scope %s is not granted to principal %s (roles %s)", s, target.ID, strings.Join(target.Roles, ", ")))
			return
		}
	}
	if !withinCeiling(w, r, scopes, "a key for principal "+target.ID) {
		return
	}
	secret, issued, err := g.keys.Mint(r.Context(), authn.MintKey{
		PrincipalID: target.ID, Name: strings.TrimSpace(req.Name), ExpiresAt: expiresAt, Scopes: scopes,
	})
	if err != nil {
		writeRegistryError(w, err, "create key")
		return
	}
	WriteJSON(w, http.StatusCreated, createdKey{Key: secret, IssuedKey: *issued})
}

// RevokePrincipalKey handles DELETE /api/v1/principals/{id}/keys/{key}.
func (g *registryRoutes) RevokePrincipalKey(w http.ResponseWriter, r *http.Request) {
	if !g.ready(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !ownerGuard(w, r, id) {
		return
	}
	info, err := g.keys.Get(r.Context(), chi.URLParam(r, "key"))
	if err != nil || info.PrincipalID != id {
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such key on this principal")
		return
	}
	if err := g.keys.Revoke(r.Context(), info.ID); err != nil {
		writeRegistryError(w, err, "revoke key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

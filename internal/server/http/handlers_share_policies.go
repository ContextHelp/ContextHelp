package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Share policies, templates, grants, ceilings and key policy bindings
// (ADR-080 §2, §11). A policy document is the YAML of ADR-080 §2, or
// its JSON form, sent as the document field of a JSON body; the store
// keeps the validated JSON and owns the version.

// policyDocumentRequest is the body of POST /api/v1/share/policies and
// PUT /api/v1/share/policies/{policy}.
type policyDocumentRequest struct {
	// Document is the policy, as YAML or JSON text.
	Document string `json:"document"`
}

// readPolicyDocument parses the body's document as one policy, writing
// the 400 that names the failing field when it does not parse.
func readPolicyDocument(w http.ResponseWriter, r *http.Request) (sharing.Policy, bool) {
	var req policyDocumentRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxPolicyBody)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON")
		return sharing.Policy{}, false
	}
	if strings.TrimSpace(req.Document) == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "document is required: the policy as YAML or JSON text")
		return sharing.Policy{}, false
	}
	p, err := sharing.ParsePolicy([]byte(req.Document))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "document: "+strings.TrimPrefix(err.Error(), "policy document 1: "))
		return sharing.Policy{}, false
	}
	return p, true
}

// ListPolicies handles GET /api/v1/share/policies.
func (s *shareRoutes) ListPolicies(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	list, err := s.sharing.Policies().List(r.Context())
	if err != nil {
		writeRegistryError(w, err, "list policies")
		return
	}
	if list == nil {
		list = []*storage.SharePolicy{}
	}
	WriteJSON(w, http.StatusOK, listBody(list, len(list)))
}

// CreatePolicy handles POST /api/v1/share/policies: the body is the
// policy document; its name names the policy. Version 1.
func (s *shareRoutes) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	p, ok := readPolicyDocument(w, r)
	if !ok {
		return
	}
	s.storePolicy(w, r, p)
}

// storePolicy creates p as a new policy row and answers it.
func (s *shareRoutes) storePolicy(w http.ResponseWriter, r *http.Request, p sharing.Policy) {
	spec, err := p.Spec()
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	row := &storage.SharePolicy{Name: p.Name, Spec: spec}
	if err := s.sharing.Policies().Create(r.Context(), row); err != nil {
		writeRegistryError(w, err, "policy")
		return
	}
	WriteJSON(w, http.StatusCreated, row)
}

// GetPolicy handles GET /api/v1/share/policies/{policy}.
func (s *shareRoutes) GetPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	row, err := s.resolvePolicy(r, chi.URLParam(r, "policy"))
	if err != nil {
		writeRegistryError(w, err, "policy")
		return
	}
	WriteJSON(w, http.StatusOK, row)
}

// UpdatePolicy handles PUT /api/v1/share/policies/{policy}: the body
// replaces the spec and the version is bumped. The document's name
// must be the policy's: a policy is never renamed through an edit.
func (s *shareRoutes) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	row, err := s.resolvePolicy(r, chi.URLParam(r, "policy"))
	if err != nil {
		writeRegistryError(w, err, "policy")
		return
	}
	p, ok := readPolicyDocument(w, r)
	if !ok {
		return
	}
	if p.Name != row.Name {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST",
			"name: the document names "+p.Name+" but this is policy "+row.Name+"; a policy is not renamed through an edit")
		return
	}
	spec, err := p.Spec()
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	updated, err := s.sharing.Policies().Update(r.Context(), row.ID, spec, time.Now().UTC())
	if err != nil {
		writeRegistryError(w, err, "policy")
		return
	}
	WriteJSON(w, http.StatusOK, updated)
}

// templateRecord is a built-in policy template: its name and the spec
// installing it stores.
type templateRecord struct {
	Name string         `json:"name"`
	Spec sharing.Policy `json:"spec"`
}

// ListTemplates handles GET /api/v1/share/templates.
func (s *shareRoutes) ListTemplates(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	tpls := sharing.Templates()
	out := make([]templateRecord, 0, len(tpls))
	for _, p := range tpls {
		out = append(out, templateRecord{Name: p.Name, Spec: p})
	}
	WriteJSON(w, http.StatusOK, listBody(out, len(out)))
}

// InstallTemplate handles POST /api/v1/share/templates/{name}: the
// built-in policy becomes a policy row at version 1. A policy of that
// name already stored is refused (409): the owner may have edited it,
// and an install never overwrites.
func (s *shareRoutes) InstallTemplate(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	p, ok := sharing.Template(strings.TrimSpace(chi.URLParam(r, "name")))
	if !ok {
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such template: want "+strings.Join(templateNames(), " or "))
		return
	}
	s.storePolicy(w, r, p)
}

func templateNames() []string {
	tpls := sharing.Templates()
	names := make([]string, 0, len(tpls))
	for _, p := range tpls {
		names = append(names, p.Name)
	}
	return names
}

// grantRecord is a grant with the names of what it binds and the
// policy's version at the time of the read.
type grantRecord struct {
	storage.ShareGrant
	Audience      string `json:"audience"`
	Policy        string `json:"policy"`
	PolicyVersion int    `json:"policy_version"`
}

// createGrantRequest is the body of POST /api/v1/share/grants.
type createGrantRequest struct {
	// Audience and Policy name what the grant binds, by id or name.
	Audience string `json:"audience"`
	Policy   string `json:"policy"`
	expiryRequest
}

// CreateGrant handles POST /api/v1/share/grants.
func (s *shareRoutes) CreateGrant(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	var req createGrantRequest
	if !decodeRegistryBody(w, r, &req) {
		return
	}
	now := time.Now().UTC()
	expiresAt, err := req.expiry(now)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if strings.TrimSpace(req.Audience) == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "audience is required")
		return
	}
	if strings.TrimSpace(req.Policy) == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "policy is required")
		return
	}
	a, err := s.resolveAudience(r, req.Audience)
	if err != nil {
		writeRegistryError(w, err, "audience")
		return
	}
	p, err := s.resolvePolicy(r, req.Policy)
	if err != nil {
		writeRegistryError(w, err, "policy")
		return
	}
	g := &storage.ShareGrant{AudienceID: a.ID, PolicyID: p.ID, ExpiresAt: expiresAt, CreatedBy: caller(r).ID, CreatedAt: now}
	if err := s.sharing.Grants().Create(r.Context(), g); err != nil {
		writeRegistryError(w, err, "grant")
		return
	}
	WriteJSON(w, http.StatusCreated, grantRecord{ShareGrant: *g, Audience: a.Name, Policy: p.Name, PolicyVersion: p.Version})
}

// ListGrants handles GET /api/v1/share/grants?audience=&policy=&principal=&include_revoked=:
// with principal, the grants in force for that principal (through a
// live membership, neither revoked nor expired); otherwise every grant
// matching the filters, revoked ones only when asked.
func (s *shareRoutes) ListGrants(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	q := r.URL.Query()
	if principal := strings.TrimSpace(q.Get("principal")); principal != "" {
		active, err := s.sharing.Grants().ListActiveForPrincipal(r.Context(), principal, time.Now().UTC())
		if err != nil {
			writeRegistryError(w, err, "list grants")
			return
		}
		out := make([]grantRecord, 0, len(active))
		for _, ag := range active {
			out = append(out, grantRecord{ShareGrant: ag.Grant, Audience: ag.Audience.Name, Policy: ag.Policy.Name, PolicyVersion: ag.Policy.Version})
		}
		WriteJSON(w, http.StatusOK, listBody(out, len(out)))
		return
	}
	f := storage.GrantFilter{IncludeRevoked: flagParam(q, "include_revoked")}
	if ref := q.Get("audience"); ref != "" {
		a, err := s.resolveAudience(r, ref)
		if err != nil {
			writeRegistryError(w, err, "audience")
			return
		}
		f.AudienceID = a.ID
	}
	if ref := q.Get("policy"); ref != "" {
		p, err := s.resolvePolicy(r, ref)
		if err != nil {
			writeRegistryError(w, err, "policy")
			return
		}
		f.PolicyID = p.ID
	}
	list, err := s.sharing.Grants().List(r.Context(), f)
	if err != nil {
		writeRegistryError(w, err, "list grants")
		return
	}
	out, err := s.grantRecords(r, list)
	if err != nil {
		writeRegistryError(w, err, "list grants")
		return
	}
	WriteJSON(w, http.StatusOK, listBody(out, len(out)))
}

// grantRecords names what each grant binds, reading each audience and
// policy once.
func (s *shareRoutes) grantRecords(r *http.Request, list []*storage.ShareGrant) ([]grantRecord, error) {
	audiences := map[string]*storage.Audience{}
	policies := map[string]*storage.SharePolicy{}
	out := make([]grantRecord, 0, len(list))
	for _, g := range list {
		a, ok := audiences[g.AudienceID]
		if !ok {
			var err error
			if a, err = s.sharing.Audiences().Get(r.Context(), g.AudienceID); err != nil {
				return nil, err
			}
			audiences[g.AudienceID] = a
		}
		p, ok := policies[g.PolicyID]
		if !ok {
			var err error
			if p, err = s.sharing.Policies().Get(r.Context(), g.PolicyID); err != nil {
				return nil, err
			}
			policies[g.PolicyID] = p
		}
		out = append(out, grantRecord{ShareGrant: *g, Audience: a.Name, Policy: p.Name, PolicyVersion: p.Version})
	}
	return out, nil
}

// RevokeGrant handles DELETE /api/v1/share/grants/{grant}.
func (s *shareRoutes) RevokeGrant(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	if err := s.sharing.Grants().Revoke(r.Context(), strings.TrimSpace(chi.URLParam(r, "grant")), time.Now().UTC()); err != nil {
		writeRegistryError(w, err, "grant")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ceilingRecord is a principal's class ceiling (ADR-080 §2): the
// classes it may never see, whatever its grants say.
type ceilingRecord struct {
	PrincipalID string   `json:"principal_id"`
	DenyClasses []string `json:"deny_classes"`
}

// GetCeiling handles GET /api/v1/principals/{id}/ceiling.
func (s *shareRoutes) GetCeiling(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	classes, err := s.sharing.DenyClasses(r.Context(), id)
	if err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	if classes == nil {
		classes = []string{}
	}
	WriteJSON(w, http.StatusOK, ceilingRecord{PrincipalID: id, DenyClasses: classes})
}

// setCeilingRequest is the body of PUT /api/v1/principals/{id}/ceiling.
type setCeilingRequest struct {
	DenyClasses []string `json:"deny_classes"`
}

// SetCeiling handles PUT /api/v1/principals/{id}/ceiling: the list
// replaces the ceiling; an empty list clears it.
func (s *shareRoutes) SetCeiling(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	var req setCeilingRequest
	if !decodeRegistryBody(w, r, &req) {
		return
	}
	for _, c := range req.DenyClasses {
		if err := storage.ValidateClassName(c); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "deny_classes: "+err.Error())
			return
		}
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	classes := storage.NormalizeClasses(req.DenyClasses)
	if err := s.sharing.SetDenyClasses(r.Context(), id, classes); err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	if classes == nil {
		classes = []string{}
	}
	WriteJSON(w, http.StatusOK, ceilingRecord{PrincipalID: id, DenyClasses: classes})
}

// keyPolicyRequest is the body of PUT .../keys/{key}/policy: the
// policy the key narrows its principal to, by id or name; "" unbinds.
type keyPolicyRequest struct {
	Policy string `json:"policy"`
}

// keyPolicyRecord is a key's policy binding.
type keyPolicyRecord struct {
	KeyID      string `json:"key_id"`
	IdentityID string `json:"identity_id"`
	// PolicyID and Policy are empty when the key is unbound.
	PolicyID string `json:"policy_id,omitempty"`
	Policy   string `json:"policy,omitempty"`
}

// BindOwnKeyPolicy handles PUT /api/v1/self/keys/{key}/policy: the
// caller binds one of its own keys (403 NOT_SELF for another's).
func (s *shareRoutes) BindOwnKeyPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) || s.keys == nil {
		return
	}
	info, err := s.keys.Get(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		writeRegistryError(w, err, "key")
		return
	}
	if !caller(r).IsSelf(info.PrincipalID) {
		WriteError(w, http.StatusForbidden, CodeNotSelf, "this key belongs to another principal; admin:shares binds others' keys through /share/keys/{key}/policy")
		return
	}
	s.bindKeyPolicy(w, r, info.ID, info.IdentityID)
}

// BindKeyPolicy handles PUT /api/v1/share/keys/{key}/policy, the
// admin:shares form: any principal's key but the owner's, which only
// the owner binds.
func (s *shareRoutes) BindKeyPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) || s.keys == nil {
		return
	}
	info, err := s.keys.Get(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		writeRegistryError(w, err, "key")
		return
	}
	if !ownerGuard(w, r, info.PrincipalID) {
		return
	}
	s.bindKeyPolicy(w, r, info.ID, info.IdentityID)
}

// bindKeyPolicy reads the body and binds identityID to the policy it
// names, or unbinds it.
func (s *shareRoutes) bindKeyPolicy(w http.ResponseWriter, r *http.Request, keyID, identityID string) {
	var req keyPolicyRequest
	if !decodeRegistryBody(w, r, &req) {
		return
	}
	rec := keyPolicyRecord{KeyID: keyID, IdentityID: identityID}
	if ref := strings.TrimSpace(req.Policy); ref != "" {
		p, err := s.resolvePolicy(r, ref)
		if err != nil {
			writeRegistryError(w, err, "policy")
			return
		}
		rec.PolicyID, rec.Policy = p.ID, p.Name
	}
	if err := s.sharing.BindIdentityPolicy(r.Context(), identityID, rec.PolicyID); err != nil {
		writeRegistryError(w, err, "bind key policy")
		return
	}
	WriteJSON(w, http.StatusOK, rec)
}

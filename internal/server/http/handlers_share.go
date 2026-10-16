package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The sharing registry over the API (ADR-080 §11): audiences and their
// members (this file), share policies, the built-in templates, grants,
// the per-principal ceiling and the policy a key narrows to
// (handlers_share_policies.go). Every route reads and writes the
// registry directly, never through the viewer-scoped driver: scopes
// govern it (admin:shares, write:self for a key's own binding), not
// share policies.
//
// Audiences, policies and grants are plain JSON records with their ids.
// A path or body may name an audience or a policy by id or by name.
// Validation errors are 400 INVALID_REQUEST naming the field.
//
// Every write goes through sharechange.Service (share_changes.go): the
// owner's change applies at once, a non-owner administrator's applies
// at once when it narrows or changes no view, and a change that widens
// anyone's view is answered 202 with a pending approval request and
// writes nothing (ADR-083 §10 B).

// maxPolicyBody bounds a policy document.
const maxPolicyBody = 64 << 10

// shareRoutes serves the sharing registry. sharing and reg are nil
// when the router is built without a driver (route enumeration) or on
// an instance with no registry.
type shareRoutes struct {
	sharing storage.SharingStore
	reg     storage.AuthStore
	keys    *authn.Keys
	// svc runs every change; nil with sharing or reg.
	svc *sharechange.Service
	// changes records the invite mint in the audit log (a change of the
	// registry is recorded by svc, in its transaction).
	changes changeLog
}

func newShareRoutes(rc RouterConfig) *shareRoutes {
	s := &shareRoutes{sharing: rc.Sharing, reg: rc.Registry, keys: rc.Keys, changes: changeLog{rc.AuditLog}}
	if rc.Sharing != nil && rc.Registry != nil {
		s.svc = &sharechange.Service{
			Deps:      sharechange.Deps{Sharing: rc.Sharing, Auth: rc.Registry, Audit: rc.AuditLog},
			Approvals: rc.Approvals,
		}
	}
	return s
}

// ready reports whether the sharing registry is wired, writing the
// refusal when it is not.
func (s *shareRoutes) ready(w http.ResponseWriter) bool {
	if s.sharing == nil || s.reg == nil {
		WriteError(w, http.StatusNotFound, CodeNotFound, "this instance has no sharing registry")
		return false
	}
	return true
}

// audienceRecord is an audience with its memberships, expired ones
// included.
type audienceRecord struct {
	storage.Audience
	Members []*storage.AudienceMember `json:"members"`
}

// resolveAudience finds the audience ref names, by id then by name.
func (s *shareRoutes) resolveAudience(r *http.Request, ref string) (*storage.Audience, error) {
	return resolveAudienceIn(r.Context(), s.sharing.Audiences(), ref)
}

// resolveAudienceIn finds the audience ref names in store, by id then
// by name; storage.ErrNotFound when neither matches.
func resolveAudienceIn(ctx context.Context, store storage.AudienceStore, ref string) (*storage.Audience, error) {
	ref = strings.TrimSpace(ref)
	a, err := store.Get(ctx, ref)
	if errors.Is(err, storage.ErrNotFound) {
		a, err = store.GetByName(ctx, ref)
	}
	return a, err
}

// resolvePolicy finds the policy ref names, by id then by name.
func (s *shareRoutes) resolvePolicy(r *http.Request, ref string) (*storage.SharePolicy, error) {
	ref = strings.TrimSpace(ref)
	p, err := s.sharing.Policies().Get(r.Context(), ref)
	if errors.Is(err, storage.ErrNotFound) {
		p, err = s.sharing.Policies().GetByName(r.Context(), ref)
	}
	return p, err
}

// audienceRecordOf fills the record of a with its members.
func (s *shareRoutes) audienceRecordOf(r *http.Request, a *storage.Audience) (audienceRecord, error) {
	members, err := s.sharing.Audiences().ListMembers(r.Context(), a.ID)
	if err != nil {
		return audienceRecord{}, err
	}
	if members == nil {
		members = []*storage.AudienceMember{}
	}
	return audienceRecord{Audience: *a, Members: members}, nil
}

// ListAudiences handles GET /api/v1/share/audiences: every audience,
// by name, with its members.
func (s *shareRoutes) ListAudiences(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	list, err := s.sharing.Audiences().List(r.Context())
	if err != nil {
		writeRegistryError(w, err, "list audiences")
		return
	}
	out := make([]audienceRecord, 0, len(list))
	for _, a := range list {
		rec, err := s.audienceRecordOf(r, a)
		if err != nil {
			writeRegistryError(w, err, "list audience members")
			return
		}
		out = append(out, rec)
	}
	WriteJSON(w, http.StatusOK, listBody(out, len(out)))
}

// createAudienceRequest is the body of POST /api/v1/share/audiences.
type createAudienceRequest struct {
	Name string `json:"name"`
}

// CreateAudience handles POST /api/v1/share/audiences.
func (s *shareRoutes) CreateAudience(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	var req createAudienceRequest
	if !decodeRegistryBody(w, r, &req) {
		return
	}
	if err := storage.ValidateSharingName("audience", strings.TrimSpace(req.Name)); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name: "+err.Error())
		return
	}
	applied, ok := s.submit(w, r, routeCreateAudience, sharing.AudienceCreate{Name: strings.TrimSpace(req.Name)})
	if !ok {
		return
	}
	WriteJSON(w, http.StatusCreated, audienceRecord{Audience: *applied.Audience, Members: []*storage.AudienceMember{}})
}

// GetAudience handles GET /api/v1/share/audiences/{audience}.
func (s *shareRoutes) GetAudience(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	a, err := s.resolveAudience(r, chi.URLParam(r, "audience"))
	if err != nil {
		writeRegistryError(w, err, "audience")
		return
	}
	rec, err := s.audienceRecordOf(r, a)
	if err != nil {
		writeRegistryError(w, err, "list audience members")
		return
	}
	WriteJSON(w, http.StatusOK, rec)
}

// expiryRequest is the optional expiry of a membership or a grant:
// ExpiresIn a lifetime in seconds, ExpiresAt an RFC 3339 time. At most
// one may be set; neither means no expiry.
type expiryRequest struct {
	ExpiresIn int       `json:"expires_in"`
	ExpiresAt time.Time `json:"expires_at"`
}

// storeTime returns at as the registry stores a time: UTC, whole
// milliseconds. nil for none.
func storeTime(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	t := at.UTC().Truncate(time.Millisecond)
	return &t
}

// expiry resolves the request's expiry fields; nil for none.
func (req expiryRequest) expiry(now time.Time) (*time.Time, error) {
	at, err := expiryOf(req.ExpiresIn, req.ExpiresAt, now)
	if err != nil || at.IsZero() {
		return nil, err
	}
	return &at, nil
}

// expiryOf resolves an expires_in / expires_at pair against now: the
// zero time for no expiry.
func expiryOf(expiresIn int, expiresAt, now time.Time) (time.Time, error) {
	switch {
	case expiresIn < 0:
		return time.Time{}, errors.New("expires_in must be positive")
	case expiresIn > 0 && !expiresAt.IsZero():
		return time.Time{}, errors.New("give expires_in or expires_at, not both")
	case expiresIn > 0:
		return now.Add(time.Duration(expiresIn) * time.Second), nil
	case !expiresAt.IsZero() && !expiresAt.After(now):
		return time.Time{}, errors.New("expires_at is not in the future")
	}
	return expiresAt, nil
}

// AddMember handles PUT /api/v1/share/audiences/{audience}/members/{principal}:
// the principal joins the audience, or its membership's expiry is
// replaced. The owner sees everything and joins no audience; an
// inactive principal is refused.
func (s *shareRoutes) AddMember(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	var req expiryRequest
	if !decodeRegistryBody(w, r, &req) {
		return
	}
	now := time.Now().UTC()
	expiresAt, err := req.expiry(now)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	a, err := s.resolveAudience(r, chi.URLParam(r, "audience"))
	if err != nil {
		writeRegistryError(w, err, "audience")
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "principal"))
	if id == storage.OwnerPrincipal {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "principal: the owner sees everything and joins no audience")
		return
	}
	p, err := s.reg.Principals().Get(r.Context(), id)
	if err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	if !p.Active() {
		WriteError(w, http.StatusConflict, CodePrincipalInactive, "the principal is suspended or revoked")
		return
	}
	applied, ok := s.submit(w, r, routePutMember, sharing.MemberPut{AudienceID: a.ID, PrincipalID: p.ID, ExpiresAt: storeTime(expiresAt)})
	if !ok {
		return
	}
	WriteJSON(w, http.StatusOK, applied.Member)
}

// RemoveMember handles DELETE /api/v1/share/audiences/{audience}/members/{principal}.
func (s *shareRoutes) RemoveMember(w http.ResponseWriter, r *http.Request) {
	if !s.ready(w) {
		return
	}
	a, err := s.resolveAudience(r, chi.URLParam(r, "audience"))
	if err != nil {
		writeRegistryError(w, err, "audience")
		return
	}
	if _, ok := s.submit(w, r, routeDeleteMember, sharing.MemberRemove{AudienceID: a.ID, PrincipalID: strings.TrimSpace(chi.URLParam(r, "principal"))}); !ok {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

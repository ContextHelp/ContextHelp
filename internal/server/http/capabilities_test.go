package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/auth/authtest"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The capability table names exactly the routes of the route table.
func TestRouteAccessTableCoversEveryRoute(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range APIRouteSpecs() {
		key := routeKey(s.Method, s.Pattern)
		seen[key] = true
		_, _, known := RouteAccess(s.Method, s.Pattern)
		assert.True(t, known, "route %s has no entry in routeAccessTable: classify it open, owner-only or by capability", key)
	}
	for key := range routeAccessTable {
		assert.True(t, seen[key], "routeAccessTable names %s, which the route table does not mount", key)
	}
}

// Tokens of the capability tests.
const (
	capOwnerToken     = "tok-owner-fixture-0123456789abcdef0123456789"
	capColleagueToken = "tok-colleague-fixture-0123456789abcdef012345"
	capColleague      = "prn_capability_colleague"
)

// capabilityEnv is a router over a store holding the owner, a
// colleague principal granted the colleagues template, a work-tagged
// contact the colleague sees and a note it does not.
type capabilityEnv struct {
	router  http.Handler
	svc     *service.Service
	contact string
	note    string
}

func newCapabilityEnv(t *testing.T) *capabilityEnv {
	t.Helper()
	ctx := ownerCtx()
	driver := storageutil.NewTestDriver(t)
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
	now := time.Now().UTC().Truncate(time.Second)

	person := &storage.Principal{ID: capColleague, Kind: storage.PrincipalKindPerson, DisplayName: "Colleague", Roles: []string{authn.RoleReader}}
	require.NoError(t, driver.Auth().Principals().Create(ctx, person))
	grantTemplate(t, driver, person.ID, "colleagues-network")

	contact := &storage.KnowledgeObject{
		ID: "cap-contact", Type: "contact", Status: storage.ObjectStatusActive, RawContent: "Sarah Lee | Head of Ops @ Acme",
		Tags: []storage.Tag{{Label: "work"}}, ContentHash: "sha256:cap-contact", CreatedAt: now, UpdatedAt: now,
	}
	note := &storage.KnowledgeObject{
		ID: "cap-note", Type: "note", Status: storage.ObjectStatusActive, RawContent: "private note about Sarah",
		ContentHash: "sha256:cap-note", CreatedAt: now, UpdatedAt: now,
	}
	for _, o := range []*storage.KnowledgeObject{contact, note} {
		require.NoError(t, driver.Objects().Create(ctx, o))
	}

	chain := authtest.TokenChain(
		authtest.Token{Token: capOwnerToken, Principal: storage.OwnerPrincipal, Roles: []string{authn.RoleAdmin}},
		authtest.Token{Token: capColleagueToken, Principal: capColleague, Roles: []string{authn.RoleReader}},
	)
	return &capabilityEnv{
		router:  NewRouterWithConfig(svc, RouterConfig{Auth: chain}),
		svc:     svc,
		contact: contact.ID,
		note:    note.ID,
	}
}

// grantTemplate puts principalID in an audience granted the built-in
// policy template named policy.
func grantTemplate(t *testing.T, driver storage.StorageDriver, principalID, policy string) {
	t.Helper()
	ctx := ownerCtx()
	st := driver.Sharing()
	var tpl storage.SharePolicy
	for _, p := range templatesAsRows(t) {
		if p.Name == policy {
			tpl = p
		}
	}
	require.NotEmpty(t, tpl.Name, "unknown template %s", policy)
	require.NoError(t, st.Policies().Create(ctx, &tpl))
	aud := &storage.Audience{Name: "cap-" + policy}
	require.NoError(t, st.Audiences().Create(ctx, aud))
	require.NoError(t, st.Audiences().AddMember(ctx, &storage.AudienceMember{AudienceID: aud.ID, PrincipalID: principalID, AddedBy: storage.OwnerPrincipal}))
	require.NoError(t, st.Grants().Create(ctx, &storage.ShareGrant{AudienceID: aud.ID, PolicyID: tpl.ID, CreatedBy: storage.OwnerPrincipal}))
}

func (e *capabilityEnv) do(t *testing.T, method, path, token string) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// The router resolves each principal's viewer: the colleague lists the
// work contact only, with an exact total, and a hidden note is a 404
// for it and a 200 for the owner.
func TestRouterResolvesRestrictedViewer(t *testing.T) {
	e := newCapabilityEnv(t)

	code, body := e.do(t, http.MethodGet, "/api/v1/objects", capColleagueToken)
	require.Equal(t, http.StatusOK, code, "%s", body)
	var list struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Data, 1, "%s", body)
	assert.Equal(t, e.contact, list.Data[0].ID)
	assert.Equal(t, 1, list.Total)
	assert.NotContains(t, string(body), e.note)

	code, body = e.do(t, http.MethodGet, "/api/v1/objects/"+e.note, capColleagueToken)
	assert.Equal(t, http.StatusNotFound, code, "%s", body)
	assert.Contains(t, string(body), CodeNotFound)
	code, _ = e.do(t, http.MethodGet, "/api/v1/objects/"+e.note, capOwnerToken)
	assert.Equal(t, http.StatusOK, code)
	code, _ = e.do(t, http.MethodGet, "/api/v1/objects/"+e.contact, capColleagueToken)
	assert.Equal(t, http.StatusOK, code)

	code, body = e.do(t, http.MethodGet, "/api/v1/stats", capColleagueToken)
	require.Equal(t, http.StatusOK, code, "%s", body)
	var st service.Stats
	require.NoError(t, json.Unmarshal(body, &st))
	assert.Equal(t, 1, st.KnowledgeObjects)
	assert.Equal(t, 0, st.Jobs.Total)
}

// Owner-only routes and routes a capability closes answer 403
// NOT_SHARED to the colleague, never an empty 200, and 200 to the
// owner; routes the colleague's capabilities open stay open.
func TestCapabilityClosedRoutesAnswerNotShared(t *testing.T) {
	e := newCapabilityEnv(t)
	closed := []string{"/api/v1/jobs", "/api/v1/inbox", "/api/v1/inbox/queue", "/api/v1/capture/recent",
		"/api/v1/events", "/api/v1/pipelines", "/api/v1/watches", "/api/v1/steps/registries",
		"/api/v1/pages", "/api/v1/topic-index", "/api/v1/search?q=type==contact"}
	for _, path := range closed {
		code, body := e.do(t, http.MethodGet, path, capColleagueToken)
		assert.Equal(t, http.StatusForbidden, code, "colleague GET %s: %s", path, body)
		assert.Contains(t, string(body), CodeNotShared, "GET %s", path)
		if strings.HasPrefix(path, "/api/v1/events") {
			continue // the owner's stream holds the connection open
		}
		code, body = e.do(t, http.MethodGet, path, capOwnerToken)
		assert.NotEqual(t, http.StatusForbidden, code, "owner GET %s: %s", path, body)
	}
	for _, path := range []string{"/api/v1/entities", "/api/v1/objects/facets", "/api/v1/aliases", "/api/v1/saved-searches", "/api/v1/whoami"} {
		code, body := e.do(t, http.MethodGet, path, capColleagueToken)
		assert.Equal(t, http.StatusOK, code, "colleague GET %s: %s", path, body)
	}
}

// A request that reaches a handler with a refused read is answered as
// a capability refusal, never as a server error.
func TestWriteErrorTurnsRefusedReadsIntoNotShared(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusInternalServerError, "INTERNAL_ERROR", "Jobs.List: "+storage.ErrRestrictedViewer.Error())
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), CodeNotShared)
	assert.NotContains(t, rec.Body.String(), "Jobs.List")

	rec = httptest.NewRecorder()
	WriteError(rec, http.StatusInternalServerError, "INTERNAL_ERROR", "disk full")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	// No viewer attached is a transport bug, not a capability answer.
	rec = httptest.NewRecorder()
	WriteError(rec, http.StatusInternalServerError, "INTERNAL_ERROR", "Objects.List: "+storage.ErrNoViewer.Error()+": "+storage.ErrRestrictedViewer.Error())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
